# ADR-0012. Inject the clock and express scheduling as data

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

The contract requires that the delay before a `PROCESSING` payment settles is
configurable per environment and **deterministic in tests**. Time also drives
idempotency lease expiry and delivery timestamps.

Code that calls `time.Now()` and `time.Sleep` directly cannot satisfy that. A
test of the settlement path would have to wait for real time to pass, making the
suite slow; and to stay fast the delay would be shortened until the test starts
racing the code it tests. Both outcomes are worse than the bug they hide.

Time-dependent behaviour that cannot be tested — lease expiry, retry backoff —
simply goes untested.

## Decision

A `Clock` port exposing `Now()`. Production wires a real clock; tests wire one
they advance explicitly.

Scheduling is expressed as a `due_at` value on an outbox row, not as a timer.
Nothing waits; work becomes due, and the worker picks it up
([ADR-0008](adr-0008-outbox-in-process-worker.md)).

The worker is a function a test calls directly. No background goroutine runs
during tests.

`DUMMYPAY_PROCESSING_DELAY` is a few seconds in local development, so a
developer can watch the transition happen, and zero in tests.

## Consequences

**Positive**

- No `time.Sleep` anywhere in the suite. The full `PROCESSING → APPROVED`
  transition is verified in microseconds.
- Time-dependent behaviour becomes ordinary behaviour: expiring an idempotency
  lease is advancing a clock and asserting, not waiting.
- The delay is a real configured value in development rather than something
  tests forced to zero everywhere.

**Negative**

- No code outside the clock adapter may call `time.Now()`, and the discipline
  needs enforcement — one stray call reintroduces nondeterminism in a way that
  shows up as an occasional CI failure months later.
- The test clock is a second implementation and can diverge from real semantics,
  particularly around monotonic versus wall-clock readings.
- Every function that reads time takes a dependency it would not otherwise need.

## Compliance

A CI check fails the build on `time.Now()` or `time.Sleep` outside the clock
adapter and `cmd/`. The test suite has a wall-clock budget; exceeding it is a
failure, on the assumption that a suddenly slow suite means something started
waiting on real time.

## Notes

Related: [ADR-0008](adr-0008-outbox-in-process-worker.md) — expressing schedule
as data rather than as timers is what makes the injected clock sufficient.
