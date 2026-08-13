# Logging and Portuguese Student Docs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Log payment settlement and webhook delivery lifecycle events while translating the student-facing documentation to Brazilian Portuguese.

**Architecture:** The worker receives a narrow `Logger` port declared by `internal/payment`; `cmd/dummypay` supplies the standard logger adapter. The existing HTTP logging middleware continues logging every inbound request. Documentation translation is isolated to README, smoke test, pitch, and Insomnia copy; protocol literals remain unchanged.

**Tech Stack:** Go standard library `log`, standard `testing`, testify/require, Make, Insomnia YAML.

## Global Constraints

- `internal/payment` imports no adapter, including `log`.
- No card data, webhook payload, signature, secret, Authorization header, or callback URL is logged.
- No `time.Now()` or `time.Sleep` outside `internal/clock` and `cmd/`.
- The user explicitly requested no tests that validate log output; existing
  functional worker tests are retained without log assertions.
- Preserve the existing uncommitted Docker publishing changes.

---

### Task 1: Expose worker lifecycle events through a domain logger port

**Files:**
- Modify: `internal/payment/ports.go`
- Modify: `internal/payment/worker.go`
- Modify: `cmd/dummypay/main.go`

**Consumes:** `WorkerDeps`, the worker's settlement and delivery flows, `log.Printf` in the composition root.

**Produces:** `payment.Logger` with `Printf(string, ...any)`, injected into worker construction; a settlement log and a delivery-attempt log after their durable state changes.

- [x] **Step 1: Add the port, adapter, and logs**

```go
type Logger interface { Printf(format string, args ...any) }

// after Update succeeds
w.deps.Logger.Printf("payment settled payment_id=%s token=%s status=%s", ...)

// after RecordAttempt succeeds
w.deps.Logger.Printf("webhook delivery delivery_id=%s payment_id=%s event_type=%s status=%s http_status=%d transport_error=%t", ...)
```

Wire a `payment.Logger` adapter around `log.Printf` from `cmd/dummypay`.

- [x] **Step 2: Run the existing worker suite**

Run: `go test ./internal/payment`

Expected: PASS.

### Task 2: Translate student materials without changing their protocols

**Files:**
- Modify: `README.md`
- Modify: `docs/smoke-test.md`
- Modify: `docs/pitch.md`
- Modify: `docs/insomnia-collection.yaml`

**Consumes:** the existing public API contract.

**Produces:** Brazilian Portuguese instructional prose and translated Insomnia labels/assertion descriptions, preserving routes, JSON, status literals, and assertions' machine values.

- [x] **Step 1: Translate prose and collection copy**

Keep, for example, `payment_token`, `PROCESSING`, `/v1/payments`, and
`insomnia.expect(body.status).to.eql('PROCESSING')` unchanged while translating
the surrounding explanation and test labels.

- [x] **Step 2: Verify collection YAML and stable protocol literals**

Run: `ruby -e 'require "yaml"; YAML.load_file("docs/insomnia-collection.yaml")'`

Expected: exit 0.

### Task 3: Verify complete change

**Files:**
- Verify: changed Go files and docs

- [x] **Step 1: Run lint and full suite**

Run: `make lint` and `go test ./...`

Expected: both exit 0.

- [x] **Step 2: Review diff hygiene**

Run: `git diff --check`

Expected: exit 0.
