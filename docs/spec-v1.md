# DummyPay V1 — Design Specification

This is the implementation contract. The [README](../README.md) describes the
service from the outside; this document describes how it is built, precisely
enough to implement and test without further decisions.

The reasoning behind the choices below is not repeated here. It lives in
[docs/decisions/](decisions/), and each section links to the relevant record.

---

## 1. Package layout

```
cmd/dummypay/            wiring and startup; the only place adapters are constructed
internal/payment/        domain, use cases, ports              (imports no adapter)
internal/webhook/        event construction, HMAC signing, outbound HTTP client
internal/postgres/       pgx adapter: repositories, outbox, transactions
internal/http/           chi adapter: routing, decoding, validation, status mapping
internal/config/         environment parsing and validation
internal/clock/          real clock; test clock lives in the test build
migrations/              goose SQL files, embedded
```

`internal/payment` declares its ports as interfaces and imports nothing under
`internal/http`, `internal/postgres`, or `internal/webhook`
([ADR-0003](decisions/adr-0003-lean-hexagonal-architecture.md)).

`internal/webhook` is a nuance worth stating: event construction and signing are
pure logic and could live in the domain, but the outbound HTTP client is an
adapter. They are kept in one package because the signature is computed over the
exact bytes the client sends, and splitting them invites the two halves to
disagree about what "the body" is. The domain depends on a `webhook.Sender` port
it declares itself, not on this package.

---

## 2. Domain model

### Payment

| Field | Type | Notes |
| --- | --- | --- |
| `ID` | `uuid.UUID` | UUIDv7, generated before insert |
| `AccountID` | `uuid.UUID` | The single technical account |
| `ReferenceID` | `string` | Opaque, caller-supplied, not interpreted |
| `Amount` | `int64` | Cents, strictly positive |
| `Currency` | `Currency` | `BRL` only |
| `Token` | `ScenarioToken` | One of four |
| `Status` | `Status` | `APPROVED`, `REJECTED`, `PROCESSING` |
| `ProviderTransactionID` | `uuid.UUID` | UUIDv7, generated with the payment |
| `CreatedAt`, `UpdatedAt` | `time.Time` | From the injected clock |

**Invariants.** Amount is greater than zero. Currency is `BRL`. Token is one of
the four known values. A payment in a terminal status never changes status
again.

### Scenario tokens

The token determines the outcome completely. This is the whole simulation
([ADR-0002](decisions/adr-0002-no-cardholder-data.md)).

| Token | Status at creation | Settles to |
| --- | --- | --- |
| `card_approved` | `APPROVED` | — |
| `card_declined` | `REJECTED` | — |
| `card_processing_approved` | `PROCESSING` | `APPROVED` |
| `card_processing_declined` | `PROCESSING` | `REJECTED` |

Any other value is a validation error. There is no default and no fallback.

### Payment state machine

```
                  card_approved
   (new) ─────────────────────────────────▶ APPROVED   (terminal)
     │            card_declined
     ├─────────────────────────────────────▶ REJECTED   (terminal)
     │
     │  card_processing_approved            settle
     ├────────────────────────▶ PROCESSING ────────────▶ APPROVED
     │  card_processing_declined            settle
     └────────────────────────▶ PROCESSING ────────────▶ REJECTED
```

`PROCESSING` is the only non-terminal status. The only transition out of it is
performed by the settlement worker. Attempting any other transition is a
programming error and panics in tests, returns an error in production.

### Events

| Trigger | Event type |
| --- | --- |
| Payment created as `APPROVED` | `payment.approved` |
| Payment created as `REJECTED` | `payment.rejected` |
| Payment created as `PROCESSING` | `payment.processing` |
| Settlement to `APPROVED` | `payment.approved` |
| Settlement to `REJECTED` | `payment.rejected` |

A payment created with a `card_processing_*` token therefore produces two
events. Events are produced only when an active subscription exists **and** it
lists that event type; otherwise no delivery is recorded and nothing is sent.

---

## 3. Data model

