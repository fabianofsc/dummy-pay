// Package clock provides the Clock port used everywhere the service needs
// the current time. No package outside this one and cmd/ may call
// time.Now() or time.Sleep() (ADR-0012). See docs/plan-v1.md step 1.2.
package clock
