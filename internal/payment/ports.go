package payment

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Clock provides the current time. No package outside internal/clock and
// cmd/ may call time.Now() directly (ADR-0012); everything else reads it
// through this port so tests can control it.
type Clock interface {
	Now() time.Time
}

// IDGenerator produces unique, time-ordered identifiers (ADR-0006).
type IDGenerator interface {
	NewID() uuid.UUID
}

// ErrPaymentNotFound is returned when no payment exists for a given id.
var ErrPaymentNotFound = errors.New("payment not found")

// PaymentRepository persists and loads payments (spec §8).
type PaymentRepository interface {
	Insert(ctx context.Context, p Payment) error
	FindByID(ctx context.Context, id uuid.UUID) (Payment, error)
	// Update persists p's current fields — used after Settle() transitions
	// a payment, to write the new status and updated_at.
	Update(ctx context.Context, p Payment) error
}

// IdempotencyState is the recorded lifecycle state of an idempotency key.
type IdempotencyState string

const (
	IdempotencyInFlight  IdempotencyState = "IN_FLIGHT"
	IdempotencyCompleted IdempotencyState = "COMPLETED"
)

// ErrIdempotencyRecordNotFound is returned by IdempotencyStore.Load when no
// record exists for the given account and key.
var ErrIdempotencyRecordNotFound = errors.New("idempotency record not found")

// IdempotencyRecord is the persisted state of one idempotency key
// (spec §3 "idempotency_keys"). PaymentID, ResponseStatus, and ResponseBody
// are zero values until State is IdempotencyCompleted; CompletedAt is the
// zero time.Time until then too.
type IdempotencyRecord struct {
	AccountID          uuid.UUID
	IdempotencyKey     string
	RequestFingerprint [32]byte
	State              IdempotencyState
	PaymentID          uuid.UUID
	ResponseStatus     int
	ResponseBody       []byte
	ClaimedAt          time.Time
	CompletedAt        time.Time
}

// IdempotencyStore is the claim/complete side of idempotent payment creation
// (spec §4.1, ADR-0007).
type IdempotencyStore interface {
	// Claim attempts to create a new IN_FLIGHT record for (accountID, key).
	// ok is false when a record already exists — the caller must Load it and
	// branch per spec §4.1's table (fingerprint mismatch / IN_FLIGHT /
	// COMPLETED). Claim never returns an error for "already exists": that is
	// the normal, expected outcome of a race, signalled by ok=false.
	Claim(ctx context.Context, accountID uuid.UUID, key string, fingerprint [32]byte, claimedAt time.Time) (ok bool, err error)

	// Load reads the existing record for (accountID, key). Returns
	// ErrIdempotencyRecordNotFound if none exists.
	Load(ctx context.Context, accountID uuid.UUID, key string) (IdempotencyRecord, error)

	// Reclaim takes over an IN_FLIGHT record whose claimedAt is strictly
	// before cutoff (i.e. its lease has expired), resetting claimedAt to
	// now. ok is false when reclaim loses the race: the record is
	// COMPLETED, already reclaimed by someone else after cutoff, or still
	// within its lease (claimedAt >= cutoff). Like Claim, "lost the race"
	// is signalled by ok=false, never by a non-nil err.
	Reclaim(ctx context.Context, accountID uuid.UUID, key string, cutoff, now time.Time) (ok bool, err error)

	// Complete marks an IN_FLIGHT record COMPLETED, attaching the payment it
	// produced and the exact response to replay verbatim on retry.
	Complete(ctx context.Context, accountID uuid.UUID, key string, paymentID uuid.UUID, responseStatus int, responseBody []byte, completedAt time.Time) error
}
