package clock

import "github.com/google/uuid"

// UUIDv7Generator implements payment.IDGenerator (ADR-0006). Generation
// order is guaranteed increasing within a process: google/uuid's NewV7
// derives both the millisecond timestamp and the sub-millisecond sequence
// field from a mutex-guarded monotonic counter, so two identifiers minted in
// the same millisecond still sort correctly as strings.
type UUIDv7Generator struct{}

func (UUIDv7Generator) NewID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		// uuid.NewV7 only fails if the entropy source (crypto/rand) fails,
		// which means the process can no longer be trusted to generate safe
		// identifiers at all.
		panic("clock: failed to generate UUIDv7: " + err.Error())
	}
	return id
}
