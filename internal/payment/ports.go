package payment

import (
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
