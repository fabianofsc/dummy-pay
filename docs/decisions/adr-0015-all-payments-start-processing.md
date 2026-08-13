# ADR-0015. Start every payment in PROCESSING and settle through the outbox

- **Status:** Accepted
- **Date:** 2026-08-12
- **Supersedes:** ADR-0008
- **Superseded by:** —

## Context

ADR-0008 limited asynchronous settlement to the two `card_processing_*`
tokens. Consequently, `card_approved` and `card_declined` returned a terminal
status synchronously, while the other tokens returned `PROCESSING`. An
integration could not use one uniform creation response, and it could observe
some terminal outcomes without receiving a corresponding settlement webhook.

The product contract now requires the initial response to every accepted
payment to be `PROCESSING`. The scenario token still selects the deterministic
terminal outcome; it must not select whether that outcome is synchronous.

## Decision

Every payment is persisted as `PROCESSING` and gets a `SETTLE_PAYMENT` outbox
row due at `now + DUMMYPAY_PROCESSING_DELAY`. The existing worker performs the
only terminal transition:

- `card_approved` and `card_processing_approved` settle to `APPROVED`.
- `card_declined` and `card_processing_declined` settle to `REJECTED`.

The creation response remains `201 Created` with `status: "PROCESSING"`.
Settlement creates the configured terminal webhook delivery, so the consumer
learns `APPROVED` or `REJECTED` through the callback. The optional
`payment.processing` event remains available for subscriptions that request it.

This supersedes ADR-0008 while retaining its outbox, transactional-write, and
`FOR UPDATE SKIP LOCKED` mechanics unchanged.

## Consequences

**Positive**

- All callers receive one creation lifecycle and handle terminal outcomes at
  the same webhook boundary.
- Every terminal status is produced by the same durable worker path, including
  restart recovery and deterministic clock-driven tests.
- The four tokens remain opaque deterministic scenarios and still accept no
  cardholder data.

**Negative**

- `card_approved` and `card_declined` no longer provide an immediate terminal
  result, so existing callers and test collections must expect `PROCESSING`.
- Even deterministic terminal outcomes wait for the configured processing
  delay and worker poll before their callback is delivered.
- Consumers subscribing to `payment.processing` can receive an additional
  lifecycle event before the terminal callback.

## Compliance

- Domain tests prove every token starts in `PROCESSING`, schedules settlement,
  and reaches its expected terminal status.
- Acceptance tests use the production HTTP router, real PostgreSQL, the
  injected clock, and the worker to prove both formerly synchronous tokens
  return `PROCESSING` before settling.
- The Insomnia collection asserts `PROCESSING` for every create response.

## Notes

ADR-0012 continues to govern the injected clock and `due_at` scheduling. No
new dependency, external service, or timer is introduced.
