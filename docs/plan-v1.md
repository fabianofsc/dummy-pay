# DummyPay V1 — Implementation Plan

Derived from [spec-v1.md](spec-v1.md). Every step is test-first: the test is
written and seen to fail before the implementation exists.

Steps are ordered so that each one is independently verifiable and leaves the
suite green. Slices that need a database come after the ones that do not, so the
domain is settled before infrastructure has a chance to shape it.

Each step names its **Test first**, its **Then** (what to build), and its **Done
when** (the observable condition). "Done when" is not "I wrote the code" — it is
a command whose output can be read.

---

## Roadmap

Progress tracker. A box is ticked only when that step's **Done when** condition
has been observed — not when the code was written. Tick it in the same commit
that satisfies it, so the list is never a summary written after the fact.

`[ ]` not started · `[~]` in progress · `[x]` done

### Documentation

- [x] **D.1** — README with the full V1 contract
- [x] **D.2** — `docs/pitch.md`
- [x] **D.3** — 13 ADRs in `docs/decisions/` plus index
- [x] **D.4** — `docs/spec-v1.md` design specification
- [x] **D.5** — `docs/plan-v1.md` implementation plan
- [ ] **D.6** — README "Running locally" filled in, design-phase banner removed *(→ Step 11.3)*

### Phase 0 — Scaffolding

- [ ] **0.1** — Repository skeleton: `go mod`, packages, Makefile, compose, `.env.example`
- [ ] **0.2** — CI running build, vet, unit and integration tests

### Phase 1 — Foundations

- [ ] **1.1** — Configuration parsing and validation
- [ ] **1.2** — Clock and UUIDv7 generator
- [ ] **1.3** — Identifier prefix codec

### Phase 2 — Domain

- [ ] **2.1** — Value objects: amount, currency, scenario token
- [ ] **2.2** — Payment state machine
- [ ] **2.3** — Event selection

### Phase 3 — Persistence

- [ ] **3.1** — Migrations and integration test harness
- [ ] **3.2** — Account seeding
- [ ] **3.3** — Payment repository

### Phase 4 — Idempotency

- [ ] **4.1** — Request fingerprinting
- [ ] **4.2** — Claim and complete
- [ ] **4.3** — Concurrency: exactly one winner
- [ ] **4.4** — Lease reclamation

### Phase 5 — Create-payment use case

- [ ] **5.1** — Happy paths against fakes
- [ ] **5.2** — Idempotent paths: replay, reuse, conflict
- [ ] **5.3** — Atomicity under a failing outbox write

### Phase 6 — HTTP: create payment

- [ ] **6.1** — Routing and authentication
- [ ] **6.2** — Decoding, strict fields, validation mapping
- [ ] **6.3** — Response shape

### Phase 7 — Subscriptions

- [ ] **7.1** — AES-256-GCM secret encryption
- [ ] **7.2** — Create subscription endpoint

### Phase 8 — Webhook payload and signing

- [ ] **8.1** — Payload serialisation and HMAC
- [ ] **8.2** — Sender against `httptest`

### Phase 9 — Worker

- [ ] **9.1** — Claiming with `SKIP LOCKED`
- [ ] **9.2** — Settlement of `PROCESSING` payments
- [ ] **9.3** — Delivery attempts and status recording

### Phase 10 — Retry

- [ ] **10.1** — Retry endpoint, byte-identical resend

### Phase 11 — Assembly

- [ ] **11.1** — Wiring and startup
- [ ] **11.2** — Acceptance walkthrough
- [ ] **11.3** — README updated for a real clone-and-run

### Phase 12 — Fitness functions

- [ ] **12.1** — Dependency direction test
- [ ] **12.2** — Time discipline check
- [ ] **12.3** — Suite wall-clock budget

### Acceptance criteria

Tracked separately from the phases, because these are what the project is
judged on. Each is ticked when a test proves it end to end *(→ Step 11.2)*.

- [ ] Service starts and the suite passes with no external network
- [ ] `card_approved` → `APPROVED`
- [ ] `card_declined` → `REJECTED`
- [ ] `card_processing_approved` → `PROCESSING` then `APPROVED`
- [ ] `card_processing_declined` → `PROCESSING` then `REJECTED`
- [ ] Idempotent replay returns the original transaction
- [ ] Same key with a different body creates nothing
- [ ] Concurrent duplicate requests return 409
- [ ] HMAC signature verifies over the raw body
- [ ] Webhook failure is recorded as `FAILED` with attempt metadata
- [ ] Retry resends and succeeds
- [ ] API and local operation documented in the README
- [ ] `go test ./...` run before delivery

