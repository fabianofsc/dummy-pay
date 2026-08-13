# Logging and Portuguese Student Docs Design

## Goal

Make the operational lifecycle visible in service logs and present the primary
student-facing documentation in Brazilian Portuguese.

## Logging Design

The HTTP request logger remains responsible for one line per inbound request,
including `GET /health`. The payment domain receives a narrow `Logger` port
through `WorkerDeps`, so its worker can report settlement and webhook delivery
without importing `log`, HTTP adapters, or PostgreSQL adapters.

The port exposes one method for a preformatted message. The `cmd/dummypay`
composition root injects a standard-library adapter. Worker logs will contain:

- a settlement after the payment is persisted: payment identifier, scenario
  token, and terminal status;
- each delivery after its attempt record is persisted: delivery identifier,
  payment identifier, event type, resulting delivery status, HTTP status, and
  transport error when one occurred.

No log line will include a webhook payload, HMAC signature, subscription
secret, authorization header, or callback URL.

## Documentation Design

Translate `README.md`, `docs/smoke-test.md`, `docs/pitch.md`, and the Insomnia
collection to Brazilian Portuguese. Keep HTTP paths, JSON keys and values,
environment-variable names, Make targets, CLI commands, identifiers, and
status/event literals unchanged. In the collection, request titles and
assertion descriptions are translated, while the executable Insomnia API calls
and their expected machine values are preserved.

## Verification

Per the user's direction, no tests validate log messages. The existing worker
suite, linter, collection syntax validation, and diff check validate the
change; the existing HTTP middleware continues to log `/health` and every
other inbound request.
