# ADR-0003. Structure the service as a lean hexagonal architecture

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

The rules worth getting right in this service are the idempotency semantics, the
payment state machine, and the webhook delivery lifecycle. All three are to be
built test-first.

If the domain imports `pgx`, every test of those rules needs a database. That is
slow enough to discourage the tight red-green loop TDD depends on, and it creates
design pressure to express business rules as SQL, where they are hard to read and
harder to change.

The opposite failure is also available. A full Clean Architecture layout —
separate `entities`, `usecases`, `interfaces`, and `infrastructure` trees — is
more indirection than a three-endpoint service can carry. The ceremony would
exceed the logic.

## Decision

Four packages, with dependencies pointing one way:

- `internal/payment` — domain types, the state machine, and use cases. Defines
  its ports as Go interfaces. Imports no infrastructure package and no database
  driver.
- `internal/http` — the chi adapter: decoding, validation, status mapping.
- `internal/postgres` — the pgx adapter implementing the repository ports.
- `internal/webhook` — the outbound HTTP client and HMAC signing.
- `cmd/dummypay` — the only place that constructs concrete adapters and wires
  them together.

Ports are declared by the domain, in the domain's vocabulary, not by the
adapters.

## Consequences

**Positive**

- Domain and use-case tests run in memory in milliseconds, which is what makes
  test-first practical for the rules that matter.
- Replacing an adapter does not touch a business rule.
- The dependency direction is visible in the import list of one package, so
  erosion is detectable rather than a matter of taste.

**Negative**

- Each port costs an interface plus a fake. That is code whose only purpose is
  testability, and it is a real cost.
- Fakes can drift from PostgreSQL's actual behaviour. This matters most for
  idempotency, where the guarantee *is* a database guarantee — a fake would
  happily approve an implementation that races in production. Mitigated by
  [ADR-0013](adr-0013-integration-tests-real-postgres.md): the concurrency
  guarantees are proved against a real database, never against a fake.
- Over-abstraction is a live risk at this size. Ports exist only where there is
  a real boundary — persistence, outbound HTTP, clock — not for every type.

## Compliance

A CI test asserts that `internal/payment` imports nothing under `internal/http`,
`internal/postgres`, or `internal/webhook`, and no third-party driver or web
framework. Adapter construction appears only under `cmd/`.

## Notes

Related: [ADR-0005](adr-0005-chi-http-router.md) — the handler type was chosen
specifically so the HTTP framework cannot leak past this boundary.
