# ADR-0013. Test against a real PostgreSQL from docker compose, one schema per test

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

An acceptance criterion is that the service builds and its tests pass with no
external network access.

There is a tension with a second requirement. The guarantees in
[ADR-0007](adr-0007-idempotency-unique-constraint.md) and
[ADR-0008](adr-0008-outbox-in-process-worker.md) *are* database guarantees —
unique-constraint conflict resolution and `FOR UPDATE SKIP LOCKED`. A fake
repository would pass those tests without proving anything, because the fake
implements the behaviour the test asserts. That is the worst kind of green.

Options: testcontainers, docker compose with per-test isolation, fakes only, or
an embedded engine.

## Decision

A `docker-compose.yml` provides PostgreSQL. Each integration test creates its
own schema, applies the embedded migrations
([ADR-0011](adr-0011-embedded-goose-migrations.md)), and drops it afterwards.

Domain and use-case tests continue to run against in-memory fakes and need no
database at all.

When no database is reachable, integration tests skip with a message saying how
to start one, so `go test ./...` still works on a machine without Docker.

**Why not testcontainers.** More ergonomic — the test manages the container
itself. But the image must be pulled, which needs network on first run, in
direct tension with the acceptance criterion. Compose pulls once, and everything
after that is offline.

**Why not an embedded engine.** It would remove the Docker requirement entirely,
and it removes the semantics under test along with it.

**Why a schema per test rather than a transaction rolled back per test.**
Transaction-per-test is faster and is the usual choice. It cannot work here: the
concurrency test needs two genuinely concurrent, genuinely committed
transactions racing for the same idempotency key. Wrapping them in one
transaction would make the race impossible to reproduce — it would disable the
exact behaviour the test exists to verify.

## Consequences

**Positive**

- The semantics tested are the semantics shipped.
- Schema isolation lets tests run in parallel without interfering.
- Offline after the first image pull.

**Negative**

- Docker is required for full coverage, and a green `go test ./...` on a machine
  without it is a weaker signal than it appears. This is the main risk of the
  decision, and the compliance rule below exists to contain it.
- Creating and dropping a schema per test is slower than a rollback.
- Two test tiers — fakes and real database — means deciding which tier a new
  test belongs to.

## Compliance

CI sets an environment variable that turns skipping into failure. An integration
test can never pass silently in CI by being skipped — locally it may skip, in CI
it must run.

Every repository method has at least one test against real PostgreSQL, and the
concurrency test in
[ADR-0007](adr-0007-idempotency-unique-constraint.md) runs with real parallel
requests.

## Notes

Related: [ADR-0003](adr-0003-lean-hexagonal-architecture.md) — this decision is
what keeps the fakes that hexagonal architecture introduces from becoming a way
to prove things that are not true.
