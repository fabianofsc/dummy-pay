# Architecture Decision Records

Every architecturally significant decision in DummyPay is recorded here. The
format and the rules for keeping it are set by
[ADR-0001](adr-0001-record-architecture-decisions.md).

Read these before proposing a change to the stack or the scope. Most of what
looks missing in this service is missing on purpose, and the reason is written
down.

| # | Decision | Status |
| --- | --- | --- |
| [0001](adr-0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](adr-0002-no-cardholder-data.md) | Accept no cardholder data; select outcomes with scenario tokens | Accepted |
| [0003](adr-0003-lean-hexagonal-architecture.md) | Structure the service as a lean hexagonal architecture | Accepted |
| [0004](adr-0004-postgresql-pgx-hand-written-sql.md) | Use PostgreSQL through pgx with hand-written SQL | Accepted |
| [0005](adr-0005-chi-http-router.md) | Use chi for HTTP routing | Accepted |
| [0006](adr-0006-uuidv7-identifiers.md) | Use UUIDv7 identifiers, prefixed at the API boundary | Accepted |
| [0007](adr-0007-idempotency-unique-constraint.md) | Implement idempotency with a unique constraint and explicit in-flight state | Accepted |
| [0008](adr-0008-outbox-in-process-worker.md) | Run asynchronous work from an outbox table with an in-process worker | Accepted |
| [0009](adr-0009-webhook-secret-encryption.md) | Encrypt webhook secrets with AES-256-GCM under a separate environment key | Accepted |
| [0010](adr-0010-configuration-from-environment.md) | Take all configuration from the environment | Accepted |
| [0011](adr-0011-embedded-goose-migrations.md) | Version the schema with goose migrations embedded in the binary | Accepted |
| [0012](adr-0012-injected-clock-scheduler.md) | Inject the clock and express scheduling as data | Accepted |
| [0013](adr-0013-integration-tests-real-postgres.md) | Test against a real PostgreSQL from docker compose, one schema per test | Accepted |
| [0014](adr-0014-testify-require-and-go-cmp.md) | Assert with testify/require and compare structs with go-cmp | Accepted |

## Writing a new one

Copy the section structure from any existing record: Context, Decision,
Consequences (positive and negative), Compliance, Notes.

Two rules carry most of the weight:

- **Give the product reason, not only the technical one.** A decision justified
  purely on technical grounds gets reopened by the next person who weighs those
  factors differently.
- **Write the negative consequences honestly.** An ADR listing only benefits is
  not a decision record, it is an advertisement, and it will not help anyone
  decide whether the situation has changed enough to revisit it.

To reverse a decision, add a new ADR and mark the old one
`Superseded by ADR-NNNN`. Never edit a superseded record into agreement with the
present — the chain is the history.