All timestamps are `timestamptz`. All identifiers are `uuid`
([ADR-0006](decisions/adr-0006-uuidv7-identifiers.md)); the `pay_`/`txn_`/`sub_`/
`evt_`/`dlv_` prefixes exist only in JSON and never in a column.

### `accounts`

```sql
id          uuid PRIMARY KEY,
key_id      text NOT NULL UNIQUE,
created_at  timestamptz NOT NULL
```

Seeded at startup from `DUMMYPAY_ACCOUNT_KEY_ID` with an upsert on `key_id`,
returning the id, which is cached for the life of the process. The secret is
**not** stored — authentication compares against the environment value directly
([ADR-0010](decisions/adr-0010-configuration-from-environment.md)).

The table exists so that the constraints below have a real foreign key to hang
on, not because multi-account is planned. It is not.

### `payments`

```sql
id                      uuid PRIMARY KEY,
account_id              uuid NOT NULL REFERENCES accounts(id),
reference_id            text NOT NULL,
amount_cents            bigint NOT NULL CHECK (amount_cents > 0),
currency                text NOT NULL CHECK (currency = 'BRL'),
payment_token           text NOT NULL,
status                  text NOT NULL CHECK (status IN ('APPROVED','REJECTED','PROCESSING')),
provider_transaction_id uuid NOT NULL UNIQUE,
created_at              timestamptz NOT NULL,
updated_at              timestamptz NOT NULL
```

The `CHECK` constraints duplicate domain validation on purpose. Validation
belongs in the domain; the constraints exist so that a bug cannot persist an
impossible row.

### `idempotency_keys`

```sql
account_id           uuid NOT NULL REFERENCES accounts(id),
idempotency_key      text NOT NULL,
request_fingerprint  bytea NOT NULL,
state                text NOT NULL CHECK (state IN ('IN_FLIGHT','COMPLETED')),
payment_id           uuid REFERENCES payments(id),
response_status      integer,
response_body        bytea,
claimed_at           timestamptz NOT NULL,
completed_at         timestamptz,
PRIMARY KEY (account_id, idempotency_key)
```

The primary key **is** the concurrency control
([ADR-0007](decisions/adr-0007-idempotency-unique-constraint.md)).

`response_body` is stored as raw bytes, not `jsonb`, so a replay returns exactly
what the original returned rather than a re-serialisation of it.

### `webhook_subscriptions`

```sql
id                uuid PRIMARY KEY,
account_id        uuid NOT NULL REFERENCES accounts(id),
url               text NOT NULL,
events            text[] NOT NULL,
secret_ciphertext bytea NOT NULL,
secret_nonce      bytea NOT NULL,
active            boolean NOT NULL DEFAULT true,
created_at        timestamptz NOT NULL

CREATE UNIQUE INDEX ON webhook_subscriptions (account_id) WHERE active;
```

The partial unique index enforces "one active subscription per account" as a
database constraint rather than an application check
([ADR-0009](decisions/adr-0009-webhook-secret-encryption.md)).

### `webhook_deliveries`

```sql
id                uuid PRIMARY KEY,
subscription_id   uuid NOT NULL REFERENCES webhook_subscriptions(id),
payment_id        uuid NOT NULL REFERENCES payments(id),
event_id          uuid NOT NULL UNIQUE,
event_type        text NOT NULL,
payload           bytea NOT NULL,
status            text NOT NULL CHECK (status IN ('PENDING','SENT','FAILED')),
attempt_count     integer NOT NULL DEFAULT 0,
last_attempted_at timestamptz,
last_http_status  integer,
created_at        timestamptz NOT NULL
```

`payload` is `bytea`, holding the exact serialised bytes that will be sent.
This is not incidental. The signature is an HMAC over the raw body, so a retry
must transmit bytes identical to the first attempt. Storing `jsonb` would
normalise key order and whitespace, and the retried signature would not match
the retried body in the way a consumer computed it the first time.

### `outbox_work`

