# Roteiro de Smoke Test — DummyPay V1

Um smoke test é uma verificação rápida, ponta a ponta, de que o serviço está
ativo e seus caminhos críticos funcionam. Execute-o após um deploy ou depois de
um clone novo, para confirmar que nada está quebrado antes de testes mais
profundos.

---

## Pré-requisitos

**Somente Docker é necessário.** Não é preciso Go, `.env` nem configurar o
banco de dados manualmente.

```sh
git clone <repo> && cd dummy-pay
make run                    # builds and starts postgres + dummypay in containers
```

Para parar:

```sh
make docker-stop
```

---

## Inicie o serviço

```sh
make run
# → dummypay escutando em :8080
```

Mantenha este terminal em execução. Execute os comandos abaixo em outro
terminal.

Credenciais usadas em todo o roteiro:

```
auth_user=local-dev-account
auth_pass=change-me-to-a-long-random-value
auth_b64=$(echo -n "$auth_user:$auth_pass" | base64)
```

---

## 1. Saúde

```sh
curl -s http://localhost:8080/health | jq .
```
**Resultado esperado:** `200` — `{"status":"ok"}`

---

## 2. Pagamentos — caminhos de sucesso

### 2.1 `card_approved` → PROCESSING → APPROVED webhook

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-approved-1" \
  -d '{"reference_id":"smoke:approved","amount":10990,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Resultado esperado:** `201` — `status: "PROCESSING"` *(liquida em APPROVED após cerca de 3 s e emite o webhook terminal)*

### 2.2 `card_declined` → PROCESSING → REJECTED webhook

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-declined-1" \
  -d '{"reference_id":"smoke:declined","amount":5000,"currency":"BRL","payment_token":"card_declined"}' | jq .
```
**Resultado esperado:** `201` — `status: "PROCESSING"` *(liquida em REJECTED após cerca de 3 s e emite o webhook terminal)*

### 2.3 `card_processing_approved` → PROCESSING → APPROVED webhook

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-processing-ok" \
  -d '{"reference_id":"smoke:proc-ok","amount":25000,"currency":"BRL","payment_token":"card_processing_approved"}' | jq .
```
**Resultado esperado:** `201` — `status: "PROCESSING"` *(liquida em APPROVED após cerca de 3 s)*

### 2.4 `card_processing_declined` → PROCESSING → REJECTED webhook

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-processing-no" \
  -d '{"reference_id":"smoke:proc-no","amount":10000,"currency":"BRL","payment_token":"card_processing_declined"}' | jq .
```
**Resultado esperado:** `201` — `status: "PROCESSING"` *(liquida em REJECTED após cerca de 3 s)*

---

## 3. Pagamentos — idempotência

### 3.1 Replay idempotente

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-approved-1" \
  -d '{"reference_id":"smoke:approved","amount":10990,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Resultado esperado:** `201` — mesmo `payment_id` e corpo do item 2.1
(idêntico byte a byte).

### 3.2 Reutilização da chave com corpo diferente

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-approved-1" \
  -d '{"reference_id":"smoke:different","amount":9999,"currency":"BRL","payment_token":"card_declined"}' | jq .
```
**Resultado esperado:** `422` — `code: "idempotency_key_reuse"`

---

## 4. Pagamentos — casos de erro

### 4.1 Sem autenticação → 401

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-no-auth" \
  -d '{"reference_id":"smoke:noauth","amount":1000,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Resultado esperado:** `401` — `code: "unauthorized"`

### 4.2 Idempotency-Key ausente → 400

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -d '{"reference_id":"smoke:no-idem","amount":1000,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Resultado esperado:** `400` — `code: "invalid_request"`

### 4.3 Campo desconhecido (`card_number`) → 400

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-card-num" \
  -d '{"reference_id":"smoke:card","amount":1000,"currency":"BRL","payment_token":"card_approved","card_number":"4111111111111111"}' | jq .
```
**Resultado esperado:** `400` — `code: "invalid_request"`

### 4.4 Valor zero → 422

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-zero" \
  -d '{"reference_id":"smoke:zero","amount":0,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Resultado esperado:** `422` — `code: "invalid_amount"`

### 4.5 Valor negativo → 422

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-neg" \
  -d '{"reference_id":"smoke:neg","amount":-100,"currency":"BRL","payment_token":"card_approved"}' | jq .
```
**Resultado esperado:** `422` — `code: "invalid_amount"`

### 4.6 Moeda USD → 422

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-usd" \
  -d '{"reference_id":"smoke:usd","amount":1000,"currency":"USD","payment_token":"card_approved"}' | jq .
```
**Resultado esperado:** `422` — `code: "unsupported_currency"`

### 4.7 Token desconhecido → 422

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-bad-token" \
  -d '{"reference_id":"smoke:bad","amount":1000,"currency":"BRL","payment_token":"invalid_token"}' | jq .
```
**Resultado esperado:** `422` — `code: "unknown_payment_token"`

### 4.8 JSON malformado → 400

```sh
curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: smoke-malformed" \
  -d 'not json' | jq .
```
**Resultado esperado:** `400` — `code: "invalid_request"`

---

## 5. Assinaturas de webhook

### 5.1 Criar assinatura → 201

```sh
curl -s -X POST http://localhost:8080/v1/webhook-subscriptions \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/webhook","events":["payment.approved","payment.rejected","payment.processing"]}' | jq .
```
**Resultado esperado:** `201` — `subscription_id` com prefixo `sub_`, `secret`
com prefixo `whsec_`.
**Guarde o `secret`: ele é exibido apenas uma vez.**

