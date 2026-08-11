# ADR-0007. Implement idempotency with a unique constraint and explicit in-flight state

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

`POST /v1/payments` must be safe to retry, with three distinct behaviours:

| Situation | Required response |
| --- | --- |
| Same key, same body, first call finished | The original transaction |
| Same key, different body | No new payment (422) |
| Same key, first call still in flight | 409 |

The naive implementation — read the key, and insert if absent — is a race. Two
requests can both find nothing and both insert, producing two charges. This is
the single most consequential correctness requirement in V1: getting it wrong
means double-charging, which is the failure real integrations exist to prevent.

The third row is the subtle one. Distinguishing "still running" from "finished"
cannot be inferred from the presence of a record; it has to be recorded.

## Decision

An idempotency record keyed by `(account_id, idempotency_key)` under a unique
constraint, holding:

- a **request fingerprint** — a hash over the canonicalised request body,
- a **state** — `IN_FLIGHT` or `COMPLETED`,
- the resulting **payment reference**, once there is one,
- a **claimed-at timestamp**.

Creation attempts `INSERT … ON CONFLICT DO NOTHING RETURNING`. The database, not
the application, decides who wins:

- **Insert succeeded.** This request owns the operation. It creates the payment
  and marks the record `COMPLETED` in the same transaction, so the payment and
  its idempotency record are never out of step.
- **Insert returned nothing.** Another request owns it. Read the existing row
  and branch on what it says: fingerprint mismatch → 422; `IN_FLIGHT` → 409;
  `COMPLETED` → replay the stored response.

An `IN_FLIGHT` record older than a configurable lease is treated as abandoned
and may be reclaimed.

The fingerprint is computed over a canonical form of the body — normalised key
order and whitespace — so that a semantically identical retry is not mistaken
for a conflicting one.

## Consequences

**Positive**

- The race is settled by the database. The guarantee holds across processes, not
  only across goroutines in one process, so it survives running more than one
  instance.
- 409 versus replay is a read of recorded state rather than an inference from
  timing.
- Payment creation and idempotency completion share a transaction, so there is
  no window where a payment exists that a retry cannot find.

**Negative**

- A process that dies mid-flight leaves an `IN_FLIGHT` record that returns 409
  until the lease expires. The lease is the fix, and its duration is a real
  trade-off: too short reintroduces the double-charge window, too long strands
  the key. It must be configurable and documented.
- Canonicalising the body for fingerprinting is easy to get subtly wrong, and
  getting it wrong produces spurious 422s on legitimate retries.
- The stored response must be persisted, not recomputed, or a replay could
  differ from the original.

## Compliance

Tests, all against a real database with genuine concurrency:

- Two concurrent requests with the same key produce exactly one payment and
  exactly one 409.
- Same key with a different body returns 422 and leaves the payment count
  unchanged.
- A replay returns a response identical to the original.
- An `IN_FLIGHT` record past its lease is reclaimed rather than returning 409
  forever.

The first of these cannot be satisfied by a fake repository, which is why
[ADR-0013](adr-0013-integration-tests-real-postgres.md) requires a real
PostgreSQL and rules out per-test transaction rollback.

## Notes

Depends on the primitives recorded in
[ADR-0004](adr-0004-postgresql-pgx-hand-written-sql.md).