```sql
id          uuid PRIMARY KEY,
kind        text NOT NULL CHECK (kind IN ('SETTLE_PAYMENT','DELIVER_WEBHOOK')),
subject_id  uuid NOT NULL,
due_at      timestamptz NOT NULL,
state       text NOT NULL CHECK (state IN ('PENDING','DONE','FAILED')),
claimed_at  timestamptz,
last_error  text,
created_at  timestamptz NOT NULL

CREATE INDEX ON outbox_work (state, due_at) WHERE state = 'PENDING';
```

`subject_id` references a payment for `SETTLE_PAYMENT` and a delivery for
`DELIVER_WEBHOOK`. It is deliberately not a foreign key, because the column
points at two different tables depending on `kind`.

**Division of responsibility.** `outbox_work` answers "what should happen and
when". `webhook_deliveries` holds delivery state — status, attempt count, last
attempt, last HTTP status — because that is what the contract exposes. Attempt
counting lives in the delivery row only; the work row is consumed by a single
attempt and then finished.

---

## 4. Request flows

### 4.1 `POST /v1/payments`

**Validation, before any database work.** Basic credentials match the
configured account, otherwise `401`. `Idempotency-Key` present and non-empty,
otherwise `400`. Body decodes with unknown fields rejected, otherwise `400`.
Amount positive, currency `BRL`, token known, otherwise `422`.

**Fingerprint.** SHA-256 over a canonical form of the validated request: the
four fields serialised in a fixed order with no insignificant whitespace.
Canonicalising the *validated* struct rather than the raw bytes means a retry
that differs only in key order or formatting is correctly treated as the same
request.

**Claim.** In one statement:

```sql
INSERT INTO idempotency_keys (account_id, idempotency_key, request_fingerprint,
                              state, claimed_at)
VALUES ($1, $2, $3, 'IN_FLIGHT', $4)
ON CONFLICT (account_id, idempotency_key) DO NOTHING
RETURNING account_id
```

**If a row was returned — this request owns the operation.** Within a single
transaction: insert the payment with the status the token dictates; if the
status is `PROCESSING`, insert a `SETTLE_PAYMENT` work row due at
`now + DUMMYPAY_PROCESSING_DELAY`; create the delivery row and a
`DELIVER_WEBHOOK` work row due immediately, if an active subscription covers the
event type; and update the idempotency row to `COMPLETED` with the payment id,
response status and response body. Commit.

Everything above is one transaction. A payment cannot exist without its event
enqueued, and cannot exist without its idempotency record completed
([ADR-0008](decisions/adr-0008-outbox-in-process-worker.md)).

**If no row was returned — someone else owns it.** Read the existing row and
branch:

| Existing row | Response |
| --- | --- |
| `request_fingerprint` differs | `422 idempotency_key_reuse` |
| `state = 'COMPLETED'` | Stored `response_status` and `response_body`, verbatim |
| `state = 'IN_FLIGHT'`, within lease | `409 idempotency_conflict` |
| `state = 'IN_FLIGHT'`, lease expired | Reclaim: take ownership and proceed as owner |

The fingerprint is checked **first**. A mismatched body is a client error
regardless of what the original request is doing.

Reclaiming an expired lease is a conditional update — `UPDATE … WHERE state =
'IN_FLIGHT' AND claimed_at < $lease_cutoff` — so two requests racing to reclaim
the same abandoned key resolve the same way the original claim did: one wins,
the other re-reads.

### 4.2 `POST /v1/webhook-subscriptions`

Authenticate. Validate that `url` parses as an absolute HTTP or HTTPS URL and
that `events` is non-empty and contains only known event types.

Generate a secret from `crypto/rand`, rendered as `whsec_` followed by 32 bytes
in base64url. Encrypt with AES-256-GCM under the configured key with a fresh
random nonce. Insert; the partial unique index rejects a second active
subscription with `409 subscription_exists`.

Return the plaintext secret in this response and never again.

### 4.3 `POST /v1/webhook-deliveries/{delivery_id}/retry`

Authenticate. Parse and verify the `dlv_` prefix; a well-formed UUID with the
wrong prefix is `404`, not a lookup. Load the delivery, scoped to the account.

