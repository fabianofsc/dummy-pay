# Smoke Test Script — DummyPay V1

A smoke test is a quick, end-to-end check that the service is alive and its
critical paths work. Run this after deploying or after a fresh clone to confirm
nothing is broken before deeper testing.

---

## Prerequisites

**Only Docker is required.** No Go, no .env, no manual database setup.

```sh
git clone <repo> && cd dummy-pay
make run                    # builds and starts postgres + dummypay in containers
```

To stop:

```sh
make docker-stop
```

---

## Start the service

```sh
make run
# → dummypay listening on :8080
```

Keep this terminal running. Run the commands below in another terminal.

Credentials used throughout:

```
auth_user=local-dev-account
auth_pass=change-me-to-a-long-random-value
auth_b64=$(echo -n "$auth_user:$auth_pass" | base64)
```

---

## 1. Health

```sh
curl -s http://localhost:8080/health | jq .
```
**Expect:** `200` — `{"status":"ok"}`

---

## 2. Payments — happy paths

### 2.1 `card_approved` → PROCESSING → APPROVED webhook

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-approved-1" \
  -d '{"reference_id":"smoke:approved","amount":10990,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Expect:** `201` — `status: "PROCESSING"` *(settles to APPROVED after ~3s and emits the terminal webhook)*

### 2.2 `card_declined` → PROCESSING → REJECTED webhook

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-declined-1" \
  -d '{"reference_id":"smoke:declined","amount":5000,"currency":"BRL","payment_token":"card_declined"}' | jq .
```
**Expect:** `201` — `status: "PROCESSING"` *(settles to REJECTED after ~3s and emits the terminal webhook)*

### 2.3 `card_processing_approved` → PROCESSING → APPROVED webhook

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-processing-ok" \
  -d '{"reference_id":"smoke:proc-ok","amount":25000,"currency":"BRL","payment_token":"card_processing_approved"}' | jq .
```
**Expect:** `201` — `status: "PROCESSING"` *(settles to APPROVED after ~3s)*

### 2.4 `card_processing_declined` → PROCESSING → REJECTED webhook

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-processing-no" \
  -d '{"reference_id":"smoke:proc-no","amount":10000,"currency":"BRL","payment_token":"card_processing_declined"}' | jq .
```
**Expect:** `201` — `status: "PROCESSING"` *(settles to REJECTED after ~3s)*

---

## 3. Payments — idempotency

### 3.1 Idempotent replay

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-approved-1" \
  -d '{"reference_id":"smoke:approved","amount":10990,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Expect:** `201` — same `payment_id` and body as 2.1 (byte-identical).

### 3.2 Key reuse with different body

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-approved-1" \
  -d '{"reference_id":"smoke:different","amount":9999,"currency":"BRL","payment_token":"card_declined"}' | jq .
```
**Expect:** `422` — `code: "idempotency_key_reuse"`

---

## 4. Payments — error cases

### 4.1 No auth → 401

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-no-auth" \
  -d '{"reference_id":"smoke:noauth","amount":1000,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Expect:** `401` — `code: "unauthorized"`

### 4.2 Missing Idempotency-Key → 400

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -d '{"reference_id":"smoke:no-idem","amount":1000,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Expect:** `400` — `code: "invalid_request"`

### 4.3 Unknown field (`card_number`) → 400

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-card-num" \
  -d '{"reference_id":"smoke:card","amount":1000,"currency":"BRL","payment_token":"card_approved","card_number":"4111111111111111"}' | jq .
```
**Expect:** `400` — `code: "invalid_request"`

### 4.4 Amount zero → 422

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-zero" \
  -d '{"reference_id":"smoke:zero","amount":0,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Expect:** `422` — `code: "invalid_amount"`

### 4.5 Amount negative → 422

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-neg" \
  -d '{"reference_id":"smoke:neg","amount":-100,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Expect:** `422` — `code: "invalid_amount"`

### 4.6 Currency USD → 422

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-usd" \
  -d '{"reference_id":"smoke:usd","amount":1000,"currency":"USD","payment_token":"card_approved"}' | jq .
```
**Expect:** `422` — `code: "unsupported_currency"`

### 4.7 Unknown token → 422

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-bad-token" \
  -d '{"reference_id":"smoke:bad","amount":1000,"currency":"BRL","payment_token":"invalid_token"}' | jq .
```
**Expect:** `422` — `code: "unknown_payment_token"`

### 4.8 Malformed JSON → 400

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-malformed" \
  -d 'not json' | jq .
```
**Expect:** `400` — `code: "invalid_request"`

---

## 5. Webhook Subscriptions

### 5.1 Create subscription → 201

```sh
curl -s -X POST http://localhost:8080/v1/webhook-subscriptions \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/webhook","events":["payment.approved","payment.rejected","payment.processing"]}' | jq .
```
**Expect:** `201` — `subscription_id` with `sub_` prefix, `secret` with `whsec_` prefix.
**Save the `secret` — it is shown only once.**

### 5.2 Duplicate subscription → 409

