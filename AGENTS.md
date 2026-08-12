# AGENTS.md — DummyPay

A self-contained, deterministic card PSP for testing payment integrations. Go,
its own PostgreSQL, three endpoints under `/v1`, no dependency on any external
system.

Read these before doing anything. They are the contract, not background.

@README.md
@docs/spec-v1.md
@docs/plan-v1.md
@docs/decisions/README.md

Individual ADRs live in `docs/decisions/adr-NNNN-*.md`. Open the one a task
touches; do not reason about a decision from its one-line summary in the index.

---

## Non-negotiable rules

These are not style preferences. Violating one is a defect, and each traces to
an ADR that explains the cost of the alternative.

1. **Never accept, store, or log card data.** No PAN, CVV, expiry, or holder
   name — there is no field for them and none will be added. Outcomes come from
   the four scenario tokens. Request bodies reject unknown fields specifically so
   a `card_number` cannot be smuggled in. (ADR-0002)
2. **No external systems.** No third-party API, no message broker, no KMS, no
   remote service. The test suite must pass with no external network. (ADR-0013)
3. **`internal/payment` imports no adapter.** Not `internal/http`, not
   `internal/postgres`, not `internal/webhook`, no driver, no web framework. The
   domain declares its ports; adapters implement them. (ADR-0003)
4. **No `time.Now()` or `time.Sleep()` outside `internal/clock` and `cmd/`.**
   Time is injected; schedule is `due_at` data, not timers. No test sleeps.
   (ADR-0012)
5. **Configuration comes only from the environment.** No config file is read.
   `.env.example` holds placeholders and never real values. (ADR-0010)
6. **SQL lives only in `internal/postgres`.** (ADR-0004)
7. **Webhook payloads are stored and sent as raw bytes.** Never re-serialise a
   payload — the HMAC is over exact bytes, and a retry must resend them
   unchanged.
8. **To reverse a decision, write a new ADR** marking the old one superseded.
   Never edit a superseded record into agreement with the present. (ADR-0001)

---

## Workflow

**Test first, always.** Write the test, run it, watch it fail for the right
reason, then implement. A test that passed the first time proves nothing until
you have seen it fail.

**Follow the plan's step order.** `docs/plan-v1.md` sequences the work so each
step is independently verifiable. Phase 4 (idempotency) is the highest-risk
slice; if its concurrency tests are not convincing, stop and fix the design
rather than building on it.

**Tick the roadmap in the commit that earns it.** A box in
`docs/plan-v1.md#roadmap` is ticked when that step's **Done when** condition has
been *observed*, not when the code was written. Never tick ahead.

**Report honestly.** If tests fail, say so and show the output. If a step was
skipped, say which and why. Never claim a step is done without running its
verification.

---

## Commands

```sh
make db-up               # start PostgreSQL via docker compose
make test                # unit tests; no database needed
make test-integration    # integration tests; requires the database
make lint                # vet plus the fitness-function checks
make run                 # run the service against the compose database
go test ./...            # everything; integration tests skip without a database
go test ./... -race -count=5   # run at the end of every phase
```

Integration tests skip when no database is reachable so `go test ./...` works
anywhere. In CI a skip is a **failure** — never rely on a local green run alone
as evidence for a database-backed guarantee.

---

## Conventions

**Layout** — see spec §1. `cmd/dummypay` is the only place adapters are
constructed.

**Errors** — domain errors are sentinel values in `internal/payment`, mapped to
HTTP codes only in `internal/http` per spec §7. Wrap with `%w`. A `500` body
never contains the underlying error; it is logged with the request ID.

**Identifiers** — `uuid.UUID` in the domain and the database, never `string`.
The `pay_`/`txn_`/`sub_`/`evt_`/`dlv_` prefix is applied and stripped at the HTTP
boundary only. A well-formed UUID with the wrong prefix is a `404`, not a
lookup. (ADR-0006)

**Money** — `int64` cents. Never a float, anywhere, for any reason.

**Tests** — table-driven, on the standard library `testing`. `testify/require`
for control flow (`NoError`, `ErrorIs`, `True`, `Len`); `cmp.Diff` reported as
`(-want +got)` for comparing structs. Never `assert` — only `require`. Never
`testify/mock` or `testify/suite`: fakes are written by hand, and `t.Cleanup`
covers teardown. Integration tests get their own schema and share no state.

> **Never use `require.Equal` on a struct containing a timestamp.** It falls
> through to `reflect.DeepEqual`, which compares the monotonic reading and the
> location pointer, so a value that has been through PostgreSQL fails against
> the original even when the instants are equal. Use `cmp.Diff`, which honours
> `time.Time`'s own `Equal`. (ADR-0014)

Domain value objects carry an `Equal` method — go-cmp panics on unexported
fields otherwise, and the method belongs in production code anyway.

**Dependencies** — the current set is `chi`, `pgx`, `goose`, `google/uuid`,
`go-cmp`, and `testify`. Adding another is an architecture decision and needs an
ADR.

**Commits** — imperative subject under 72 characters, prefixed by area
(`feat:`, `fix:`, `docs:`, `test:`, `refactor:`). The body explains why, not
what. Do not commit or push unless asked.

---

## Out of scope

Do not build these, and do not "prepare" for them. Each absence is a decision
recorded in the README and the ADRs:

Tokenization, card storage, real PCI compliance · auth-only, pre-authorization,
later or partial capture · refunds, chargebacks, 3DS, installments, anti-fraud,
split payments, recurring billing, Pix, boleto · multi-account, dashboard,
public API · automatic webhook retry with backoff · webhook secret rotation ·
subscription update or deletion · listing endpoints · integration with any other
service.

If a task seems to require one of these, stop and ask. It is more likely that
the task is wrong than that the scope is.

---

## Current state

All 36 plan steps complete, all 13 acceptance criteria verified. The full V1 contract is implemented: `GET /health`, `POST /v1/payments` with idempotency, `POST /v1/webhook-subscriptions` with AES-256-GCM secret encryption, `POST /v1/webhook-deliveries/{delivery_id}/retry`, outbox worker with `FOR UPDATE SKIP LOCKED`, HMAC-signed webhook delivery with exact-byte retry, and four fitness-function tests. The suite passes clean under `-race -count=5` with no external network.
