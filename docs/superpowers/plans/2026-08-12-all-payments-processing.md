# All Payments Start Processing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make every accepted payment return `PROCESSING` and publish only its terminal outcome after outbox settlement.

**Architecture:** Keep the four opaque tokens and the existing transactional outbox. The payment constructor will always set `PROCESSING`; settlement will map each token to its deterministic terminal outcome, so every creation schedules `SETTLE_PAYMENT` and the existing worker emits the terminal webhook event.

**Tech Stack:** Go, standard `testing`, testify/require, go-cmp, PostgreSQL integration tests, Insomnia YAML collection.

## Global Constraints

- `internal/payment` imports no adapter; it declares its ports.
- No card data, external system, `time.Now`, or `time.Sleep` is introduced.
- Tests are table-driven and use `require` for control flow; timestamp-bearing values use `cmp.Diff` or `time.Equal`.
- SQL remains in `internal/postgres`; no dependencies are added.
- Do not commit or push without an explicit request.

---

### Task 1: Make all scenario tokens asynchronous in the domain

**Files:**
- Modify: `internal/payment/payment_test.go`
- Modify: `internal/payment/create_payment_test.go`
- Modify: `internal/payment/payment.go`

**Consumes:** `ScenarioToken`, `Payment.NewPayment`, `Payment.Settle`, and `CreatePaymentUseCase.Execute`.

**Produces:** Every new `Payment` has `StatusProcessing`; `Settle` maps approved tokens to `StatusApproved` and declined tokens to `StatusRejected`; every create flow queues settlement and emits a processing event.

- [x] **Step 1: Write failing domain and use-case assertions**

```go
{TokenCardApproved, StatusProcessing, EventPaymentProcessing, true},
{TokenCardDeclined, StatusProcessing, EventPaymentProcessing, true},
```

Add settlement assertions for `TokenCardApproved -> StatusApproved` and `TokenCardDeclined -> StatusRejected`.

- [x] **Step 2: Run the focused package tests and observe failure**

Run: `go test ./internal/payment`

Expected: FAIL because approved/declined payments are constructed terminal and no settlement work is queued.

- [x] **Step 3: Implement the minimal domain mapping**

```go
func creationStatus(ScenarioToken) Status { return StatusProcessing }

// Settle maps both approved tokens to APPROVED and both declined tokens to REJECTED.
```

- [x] **Step 4: Run the focused package tests and observe success**

Run: `go test ./internal/payment`

Expected: PASS.

### Task 2: Prove the HTTP-to-webhook lifecycle end to end

**Files:**
- Modify: `internal/postgres/acceptance_test.go`

**Consumes:** the production router, PostgreSQL repositories, injected fake clock, and worker.

**Produces:** Acceptance coverage that `card_approved` and `card_declined` return `PROCESSING`, persist that state, and become `APPROVED` or `REJECTED` only after worker processing.

- [x] **Step 1: Update the acceptance tests to expect the lifecycle**

```go
resp := assertPaymentResponse(t, rec, http.StatusCreated, "PROCESSING")
deps.Clock.Advance(processingDelay)
require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))
require.Equal(t, "APPROVED", queryPaymentStatus(t, deps.Pool, resp.PaymentID))
```

Use the analogous `REJECTED` assertion for `card_declined`, and update replay/webhook fixture expectations to initial `PROCESSING` where they inspect the creation response.

- [x] **Step 2: Run the integration suite**

Run: `make test-integration`

Expected: PASS with a running PostgreSQL database; otherwise report the exact unavailable dependency.

### Task 3: Align published contract and executable Insomnia assertions

**Files:**
- Modify: `README.md`
- Modify: `docs/spec-v1.md`
- Create: `docs/decisions/adr-0015-all-payments-start-processing.md`
- Modify: `docs/decisions/README.md`
- Modify: `docs/insomnia-collection.yaml`

**Consumes:** the new domain lifecycle.

**Produces:** Documentation and the Insomnia collection state that all four tokens create `PROCESSING`; token names select only the final webhook/settlement outcome.

- [x] **Step 1: Update contract prose and state machine**

Replace immediate `APPROVED`/`REJECTED` creation paths with four `PROCESSING -> terminal` paths. State that a processing event can be subscribed to and terminal events are emitted by settlement.

- [x] **Step 2: Record the architecture decision**

Create ADR-0015 explaining that uniform asynchronous creation gives integrations one response shape and makes terminal status observable through webhook delivery; retain the existing outbox decision because only its scenario timing detail changes.

- [x] **Step 3: Update collection requests and assertions**

Rename the first two payment requests to show `PROCESSING` and assert `body.status === 'PROCESSING'`; update the idempotency replay assertion to the same status.

- [x] **Step 4: Verify YAML parses and run final checks**

Run: `ruby -e 'require "yaml"; YAML.load_file("docs/insomnia-collection.yaml")'`, `make lint`, and `go test ./... -race -count=5`.

Expected: all commands exit 0.
