# ADR-0010. Take all configuration from the environment

- **Status:** Accepted
- **Date:** 2026-08-10
- **Supersedes:** —
- **Superseded by:** —

## Context

The service needs a listen address, a database DSN, technical account
credentials, a webhook secret encryption key, and the processing delay. Three of
those are secrets.

The usual mixed approach — a config file for structure, environment variables
for secrets — creates two places to look and, more importantly, a file that
someone will eventually fill in with real values and commit. That is the most
common way credentials reach a repository.

## Decision

Every configuration value comes from an environment variable. No configuration
file is read at runtime.

Values are parsed once at startup into an immutable struct, fully validated
before the server binds a port. A missing or malformed required variable stops
the process with a message naming the variable.

`.env.example` is committed as documentation. It lists every variable with
placeholder values and no real ones.

## Consequences

**Positive**

- A secret cannot be committed by accident, because no file is read. The
  protection is structural rather than a rule people have to remember.
- Identical mechanism on a laptop, in docker compose, and in CI.
- Failure is at startup with a clear message, rather than a nil dereference on
  the first payment.
- One struct definition documents everything that is configurable.

**Negative**

- Several variables to set by hand for local development. Mitigated by
  `.env.example` and by compose supplying them.
- No runtime reconfiguration; any change needs a restart. Acceptable here.
- Environment variables are visible in process listings and in some container
  inspection output. On a shared host this is a real exposure, and it is the
  cost of not using a secret manager — which is itself excluded by the project's
  independence constraint.

## Compliance

- A startup test asserts that omitting each required variable produces a clear
  error and a non-zero exit.
- The configuration struct is constructed only under `cmd/`, so no package reads
  the environment on its own.
- `.env.example` is checked to contain placeholders only.

## Notes

Related: [ADR-0009](adr-0009-webhook-secret-encryption.md), which depends on the
encryption key arriving through a variable separate from the database DSN.