---

## Phase 0 — Scaffolding

### Step 0.1 — Repository skeleton

**Then:** `go mod init`; the package directories from spec §1 with a
`doc.go` each; a `Makefile` with `test`, `test-integration`, `lint`, `run`,
`db-up`, `db-down`; `docker-compose.yml` with PostgreSQL and a named volume;
`.env.example` listing every variable from spec §9 with placeholder values only.

**Done when:** `go build ./...` and `go test ./...` both succeed on an empty
tree, and `docker compose up -d` yields a reachable database.

### Step 0.2 — CI

**Then:** a workflow that runs build, vet, unit tests, and integration tests
with the database service running, exporting the variable that turns an
integration skip into a failure.

**Done when:** CI is green and deliberately breaking one test turns it red.

---

## Phase 1 — Foundations (no database, no HTTP)

### Step 1.1 — Configuration

**Test first:** each required variable, when absent or malformed, produces an
error naming that variable; defaults apply when optionals are absent; a
`WEBHOOK_SECRET_ENC_KEY` that is not 32 bytes after base64 decoding is rejected.

**Then:** `internal/config` — parse once into an immutable struct, validate
everything before returning.

**Done when:** the table-driven test covers every row of spec §9.

### Step 1.2 — Clock and identifiers

**Test first:** the test clock returns what it was set to and advances by
exactly what it was told; the generator produces valid UUIDv7 values that sort
in generation order.

**Then:** `internal/clock` with a real and a test implementation; a UUIDv7
generator behind the `IDGenerator` port.

**Done when:** a thousand generated identifiers are strictly increasing when
sorted as strings.

### Step 1.3 — Identifier prefixes

**Test first:** encoding produces `pay_<uuid>`; decoding accepts the matching
prefix; decoding a well-formed UUID with the *wrong* prefix returns an error;
decoding a malformed UUID returns an error.

**Then:** a small prefix codec used only at the HTTP boundary.

**Done when:** the wrong-prefix case is covered for all five prefixes.

---

## Phase 2 — Domain

### Step 2.1 — Value objects and validation

**Test first:** amount zero and negative are rejected; `USD` is rejected; `BRL`
is accepted; each of the four tokens parses; an unknown token is rejected with
`unknown_payment_token`.

**Then:** `Amount`, `Currency`, `ScenarioToken` in `internal/payment`, each
constructed through a validating function so an invalid value cannot exist.

**Done when:** no test can construct an invalid value object.

### Step 2.2 — Payment state machine

**Test first:** each token maps to its creation status per spec §2; each
`card_processing_*` token settles to the right terminal status; a transition out
of a terminal status is refused; settling a payment that is no longer
`PROCESSING` is a no-op rather than an error.

**Then:** the `Payment` type with its transitions.

**Done when:** every arrow in the spec §2 diagram has a test, including the
refused ones.

### Step 2.3 — Event selection

**Test first:** each status change produces the right event type; a subscription
that does not list the type produces no event; no subscription produces no
event; a `PROCESSING` payment produces exactly two events across its lifetime.

**Then:** event construction as pure logic.

**Done when:** the mapping table in spec §2 is covered row by row.

---

## Phase 3 — Persistence

### Step 3.1 — Migrations and test harness

**Test first:** the harness creates a schema, applies embedded migrations, and
drops it; the same test run twice in parallel does not interfere.

**Then:** goose migrations for all six tables from spec §3, embedded via
`embed.FS`; a test helper returning a connection scoped to a fresh schema, that
skips with a clear message when no database is reachable and fails instead of
skipping when the CI variable is set.

**Done when:** `make test-integration` builds and drops schemas cleanly, and
migrating up, down, and up again reaches the same schema.

### Step 3.2 — Account seeding

**Test first:** startup with a new `key_id` creates a row; startup again with
the same `key_id` returns the same id rather than a second row.

**Then:** the upsert described in spec §3.

**Done when:** the repeat-startup test passes against a real database.

### Step 3.3 — Payment repository

**Test first:** insert then load returns an equal payment; the amount and
currency constraints reject invalid rows even when the domain is bypassed; a
status transition persists and updates `updated_at`.

**Then:** `internal/postgres` payment repository.

**Done when:** every method has an integration test.

---

## Phase 4 — Idempotency

This is the highest-risk slice in the project. It gets its own phase and its
tests run against a real database, because the guarantee is a database guarantee
([ADR-0007](decisions/adr-0007-idempotency-unique-constraint.md)).

