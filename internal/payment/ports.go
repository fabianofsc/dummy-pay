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
// record exists for the given account and key, and by Complete when the key
// it is asked to complete was never claimed at all.
var ErrIdempotencyRecordNotFound = errors.New("idempotency record not found")

// ErrIdempotencyOwnershipLost is returned by IdempotencyStore.Complete when
// the record still exists but is no longer held by the caller: the caller's
// lease expired and another request reclaimed the key before this one
// finished its work. Distinct from ErrIdempotencyRecordNotFound, which means
// no record exists — that is a programming error, whereas losing ownership
// is an operational fact about a request that ran longer than its lease.
var ErrIdempotencyOwnershipLost = errors.New("idempotency key ownership lost to a reclaim")

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
	//
	// Reclaim only means anything because Claim commits on its own, outside
	// the transaction that does the work. That is deliberate: if the claim
	// were part of the work transaction, a process dying mid-flight would
	// roll its claim back and leave nothing to reclaim, and the lease would
	// protect against nothing (ADR-0007). The cost of that choice is that a
	// claim can outlive its owner's usefulness — see Complete.
	Reclaim(ctx context.Context, accountID uuid.UUID, key string, cutoff, now time.Time) (ok bool, err error)

	// Complete marks an IN_FLIGHT record COMPLETED, attaching the payment it
	// produced and the exact response to replay verbatim on retry.
	//
	// claimedAt is an ownership token, not a timestamp to store: it must be
	// the claimedAt this caller established — the value it passed to Claim,
	// or the now a successful Reclaim set — and the record is only completed
	// while it still carries that value. This is what keeps a slow owner
	// honest. Because Claim commits independently of the work transaction, a
	// request whose work outlives its lease can find its key reclaimed and
	// re-completed by someone else while it is still running; without this
	// check its Complete would overwrite the reclaimer's payment and
	// response, leaving two payments for one key and a stored response no
	// caller ever received.
	//
	// Returns ErrIdempotencyOwnershipLost when the record exists but has
	// moved on, and ErrIdempotencyRecordNotFound when there is no record at
	// all. Never reports success without writing.
	Complete(ctx context.Context, accountID uuid.UUID, key string, claimedAt time.Time, paymentID uuid.UUID, responseStatus int, responseBody []byte, completedAt time.Time) error
}

var (
	// ErrIdempotencyConflict is returned when a request arrives with an
	// idempotency key whose first request is still in flight, within its
	// lease (spec §4.1, 409 idempotency_conflict).
	ErrIdempotencyConflict = errors.New("idempotency key is in flight")
	// ErrIdempotencyKeyReuse is returned when a key is replayed with a body
	// that fingerprints differently from the original (spec §4.1, 422
	// idempotency_key_reuse).
	ErrIdempotencyKeyReuse = errors.New("idempotency key reused with a different request body")
)

// TxManager runs fn within a single transaction; every port call fn makes
// through the ctx it is given participates in that transaction (spec §8).
type TxManager interface {
	Within(ctx context.Context, fn func(ctx context.Context) error) error
}

// OutboxKind names one of the two kinds of scheduled work (spec §3
// "outbox_work").
type OutboxKind string

const (
	OutboxSettlePayment  OutboxKind = "SETTLE_PAYMENT"
	OutboxDeliverWebhook OutboxKind = "DELIVER_WEBHOOK"
)

// OutboxWriter enqueues work due at a given time (spec §8).
type OutboxWriter interface {
	Enqueue(ctx context.Context, kind OutboxKind, subjectID uuid.UUID, dueAt time.Time) error
}

// Subscription is the active webhook subscription for an account, reduced to
// what the create-payment flow needs to decide whether to emit an event. The
// full subscription — URL, secret — is Phase 7's concern.
type Subscription struct {
	ID     uuid.UUID
	Events []EventType
}

// SubscriptionRepository loads the active subscription for an account
// (spec §8). ok is false when there is no active subscription.
type SubscriptionRepository interface {
	LoadActive(ctx context.Context, accountID uuid.UUID) (Subscription, bool, error)
}

// DeliveryDraft carries everything needed to create a webhook delivery row.
// Payload bytes are computed by the concrete DeliveryRepository adapter, not
// here — spec §6 keeps event construction and HMAC signing coupled in
// internal/webhook so the bytes are never re-serialised between creation and
// send. The domain only decides whether, and with what data, to create a
// delivery.
type DeliveryDraft struct {
	EventID               uuid.UUID
	EventType             EventType
	SubscriptionID        uuid.UUID
	PaymentID             uuid.UUID
	ReferenceID           string
	Status                Status
	ProviderTransactionID uuid.UUID
	CreatedAt             time.Time
}

// DeliveryRepository creates webhook delivery records (spec §8).
type DeliveryRepository interface {
	Create(ctx context.Context, d DeliveryDraft) (deliveryID uuid.UUID, err error)
}

// Sender delivers signed webhook bytes to a URL (spec §5 "DELIVER_WEBHOOK",
// spec §8). A non-2xx response is reported as httpStatus with err nil — it
// is not a failure to deliver, only a failure the receiving side reported.
// Only a transport failure (connection refused, timeout, DNS) is an err,
// with httpStatus 0; the worker relies on this distinction to leave
// last_http_status null exactly for transport failures (spec §5).
type Sender interface {
	Send(ctx context.Context, url string, body []byte, signature string) (httpStatus int, err error)
}
