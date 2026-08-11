package clock_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dummypay/internal/clock"
	"dummypay/internal/payment"
)

// Compile-time proof that both implementations satisfy the domain-declared
// port (ADR-0003): the domain owns the contract, the adapter conforms to it.
var (
	_ payment.Clock = clock.Real{}
	_ payment.Clock = &clock.Fake{}
)

func TestReal_NowReflectsActualTime(t *testing.T) {
	before := time.Now()
	got := clock.Real{}.Now()
	after := time.Now()

	require.False(t, got.Before(before))
	require.False(t, got.After(after))
}

func TestFake_NowReturnsWhatItWasSetTo(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := clock.NewFake(start)

	require.True(t, c.Now().Equal(start))

	next := time.Date(2030, 6, 15, 12, 0, 0, 0, time.UTC)
	c.Set(next)

	require.True(t, c.Now().Equal(next))
}

func TestFake_AdvanceMovesByExactlyTheGivenDuration(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := clock.NewFake(start)

	c.Advance(90 * time.Second)

	require.True(t, c.Now().Equal(start.Add(90*time.Second)))
}

func TestFake_NeverCallsRealTime(t *testing.T) {
	// A fake constructed far in the past must stay there — proof it never
	// falls back to time.Now() internally.
	start := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	c := clock.NewFake(start)

	require.True(t, c.Now().Before(time.Now()))
	require.True(t, c.Now().Equal(start))
}
