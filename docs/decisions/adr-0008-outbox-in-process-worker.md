# ADR-0008. Run asynchronous work from an outbox table with an in-process worker

- **Status:** Superseded by ADR-0015
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

Two behaviours in V1 happen after the response has been sent:

1. A payment created with `card_processing_approved` or
   `card_processing_declined` returns `PROCESSING` and settles later, after a
   configurable delay.
2. Webhook events are delivered, and failed deliveries are retried on demand.

There is also a correctness trap. A payment is committed to PostgreSQL, and an
event about it must be delivered. If those are separate operations, a crash
between them produces a payment nobody was told about — the dual-write problem.
For a service whose value proposition is trustworthy webhook behaviour, that is
the worst available failure.

Options considered: goroutines with `time.AfterFunc`, a transactional outbox
with a worker, a separate worker binary, and an external queue.

## Decision

A work table holding `due_at`, a kind, a payload, an attempt count, and a state.
Rows are inserted **in the same transaction as the state change that caused
them**. An in-process worker polls with `SELECT … FOR UPDATE SKIP LOCKED`,
executes due work, and records the result.

Scheduled settlement and webhook delivery are two kinds of work in one
mechanism. `POST /v1/webhook-deliveries/{delivery_id}/retry` re-enqueues, so a
retry travels the same code path as the first attempt.

**Why not goroutines with timers.** Everything pending lives in memory, so a
restart silently drops scheduled settlements and in-progress deliveries. It also
pushes tests toward waiting on wall-clock time, which is how a suite becomes
slow and flaky.

**Why not a separate worker binary.** It is closer to a production topology and
would be right at a larger size. Here it means a second entrypoint, a second
deployment, and orchestrating two processes in the test harness — for a service
with three endpoints.

**Why not an external queue.** Excluded by the constraint that DummyPay depends
on no external system.

## Consequences

**Positive**

- The dual-write problem disappears. A payment cannot be committed without its
  event durably enqueued, because they are one transaction.
- Pending work survives a restart, which is exactly the property the goroutine
  approach lacks.
- Retry is not a special case. It re-enqueues, and the ordinary path runs.
- Tests call the worker function directly under a controlled clock
  ([ADR-0012](adr-0012-injected-clock-scheduler.md)) — the full
  `PROCESSING → APPROVED` transition is exercised with no sleeping.
- `SKIP LOCKED` means running more than one instance later requires no change.

**Negative**

- Polling costs latency and idle queries. Tuned by the poll interval; irrelevant
  at this scale, but it is a real cost and not free.
- The work table needs an index on `(state, due_at)` and, eventually, a
  retention policy. Without one it grows forever.
- **Delivery order is not guaranteed.** Two events for the same payment may
  arrive out of order. This is a property consumers must tolerate, and it has to
  be stated in the README rather than discovered.
- The worker shares a process with the API, so a pathological delivery can
  affect request latency.

## Compliance

- A test asserts no webhook is ever sent without a persisted delivery row
  written first.
- A test asserts that when enqueueing fails, the payment is not committed either
  — proving the transaction actually spans both writes.
- Worker tests run with the processing delay at zero and an injected clock, and
  contain no `time.Sleep`.

## Notes

Depends on `SKIP LOCKED` from
[ADR-0004](adr-0004-postgresql-pgx-hand-written-sql.md). If the service ever
needs guaranteed per-payment ordering, that is a superseding decision, not a
tweak.
