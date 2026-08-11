# ADR-0004. Use PostgreSQL through pgx with hand-written SQL

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

DummyPay owns its database and depends on no external system. What remains open
is which database and which access layer.

The choice is not incidental, because the hardest guarantees in this service are
database guarantees, not application logic:

- Idempotency resolves a race between two concurrent requests carrying the same
  key. The resolution is a unique constraint plus `INSERT … ON CONFLICT`, not an
  application mutex — a mutex only works within one process.
- The outbox worker must claim pending work without two workers taking the same
  row. That is `SELECT … FOR UPDATE SKIP LOCKED`.
- A payment and the event it produces must be committed together or not at all.
  That is one transaction spanning two tables.

An engine without these primitives cannot implement the contract, and an access
layer that hides them makes the implementation unreviewable.

## Decision

PostgreSQL, accessed with `pgx/v5` directly — not through `database/sql` — with
SQL written by hand and confined to `internal/postgres`.

**Why not SQLite or an embedded engine.** It would make tests trivial to run,
which is tempting. It also lacks `SKIP LOCKED` and has different concurrency
semantics, so the properties under test would not be the properties shipped. A
test double whose own concurrency behaviour is untested is not worth much.

**Why pgx rather than `database/sql`.** Native protocol support for `uuid`,
`timestamptz`, and arrays without scanning through `interface{}`, better
performance, and direct access to PostgreSQL features rather than the lowest
common denominator of a generic driver interface.

**Why not an ORM.** The handful of queries that carry the weight in this service
are precisely the ones ORMs express badly: `ON CONFLICT DO NOTHING … RETURNING`,
`FOR UPDATE SKIP LOCKED`, and a two-table transactional write. Expressing them
through a query builder would obscure the exact semantics a reviewer needs to
check.

**Why not sqlc.** A reasonable alternative — generated, type-checked query
methods from plain SQL. Rejected because the query set is small enough that
generation buys little, and it adds a code-generation step to the toolchain of a
project whose acceptance criterion is that it clones and runs.

## Consequences

**Positive**

- The concurrency-critical SQL is visible in the repository and can be reviewed
  as SQL, which is how it has to be reasoned about anyway.
- `SKIP LOCKED` gives the outbox worker safe polling with no extra coordination.
- No hidden query behaviour: no lazy loading, no N+1 emerging from a relation.

**Negative**

- Rows are mapped to structs by hand. It is repetitive and a place for mistakes.
- A malformed query fails at runtime rather than at compile time. This is the
  real cost of rejecting sqlc, and it is paid for by requiring every repository
  method to have an integration test.
- `internal/postgres` would be rewritten wholesale to change engines. Accepted:
  the engine is a fixed requirement, not a variable.

## Compliance

Every exported repository method has at least one test against a real PostgreSQL
instance ([ADR-0013](adr-0013-integration-tests-real-postgres.md)). No SQL exists
outside `internal/postgres`, which follows from the dependency test in
[ADR-0003](adr-0003-lean-hexagonal-architecture.md).

## Notes

Related: [ADR-0007](adr-0007-idempotency-unique-constraint.md) and
[ADR-0008](adr-0008-outbox-in-process-worker.md) both depend on the primitives
named here.