If its status is `SENT`, respond `409` — there is nothing to retry. Otherwise
insert a `DELIVER_WEBHOOK` work row due immediately, set the delivery to
`PENDING`, and return its current state.

Retry re-enqueues; it does not send. The attempt travels the ordinary worker
path.

---

## 5. The worker

A single loop, started by `cmd/dummypay`, that on each tick claims and processes
due work:

```sql
UPDATE outbox_work SET state = 'DONE', claimed_at = $now
WHERE id IN (
  SELECT id FROM outbox_work
  WHERE state = 'PENDING' AND due_at <= $now
  ORDER BY due_at
  LIMIT $batch
  FOR UPDATE SKIP LOCKED
)
RETURNING id, kind, subject_id
```

`SKIP LOCKED` means two workers never claim the same row, so running more than
one instance needs no change ([ADR-0004](decisions/adr-0004-postgresql-pgx-hand-written-sql.md)).

**`SETTLE_PAYMENT`.** Load the payment. If it is no longer `PROCESSING`, do
nothing — the work is idempotent. Otherwise transition it to the status its
token dictates and, in the same transaction, create the delivery and its
`DELIVER_WEBHOOK` work row if a subscription covers the event.

**`DELIVER_WEBHOOK`.** Load the delivery, increment `attempt_count`, POST
`payload` to the subscription URL with the `X-Webhook-Signature` header and a
bounded timeout. A 2xx sets `SENT`; anything else — including a transport error,
where `last_http_status` stays null — sets `FAILED`. Either way
`last_attempted_at` and `last_http_status` are recorded and the work row is
finished.

**There is no automatic retry in V1.** A `FAILED` delivery stays failed until
`POST /v1/webhook-deliveries/{id}/retry` is called. This is what the contract
asks for, and it keeps failure visible rather than absorbed by a backoff nobody
is watching. Automatic retry would be a superseding decision, not a tweak.

**Ordering is not guaranteed.** Batch claiming and independent attempts mean two
events for one payment can arrive out of order. Consumers must tolerate it; the
README says so.

### Determinism

The worker is a method that processes one batch and returns
([ADR-0012](decisions/adr-0012-injected-clock-scheduler.md)). `cmd/dummypay`
wraps it in a ticker. Tests call it directly, after advancing a test clock. No
test starts the loop, and no test sleeps.

---

## 6. Webhook payload and signature

Body, serialised with a fixed field order:

```json
{
  "event_id": "evt_0199a1f4-4b17-70f2-a35d-8c1e64907bda",
  "type": "payment.approved",
  "created_at": "2026-08-10T12:00:00Z",
  "data": {
    "payment_id": "pay_0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b",
    "reference_id": "checkout:123",
    "status": "APPROVED",
    "provider_transaction_id": "txn_0199a1f4-3c83-7a04-8f21-6d3b0e57c91a"
  }
}
```

Timestamps are RFC 3339 in UTC.

`X-Webhook-Signature` is the lowercase hex encoding of
`HMAC-SHA-256(secret, payload_bytes)`, where `payload_bytes` are the exact bytes
stored in `webhook_deliveries.payload` and written to the request body, and
`secret` is the decrypted plaintext secret.

The bytes are serialised once, at delivery creation, and stored. Signing and
sending both read that stored value. Nothing re-serialises.

---

## 7. Error model

```json
{ "code": "invalid_amount", "message": "amount must be a positive integer of cents" }
```

| Code | HTTP | Cause |
| --- | --- | --- |
| `invalid_request` | 400 | Malformed JSON, unknown field, missing `Idempotency-Key` |
| `unauthorized` | 401 | Missing or wrong credentials |
| `not_found` | 404 | Unknown or wrongly-prefixed identifier |
| `idempotency_conflict` | 409 | Same key, first request still in flight |
| `subscription_exists` | 409 | An active subscription already exists |
| `delivery_not_retryable` | 409 | Delivery is already `SENT` |
| `invalid_amount` | 422 | Amount not a positive integer |
| `unsupported_currency` | 422 | Currency other than `BRL` |
| `unknown_payment_token` | 422 | Token outside the known four |
| `idempotency_key_reuse` | 422 | Same key, different body |
| `internal_error` | 500 | Anything unhandled |