```sh
curl -s -X POST http://localhost:8080/v1/webhook-subscriptions \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/other","events":["payment.approved"]}' | jq .
```
**Expect:** `409` — `code: "subscription_exists"`

### 5.3 Invalid URL → 422

```sh
curl -s -X POST http://localhost:8080/v1/webhook-subscriptions \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -d '{"url":"not-a-url","events":["payment.approved"]}' | jq .
```
**Expect:** `422`

### 5.4 Unknown event type → 422

```sh
curl -s -X POST http://localhost:8080/v1/webhook-subscriptions \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/webhook","events":["payment.refunded"]}' | jq .
```
**Expect:** `422`

---

## 6. Webhook Retry

### 6.1 Unknown delivery → 404

```sh
curl -s -X POST http://localhost:8080/v1/webhook-deliveries/dlv_00000000-0000-0000-0000-000000000000/retry \
  -H "Authorization: Basic $auth_b64" | jq .
```
**Expect:** `404` — `code: "not_found"`

### 6.2 Wrong prefix → 404

```sh
curl -s -X POST http://localhost:8080/v1/webhook-deliveries/pay_00000000-0000-0000-0000-000000000000/retry \
  -H "Authorization: Basic $auth_b64" | jq .
```
**Expect:** `404` — `code: "not_found"` *(wrong prefix — never a lookup, ADR-0006)*

---

## One-liner (all together)

```sh
auth_b64=$(echo -n 'local-dev-account:change-me-to-a-long-random-value' | base64)
base=http://localhost:8080

# 1. Health
echo "=== Health ===" && curl -s $base/health | jq -c '{health: .status}'

# 2. Happy paths
echo "=== approved ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i1" -d '{"reference_id":"a","amount":10990,"currency":"BRL","payment_token":"card_approved"}' | jq -c '{status: .status}'
echo "=== declined ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i2" -d '{"reference_id":"d","amount":5000,"currency":"BRL","payment_token":"card_declined"}' | jq -c '{status: .status}'
echo "=== processing OK ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i3" -d '{"reference_id":"p","amount":25000,"currency":"BRL","payment_token":"card_processing_approved"}' | jq -c '{status: .status}'
echo "=== processing NO ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i4" -d '{"reference_id":"p","amount":10000,"currency":"BRL","payment_token":"card_processing_declined"}' | jq -c '{status: .status}'

# 3. Idempotency
echo "=== replay ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i1" -d '{"reference_id":"a","amount":10990,"currency":"BRL","payment_token":"card_approved"}' | jq -c '{status: .status}'
echo "=== reuse ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i1" -d '{"reference_id":"x","amount":9999,"currency":"BRL","payment_token":"card_declined"}' | jq -c '{code: .code}'

# 4. Errors
echo "=== no auth ===" && curl -s -X POST $base/v1/payments -H "Content-Type: application/json" -H "Idempotency-Key: e1" -d '{"reference_id":"n","amount":1000,"currency":"BRL","payment_token":"card_approved"}' | jq -c '{code: .code}'
echo "=== no idem key ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -d '{"reference_id":"n","amount":1000,"currency":"BRL","payment_token":"card_approved"}' | jq -c '{code: .code}'
echo "=== card_number ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: e3" -d '{"reference_id":"n","amount":1000,"currency":"BRL","payment_token":"card_approved","card_number":"4111..."}' | jq -c '{code: .code}'
echo "=== zero amount ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: e4" -d '{"reference_id":"n","amount":0,"currency":"BRL","payment_token":"card_approved"}' | jq -c '{code: .code}'
echo "=== USD ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: e5" -d '{"reference_id":"n","amount":1000,"currency":"USD","payment_token":"card_approved"}' | jq -c '{code: .code}'
echo "=== unknown token ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: e6" -d '{"reference_id":"n","amount":1000,"currency":"BRL","payment_token":"bad"}' | jq -c '{code: .code}'
echo "=== malformed ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: e7" -d 'not json' | jq -c '{code: .code}'

# 5. Subscriptions
echo "=== create sub ===" && curl -s -X POST $base/v1/webhook-subscriptions -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -d '{"url":"https://example.com/webhook","events":["payment.approved"]}' | jq -c '{sub: .subscription_id, secret_prefix: (.secret[:10]+"...")}'
echo "=== dup sub ===" && curl -s -X POST $base/v1/webhook-subscriptions -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -d '{"url":"https://example.com/other","events":["payment.approved"]}' | jq -c '{code: .code}'

# 6. Retry
echo "=== retry 404 ===" && curl -s -X POST $base/v1/webhook-deliveries/dlv_00000000-0000-0000-0000-000000000000/retry -H "Authorization: Basic $auth_b64" | jq -c '{code: .code}'
echo "=== wrong prefix ===" && curl -s -X POST $base/v1/webhook-deliveries/pay_00000000-0000-0000-0000-000000000000/retry -H "Authorization: Basic $auth_b64" | jq -c '{code: .code}'

echo ""
echo "=== DONE ==="
```