### Step 4.1 — Fingerprinting

**Test first:** two bodies differing only in key order or whitespace produce the
same fingerprint; a body differing in any field value produces a different one.

**Then:** canonical serialisation of the validated request plus SHA-256.

**Done when:** the reordering case is covered for every field.

### Step 4.2 — Claim and complete

**Test first:** claiming a fresh key succeeds; claiming a held key returns the
existing row rather than an error; completing stores the payment reference,
status, and response bytes; a replay returns bytes identical to what was stored.

**Then:** the `IdempotencyStore` adapter using `ON CONFLICT DO NOTHING
RETURNING`.

**Done when:** all four behaviours pass against a real database.

### Step 4.3 — Concurrency

**Test first:** N goroutines issue the same claim simultaneously through
separate connections; exactly one wins and the rest observe the existing row.

**Then:** nothing new — this test proves Step 4.2 rather than driving new code.
If it fails, the design is wrong, not the test.

**Done when:** it passes repeatedly under `-race` and with `-count=20`.

### Step 4.4 — Lease reclamation

**Test first:** an `IN_FLIGHT` row inside its lease yields conflict; the same
row past its lease is reclaimable; two concurrent reclaims resolve with exactly
one winner.

**Then:** the conditional reclaim update from spec §4.1.

**Done when:** the concurrent reclaim test passes under `-count=20`.

---

## Phase 5 — Create-payment use case

### Step 5.1 — Happy paths with fakes

**Test first:** `card_approved` produces an `APPROVED` payment and one
`payment.approved` event; `card_declined` likewise; both `card_processing_*`
tokens produce a `PROCESSING` payment, one `payment.processing` event, and a
settlement work item due at `now + delay` measured on the test clock.

**Then:** the use case, orchestrating the ports from spec §8 against fakes.

**Done when:** the due time is asserted exactly, not within a tolerance.

### Step 5.2 — Idempotent paths

**Test first:** replay returns the stored response and creates nothing; a
differing body returns `idempotency_key_reuse` and creates nothing; an in-flight
key returns `idempotency_conflict`.

**Then:** the branch table from spec §4.1, fingerprint checked first.

**Done when:** each branch asserts both the response and that the payment count
is unchanged.

### Step 5.3 — Atomicity

**Test first:** with the outbox writer forced to fail, no payment exists
afterwards and the idempotency key is free.

**Then:** wrap the flow in `TxManager`.

**Done when:** the test passes against a real database — a fake cannot prove
this.

---

## Phase 6 — HTTP: create payment

### Step 6.1 — Routing and authentication

**Test first:** every route under `/v1` returns 401 without credentials, and
with wrong credentials; the health endpoint does not.

**Then:** the chi router with the authenticated group
([ADR-0005](decisions/adr-0005-chi-http-router.md)).

**Done when:** the test enumerates routes from the router rather than a
hand-written list, so a new route cannot escape it.

### Step 6.2 — Decoding and validation

**Test first:** malformed JSON is 400; an unknown field — specifically
`card_number` — is 400 and creates nothing; a missing or empty `Idempotency-Key`
is 400; each validation failure maps to its code from spec §7.

**Then:** the handler with a strict decoder.

**Done when:** every row of the spec §7 table has a test.

### Step 6.3 — Response shape

**Test first:** a created payment returns 201 with prefixed identifiers and the
documented fields; a replay returns the identical body.

**Then:** response mapping.

**Done when:** the response is asserted against the README example.

---

## Phase 7 — Subscriptions

### Step 7.1 — Secret encryption

**Test first:** encrypt then decrypt round-trips; ciphertext differs across two
encryptions of the same secret; tampered ciphertext fails to decrypt rather than
returning wrong bytes.

**Then:** AES-256-GCM helpers
([ADR-0009](decisions/adr-0009-webhook-secret-encryption.md)).

**Done when:** the tamper case asserts an error, not a mismatch.

### Step 7.2 — Create subscription

**Test first:** creating returns 201 with a `whsec_` secret; the stored column
never equals the plaintext; a second active subscription returns
`subscription_exists`; an invalid URL or an unknown event type is 422.

**Then:** the endpoint, the repository, and the partial unique index.

**Done when:** the duplicate case is proved against the real index, not an
application check.

---

## Phase 8 — Webhook payload and signing

### Step 8.1 — Payload and signature