`401` responses carry no detail about which part of the credential failed.
`500` bodies never include the underlying error; it is logged with the request
ID instead.

---

## 8. Ports

Declared by `internal/payment`, in domain vocabulary:

| Port | Purpose |
| --- | --- |
| `TxManager` | `Within(ctx, func(ctx) error) error` — everything in one transaction |
| `PaymentRepository` | Insert, load, transition status |
| `IdempotencyStore` | Claim, read, complete, reclaim expired |
| `OutboxWriter` | Enqueue work with a due time |
| `SubscriptionRepository` | Load the active subscription and its decrypted secret |
| `DeliveryRepository` | Create, load, record an attempt |
| `Sender` | Deliver signed bytes to a URL, return HTTP status or transport error |
| `Clock` | `Now()` |
| `IDGenerator` | UUIDv7 |

`TxManager` carries the transaction in the context, so repository methods take
`ctx` and nothing transaction-shaped appears in a domain signature.

---

## 9. Configuration

| Variable | Required | Default | Validation |
| --- | --- | --- | --- |
| `DUMMYPAY_HTTP_ADDR` | no | `:8080` | Parses as a listen address |
| `DUMMYPAY_DATABASE_URL` | yes | — | Parses as a PostgreSQL DSN |
| `DUMMYPAY_ACCOUNT_KEY_ID` | yes | — | Non-empty |
| `DUMMYPAY_ACCOUNT_KEY_SECRET` | yes | — | At least 16 characters |
| `DUMMYPAY_WEBHOOK_SECRET_ENC_KEY` | yes | — | base64, exactly 32 bytes decoded |
| `DUMMYPAY_PROCESSING_DELAY` | no | `3s` | Go duration, not negative |
| `DUMMYPAY_IDEMPOTENCY_LEASE` | no | `30s` | Go duration, positive |
| `DUMMYPAY_WORKER_POLL_INTERVAL` | no | `250ms` | Go duration, positive |
| `DUMMYPAY_WEBHOOK_TIMEOUT` | no | `5s` | Go duration, positive |

Parsed once into an immutable struct and fully validated before the listener
binds. A missing or invalid required variable stops the process with a message
naming it.

---

## 10. Testing

Assertions use `testify/require` for control flow and `cmp.Diff` for struct
comparison, never `require.Equal` on a timestamped struct
([ADR-0014](decisions/adr-0014-testify-require-and-go-cmp.md)). Fakes are
written by hand against the ports in §8.

**Domain and use-case tests** run against in-memory fakes with a test clock. No
database, no HTTP. These cover the state machine, token mapping, validation, and
event selection.

**HTTP tests** exercise the chi router with `httptest`, against fakes, covering
status mapping, decoding, prefix handling, and authentication.

**Integration tests** run against real PostgreSQL, one schema each
([ADR-0013](decisions/adr-0013-integration-tests-real-postgres.md)), and cover
every repository method plus the two guarantees a fake cannot prove: concurrent
claims on one idempotency key, and two workers not claiming the same work row.

**Webhook tests** use an `httptest.Server` as the consumer — loopback, not
external network — and verify that the signature computed over the received raw
body matches, that a non-2xx marks the delivery `FAILED`, and that a retry
re-sends byte-identical bytes with an incremented attempt count.

The acceptance list from the README maps to tests as follows: approved,
declined, both `PROCESSING` scenarios, idempotent replay, key reuse with a
different body, concurrent duplicate requests, expired lease reclamation, HMAC
correctness, delivery failure, and successful retry.

---

## 11. Deliberately excluded from V1

Automatic webhook retry with backoff. Key rotation for webhook secrets.
Deleting or updating a subscription. Listing payments or deliveries. Retention
or pruning of `outbox_work`. Multiple accounts. Everything already listed as out
of scope in the README.

Each is absent by decision. Adding any of them is a new ADR, not a patch.
