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