**Test first:** the serialised payload matches the README example byte for byte
for a fixed input and clock; the signature matches a fixture computed
independently; changing one byte of the body changes the signature.

**Then:** payload serialisation and HMAC signing in `internal/webhook`.

**Done when:** the fixture is checked in and its derivation documented in the
test.

### Step 8.2 — Sender

**Test first:** against an `httptest.Server`, the received raw body verifies
against the header; a 500 response and a transport error both report failure and
are distinguishable; the timeout is honoured.

**Then:** the HTTP client behind the `Sender` port.

**Done when:** the consumer side of the test verifies the signature the way the
README instructs a real consumer to.

---

## Phase 9 — Worker

### Step 9.1 — Claiming

**Test first:** only work due at or before now is claimed; two concurrent
workers never claim the same row; claiming respects the batch limit.

**Then:** the `FOR UPDATE SKIP LOCKED` claim from spec §5, against a real
database.

**Done when:** the two-worker test passes under `-count=20`.

### Step 9.2 — Settlement

**Test first:** advancing the clock past the delay and running one batch
transitions a `PROCESSING` payment to its terminal status and enqueues the
event; running the same work twice is a no-op; before the delay elapses nothing
happens.

**Then:** the `SETTLE_PAYMENT` handler.

**Done when:** the full `PROCESSING → APPROVED` path is verified with no
`time.Sleep` anywhere in the test.

### Step 9.3 — Delivery

**Test first:** a delivery row exists before any request is sent; a 2xx sets
`SENT`; a non-2xx sets `FAILED` and records the status; a transport error sets
`FAILED` with a null status; `attempt_count` and `last_attempted_at` are
recorded in every case.

**Then:** the `DELIVER_WEBHOOK` handler.

**Done when:** the "persisted before sent" assertion is structural — the
consumer records what it saw, and the test asserts the row predates it.

---

## Phase 10 — Retry

### Step 10.1 — Retry endpoint

**Test first:** retrying a `FAILED` delivery re-enqueues and returns its state;
running the worker then sends bytes identical to the first attempt with the same
signature and an incremented count; retrying a `SENT` delivery is 409; an
unknown or wrongly-prefixed id is 404.

**Then:** the endpoint.

**Done when:** the identical-bytes assertion compares the two received bodies
directly, proving the stored-payload decision in spec §3.

---

## Phase 11 — Assembly

### Step 11.1 — Wiring and startup

**Test first:** startup applies migrations, seeds the account, and serves; it
fails with a clear message when a required variable is missing.

**Then:** `cmd/dummypay`, constructing every adapter, starting the worker
ticker.

**Done when:** `make run` serves against a compose database from a clean clone.

### Step 11.2 — Acceptance walkthrough

**Test first:** one test per README acceptance bullet, end to end through HTTP
against a real database and an `httptest` consumer.

**Then:** nothing new. Failures here mean an earlier phase was wrong.

**Done when:** all ten acceptance criteria pass.

### Step 11.3 — README

**Then:** replace the "Running locally" placeholder with the real steps and
remove the design-phase banner.

**Done when:** a reader following only the README gets a running service and a
successful payment.

---

## Phase 12 — Fitness functions

These are the Compliance sections of the ADRs, made executable
([ADR-0001](decisions/adr-0001-record-architecture-decisions.md)). They are last
only because they need the code to guard.

### Step 12.1 — Dependency direction

**Test first:** `internal/payment` imports nothing under `internal/http`,
`internal/postgres`, or `internal/webhook`, and no driver or web framework.

**Done when:** adding such an import to a scratch file turns the test red.

### Step 12.2 — Time discipline

**Then:** a CI check failing on `time.Now(` or `time.Sleep(` outside
`internal/clock` and `cmd/`.

**Done when:** introducing a stray call fails the build.

### Step 12.3 — Suite budget

**Then:** a wall-clock budget for the suite, enforced in CI, so a test that
starts waiting on real time is caught immediately.

**Done when:** the budget is set from the measured runtime with headroom, and
documented.

---

## Sequencing notes

Phases 1 and 2 have no infrastructure dependency and can be built in any order.
Everything from Phase 3 on is sequential: Phase 4 needs the harness from 3.1,
Phase 5 needs 4, Phase 6 needs 5, and Phases 9 and 10 need 8.

Phase 4 is the one to slow down on. If its concurrency tests are not convincing,
nothing built on top of it is trustworthy, and the correct response is to stop
and fix the design rather than proceed.

`go test ./...` runs at the end of every step, and `go test ./... -race
-count=5` at the end of every phase.