### 5.2 Assinatura duplicada → 409

```sh
curl -s -X POST http://localhost:8080/v1/webhook-subscriptions \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/other","events":["payment.approved"]}' | jq .
```
**Resultado esperado:** `409` — `code: "subscription_exists"`

### 5.3 URL inválida → 422

```sh
curl -s -X POST http://localhost:8080/v1/webhook-subscriptions \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -d '{"url":"not-a-url","events":["payment.approved"]}' | jq .
```
**Resultado esperado:** `422`

### 5.4 Tipo de evento desconhecido → 422

```sh
curl -s -X POST http://localhost:8080/v1/webhook-subscriptions \
  -H "Authorization: Basic $auth_b64" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/webhook","events":["payment.refunded"]}' | jq .
```
**Resultado esperado:** `422`

---

## 6. Retry de webhook

### 6.1 Entrega desconhecida → 404

```sh
curl -s -X POST http://localhost:8080/v1/webhook-deliveries/dlv_00000000-0000-0000-0000-000000000000/retry \
  -H "Authorization: Basic $auth_b64" | jq .
```
**Resultado esperado:** `404` — `code: "not_found"`

### 6.2 Prefixo incorreto → 404

```sh
curl -s -X POST http://localhost:8080/v1/webhook-deliveries/pay_00000000-0000-0000-0000-000000000000/retry \
  -H "Authorization: Basic $auth_b64" | jq .
```
**Resultado esperado:** `404` — `code: "not_found"` *(prefixo incorreto:
nunca é feita uma busca; ADR-0006)*

---

## Comando único (tudo junto)

```sh
auth_b64=$(echo -n 'local-dev-account:change-me-to-a-long-random-value' | base64)
base=http://localhost:8080

# 1. Saúde
echo "=== Saúde ===" && curl -s $base/health | jq -c '{health: .status}'

# 2. Caminhos de sucesso
echo "=== aprovado ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i1" -d '{"reference_id":"a","amount":10990,"currency":"BRL","payment_token":"card_approved"}' | jq -c '{status: .status}'
echo "=== recusado ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i2" -d '{"reference_id":"d","amount":5000,"currency":"BRL","payment_token":"card_declined"}' | jq -c '{status: .status}'
echo "=== processamento aprovado ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i3" -d '{"reference_id":"p","amount":25000,"currency":"BRL","payment_token":"card_processing_approved"}' | jq -c '{status: .status}'
echo "=== processamento recusado ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i4" -d '{"reference_id":"p","amount":10000,"currency":"BRL","payment_token":"card_processing_declined"}' | jq -c '{status: .status}'

# 3. Idempotência
echo "=== replay ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i1" -d '{"reference_id":"a","amount":10990,"currency":"BRL","payment_token":"card_approved"}' | jq -c '{status: .status}'
echo "=== reutilização ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: i1" -d '{"reference_id":"x","amount":9999,"currency":"BRL","payment_token":"card_declined"}' | jq -c '{code: .code}'

# 4. Erros
echo "=== sem autenticação ===" && curl -s -X POST $base/v1/payments -H "Content-Type: application/json" -H "Idempotency-Key: e1" -d '{"reference_id":"n","amount":1000,"currency":"BRL","payment_token":"card_approved"}' | jq -c '{code: .code}'
echo "=== sem chave de idempotência ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -d '{"reference_id":"n","amount":1000,"currency":"BRL","payment_token":"card_approved"}' | jq -c '{code: .code}'
echo "=== card_number ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: e3" -d '{"reference_id":"n","amount":1000,"currency":"BRL","payment_token":"card_approved","card_number":"4111..."}' | jq -c '{code: .code}'
echo "=== valor zero ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: e4" -d '{"reference_id":"n","amount":0,"currency":"BRL","payment_token":"card_approved"}' | jq -c '{code: .code}'
echo "=== USD ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: e5" -d '{"reference_id":"n","amount":1000,"currency":"USD","payment_token":"card_approved"}' | jq -c '{code: .code}'
echo "=== token desconhecido ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: e6" -d '{"reference_id":"n","amount":1000,"currency":"BRL","payment_token":"bad"}' | jq -c '{code: .code}'
echo "=== malformado ===" && curl -s -X POST $base/v1/payments -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -H "Idempotency-Key: e7" -d 'not json' | jq -c '{code: .code}'

# 5. Assinaturas
echo "=== create sub ===" && curl -s -X POST $base/v1/webhook-subscriptions -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -d '{"url":"https://example.com/webhook","events":["payment.approved"]}' | jq -c '{sub: .subscription_id, secret_prefix: (.secret[:10]+"...")}'
echo "=== dup sub ===" && curl -s -X POST $base/v1/webhook-subscriptions -H "Authorization: Basic $auth_b64" -H "Content-Type: application/json" -d '{"url":"https://example.com/other","events":["payment.approved"]}' | jq -c '{code: .code}'

# 6. Retry
echo "=== retry 404 ===" && curl -s -X POST $base/v1/webhook-deliveries/dlv_00000000-0000-0000-0000-000000000000/retry -H "Authorization: Basic $auth_b64" | jq -c '{code: .code}'
echo "=== prefixo incorreto ===" && curl -s -X POST $base/v1/webhook-deliveries/pay_00000000-0000-0000-0000-000000000000/retry -H "Authorization: Basic $auth_b64" | jq -c '{code: .code}'

echo ""
echo "=== DONE ==="
```
