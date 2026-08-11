// Package clock provides the Clock port used everywhere the service needs
// the current time. No package outside this one and cmd/ may call
// time.Now() or time.Sleep() (ADR-0012). See docs/plan-v1.md step 1.2.
package clock

import "time"

// Real implements payment.Clock against the system clock. It is the only
// place in the service, besides cmd/, that may call time.Now().
type Real struct{}

func (Real) Now() time.Time { return time.Now() }

// Fake implements payment.Clock with a time that only moves when told to.
// It exists so scheduling logic never depends on wall-clock time in a test
// (ADR-0012): nothing sleeps, tests advance the clock and assert.
type Fake struct {
	now time.Time
}

// NewFake returns a Fake clock starting at start.
func NewFake(start time.Time) *Fake {
	return &Fake{now: start}
}

func (c *Fake) Now() time.Time { return c.now }

// Set moves the clock to t directly.
func (c *Fake) Set(t time.Time) { c.now = t }

// Advance moves the clock forward by exactly d.
func (c *Fake) Advance(d time.Duration) { c.now = c.now.Add(d) }
