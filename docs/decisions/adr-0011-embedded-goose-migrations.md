# ADR-0011. Version the schema with goose migrations embedded in the binary

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

The schema must be versioned. Two questions follow: which tool, and who applies
the migrations.

The second matters more. If migrations are applied by a separate command, the
binary and the schema can disagree — a deploy that skips the step leaves the
service running against a schema it does not expect. It also means "clone and
run" requires installing a migration tool first, which works against an
acceptance criterion of this project.

## Decision

`goose`, with plain SQL migration files embedded via `embed.FS` and applied by
the service binary at startup, before it begins serving.

The test harness applies migrations from the same embedded filesystem, so test
schemas are built by the same path as production ones.

**Why goose over golang-migrate.** Plain SQL files with up and down in a single
file, and a straightforward embedding story. golang-migrate would serve equally
well. The choice is recorded so it is not reopened, not because it is decisive.

**Why the binary applies them.** No version skew between code and schema, and no
extra tool to install. Cloning the repository and running it is enough.

## Consequences

**Positive**

- Schema and binary ship together and cannot drift.
- Local setup stays a single command, which is an explicit project goal.
- Tests build their schema through the same migrations the service uses, so a
  test can never pass against a schema production will not have.
- No migration tool in the CI image.

**Negative**

- Migrating at startup fits a single instance. With several instances starting
  together, goose's lock serialises them, but a slow migration blocks every
  startup behind it. Acceptable for a single-instance test double; running
  multiple instances would need a superseding decision.
- Rollback is a manual operation, not part of any automated path.
- Embedded migrations mean fixing a bad migration requires a new binary.

## Compliance

The integration test harness applies migrations from the embedded filesystem,
never from disk. A test asserts that migrating up, down, and up again arrives at
the expected schema, so down migrations are exercised rather than assumed.

## Notes

Related: [ADR-0013](adr-0013-integration-tests-real-postgres.md), which builds
each test schema this way.
