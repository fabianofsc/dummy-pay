package clock_test

import (
	"sort"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/clock"
	"dummypay/internal/payment"
)

var _ payment.IDGenerator = clock.UUIDv7Generator{}

func TestUUIDv7Generator_ProducesValidUUIDv7(t *testing.T) {
	gen := clock.UUIDv7Generator{}

	id := gen.NewID()

	require.Equal(t, uuid.Version(7), id.Version())
}

// The whole reason for choosing UUIDv7 over UUIDv4 (ADR-0006) is index
// locality from time ordering. This is the property that actually matters:
// generated-in-order identifiers must also sort in that order as strings,
// the way a PostgreSQL btree on a uuid column would see them.
func TestUUIDv7Generator_AThousandIDsSortInGenerationOrder(t *testing.T) {
	gen := clock.UUIDv7Generator{}

	const n = 1000
	generated := make([]string, n)
	for i := range generated {
		generated[i] = gen.NewID().String()
	}

	sorted := make([]string, n)
	copy(sorted, generated)
	sort.Strings(sorted)

	require.Equal(t, generated, sorted)
}

func TestUUIDv7Generator_NeverProducesTheNilUUID(t *testing.T) {
	gen := clock.UUIDv7Generator{}

	require.NotEqual(t, uuid.Nil, gen.NewID())
}
