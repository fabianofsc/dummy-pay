# DummyPay

Um PSP de cartões autocontido e determinístico para testar integrações de
pagamento. Ele expõe uma API HTTP com formato realista, mantém seu próprio
PostgreSQL e nunca aceita dados de cartão.

- **Por que existe** — [docs/pitch.md](docs/pitch.md)
- **Por que é construído assim** — [docs/decisions/](docs/decisions/)
- **Como é construído** — [docs/spec-v1.md](docs/spec-v1.md)
- **Em que ordem foi construído e o que já está pronto** — [docs/plan-v1.md](docs/plan-v1.md#roadmap)

## Sumário

- [O que faz](#o-que-faz)
- [Conceitos centrais](#conceitos-centrais)
- [Stack](#stack)
- [API](#api)
  - [Autenticação](#autenticação)
  - [POST /v1/payments](#post-v1payments)
  - [POST /v1/webhook-subscriptions](#post-v1webhook-subscriptions)
  - [POST /v1/webhook-deliveries/{delivery_id}/retry](#post-v1webhook-deliveriesdelivery_idretry)
- [Webhooks](#webhooks)
- [Configuração](#configuração)
- [Execução local](#execução-local)
- [Testes](#testes)
- [Escopo](#escopo)
- [Segurança](#segurança)

---

## O que faz

O DummyPay expõe uma única operação de pagamento — **venda**: autoriza e
captura em uma só chamada. Não há uma etapa separada de captura.

O resultado de um pagamento é escolhido pelo chamador por meio de um **token
de cenário** opaco; assim, cada teste é reproduzível:

| Token                       | Comportamento                 |
| --------------------------- | ----------------------------- |
| `card_approved`             | `PROCESSING`, depois `APPROVED` |
| `card_declined`             | `PROCESSING`, depois `REJECTED` |
| `card_processing_approved`  | `PROCESSING`, depois `APPROVED` |
| `card_processing_declined`  | `PROCESSING`, depois `REJECTED` |

Todo pagamento retorna `PROCESSING` primeiro. O estado terminal é entregue por
webhook após a liquidação; o atraso é configurado por ambiente e é zero (ou
controlado por um relógio) nos testes.

## Conceitos centrais

**Conta técnica.** Exatamente uma por ambiente, provisionada por configuração.
Ela é dona de todos os pagamentos e da assinatura de webhook. Não há
multitenancy na V1.

**Pagamento.** Uma única venda. É identificado por um `payment_id` estável,
gerado pelo DummyPay, e um `provider_transaction_id`, que representa a
transação no adquirente imaginário. Ambos permanecem estáveis durante toda a
vida do pagamento.

**Identificadores.** Todo identificador público é um UUIDv7 com prefixo de tipo:
`pay_`, `txn_`, `sub_`, `evt_`, `dlv_`. O prefixo torna os identificadores
autoexplicativos nos logs, mas não faz parte do valor armazenado. Trate-os como
strings opacas.

**Status do pagamento.** Um entre `APPROVED`, `REJECTED`, `PROCESSING`.
`PROCESSING` é transitório e sempre termina em `APPROVED` ou `REJECTED`.

**Reference ID.** Uma string opaca fornecida pelo chamador (por exemplo,
`checkout:123`). O DummyPay a armazena e devolve, sem atribuir significado.

**Chave de idempotência.** Um cabeçalho fornecido pelo chamador que torna a
criação de pagamentos segura para repetição. Consulte [POST /v1/payments](#post-v1payments).

**Assinatura de webhook.** Registrada uma vez, antecipadamente. URLs de
callback não são associadas a pagamentos individuais. A V1 permite uma
assinatura ativa por conta técnica.

**Entrega de webhook.** Um registro com contagem de tentativas para cada evento
enviado à assinatura. Ele é persistido antes do envio.

## Stack

| Responsabilidade | Escolha | Por quê | ADR |
| --- | --- | --- | --- |
| Linguagem | Go | — | — |
| Arquitetura | Hexagonal enxuta | `internal/payment` contém o domínio e os casos de uso e não importa infraestrutura. `internal/http`, `internal/postgres` e `internal/webhook` são adaptadores por trás de portas declaradas pelo domínio; assim, as regras são testáveis em memória. | [0003](docs/decisions/adr-0003-lean-hexagonal-architecture.md) |
| Banco de dados | PostgreSQL via `pgx/v5`, SQL escrito à mão | Idempotência e o claim do worker são garantias do banco — `INSERT … ON CONFLICT` e `FOR UPDATE SKIP LOCKED`. Um ORM esconderia justamente o que precisa ser revisável. | [0004](docs/decisions/adr-0004-postgresql-pgx-hand-written-sql.md) |
| Roteador | `chi` v5 | Os handlers continuam sendo `http.HandlerFunc`, portanto `httptest` não precisa de adaptador e nenhum contexto de framework chega ao domínio. Grupos de rotas aplicam autenticação a `/v1` como unidade. | [0005](docs/decisions/adr-0005-chi-http-router.md) |
| Identificadores | UUIDv7 em colunas nativas `uuid`, com prefixo na borda | Ordenados por tempo, para que inserções mantenham a localidade do índice e `ORDER BY id` seja cronológico, sem a fragmentação do UUIDv4. | [0006](docs/decisions/adr-0006-uuidv7-identifiers.md) |
| Idempotência | Restrição única mais estado explícito `IN_FLIGHT`/`COMPLETED` | A corrida é resolvida pelo banco, então a garantia vale entre processos. `409` versus replay é estado registrado, não inferência. | [0007](docs/decisions/adr-0007-idempotency-unique-constraint.md) |
| Trabalho assíncrono | Tabela outbox mais worker no processo | Liquidações e entregas são linhas com horário de vencimento, escritas na mesma transação que provocou a mudança. Sobrevive a reinicializações; `retry` reenfileira. | [0008](docs/decisions/adr-0008-outbox-in-process-worker.md) |
| Segredo em repouso | AES-256-GCM com chave separada do DSN do banco | A assinatura precisa ser reversível, portanto hash não serve. Um DSN vazado sozinho não consegue forjar eventos. | [0009](docs/decisions/adr-0009-webhook-secret-encryption.md) |
| Configuração | Somente variáveis de ambiente | Nenhum arquivo de configuração é lido; assim, segredos não são commitados por acidente. | [0010](docs/decisions/adr-0010-configuration-from-environment.md) |
| Migrações | `goose`, embutido via `embed.FS`, aplicado na inicialização | Esquema e binário são distribuídos juntos e não podem divergir. Clonar e executar não exige ferramenta extra. | [0011](docs/decisions/adr-0011-embedded-goose-migrations.md) |
| Determinismo | `Clock` injetado; agenda expressa como dado `due_at`, não timers | Atraso real no desenvolvimento, zero nos testes, sem `time.Sleep` na suíte. | [0012](docs/decisions/adr-0012-injected-clock-scheduler.md) |
| Banco de teste | PostgreSQL do `docker compose`, um schema por teste | A semântica testada é a semântica entregue. Faz pull uma vez e funciona offline depois. | [0013](docs/decisions/adr-0013-integration-tests-real-postgres.md) |

O raciocínio completo de cada escolha, incluindo opções rejeitadas e seus
custos, está em **[docs/decisions/](docs/decisions/)**. Leia esses registros
antes de propor mudanças na stack: várias foram decisões equilibradas e os
argumentos foram registrados para não precisarem ser reabertos.

## API

Todos os endpoints são JSON sobre HTTP, sob `/v1`. Requisições e respostas usam
`application/json`.

### Autenticação

Todo endpoint exige autenticação HTTP Basic com as credenciais da conta técnica,
vindas do ambiente:

```
Authorization: Basic <base64(key_id:key_secret)>
```

Credenciais ausentes ou inválidas retornam `401`.

### POST /v1/payments

Cria e liquida uma venda.

**Cabeçalhos**

| Cabeçalho        | Obrigatório | Observações                    |
| ---------------- | ----------- | ------------------------------ |
| `Authorization`   | sim         | Basic, conta técnica           |
| `Idempotency-Key` | sim         | String não vazia               |
| `Content-Type`    | sim         | `application/json`            |

**Requisição**

```json
{
  "reference_id": "checkout:123",
  "amount": 10990,
  "currency": "BRL",
  "payment_token": "card_processing_approved"
}
```

| Campo           | Tipo    | Regras                                         |
| --------------- | ------- | ---------------------------------------------- |
| `reference_id`  | string  | Opaco, definido pelo chamador                  |
| `amount`        | integer | Positivo, em centavos (`10990` = R$ 109,90)    |
| `currency`      | string  | Somente `BRL`                                  |
| `payment_token` | string  | Um dos quatro tokens de cenário acima          |

**Resposta — `201 Created`**

```json
{
  "payment_id": "pay_0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b",
  "provider_transaction_id": "txn_0199a1f4-3c83-7a04-8f21-6d3b0e57c91a",
  "reference_id": "checkout:123",
  "amount": 10990,
  "currency": "BRL",
  "status": "PROCESSING",
  "created_at": "2026-08-10T12:00:00Z"
}
```

**Regras de idempotência**

- Repetir a mesma `Idempotency-Key` com o **mesmo corpo** retorna a transação
  original. Nenhum segundo pagamento é criado.
- Reutilizar a mesma chave com um **corpo diferente** é rejeitado e não cria
  pagamento (`422`). Os corpos são comparados por hash sobre uma forma canônica,
  portanto a ordem das chaves e espaços em branco não importam.
- Uma segunda requisição que chega com a mesma chave enquanto a primeira ainda
  está **em andamento** retorna `409`.
- Uma requisição que nunca terminou — porque o processo morreu no meio — retém
  a chave até uma lease configurável expirar; então ela pode ser recuperada.

A corrida é resolvida por uma restrição única no PostgreSQL, não por lock no
processo; portanto a garantia vale entre processos. Consulte
[ADR-0007](docs/decisions/adr-0007-idempotency-unique-constraint.md).

**Erros**

| Status | Quando |
| ------ | ------ |
| `400` | JSON malformado, campo desconhecido no corpo, `Idempotency-Key` ausente ou vazia |
| `401` | Credenciais ausentes ou inválidas |
| `409` | Requisição concorrente com chave de idempotência em andamento |
| `422` | Falha de validação ou chave de idempotência reutilizada com corpo diferente |

### POST /v1/webhook-subscriptions

Registra o endpoint de callback da conta técnica.

**Requisição**

```json
{
  "url": "http://consumer:8080/internal/provider-events",
  "events": ["payment.approved", "payment.rejected", "payment.processing"]
}
```

**Resposta — `201 Created`**

```json
{
  "subscription_id": "sub_0199a0b7-1d55-7e6c-9a83-41f7d2b8e05c",
  "url": "http://consumer:8080/internal/provider-events",
  "events": ["payment.approved", "payment.rejected", "payment.processing"],
  "secret": "whsec_...",
  "created_at": "2026-08-10T12:00:00Z"
}
```

O `secret` é **exibido exatamente uma vez**, nesta resposta. Ele é armazenado
criptografado, sob uma chave separada das credenciais do banco, e nunca é
retornado novamente. Se for perdido, registre uma nova assinatura.

A V1 permite uma assinatura ativa por conta técnica.

### POST /v1/webhook-deliveries/{delivery_id}/retry

Tenta novamente uma entrega que falhou. Retorna o estado atualizado da entrega
(status, quantidade de tentativas, horário da última tentativa e último status
HTTP).

## Webhooks

O DummyPay envia requisições `POST` com um corpo JSON para a URL assinada:

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

Tipos de evento: `payment.approved`, `payment.rejected`, `payment.processing`.

**Assinatura.** Cada requisição carrega:

```
X-Webhook-Signature: <hex HMAC-SHA-256 do corpo bruto, com chave no segredo da assinatura>
```

Verifique os **bytes brutos** recebidos antes de fazer o parse. Serializar o
JSON novamente altera a assinatura.

**Ciclo de vida da entrega.** Uma linha de entrega é escrita *antes* da chamada
HTTP, portanto nada é enviado sem registro. Cada entrega contém:

| Campo              | Significado                                  |
| ------------------ | -------------------------------------------- |
| `status`           | `PENDING`, `SENT` ou `FAILED`                |
| `attempt_count`    | Número de tentativas realizadas              |
| `last_attempted_at`| Horário da tentativa mais recente            |
| `last_http_status` | Status HTTP da última tentativa, se houver   |

Uma falha de rede ou resposta não-2xx mantém a entrega disponível para retry.
Repita-a pelo endpoint acima; o retry percorre o mesmo caminho da primeira
tentativa.

**A ordem não é garantida.** Entregas são reivindicadas de uma tabela de
trabalho por um worker de polling, então dois eventos do mesmo pagamento podem
chegar fora de ordem — um `payment.approved` pode alcançar seu endpoint antes
do `payment.processing` que o precedeu. Consumidores precisam tolerar isso. É
uma propriedade do desenho, não um defeito; consulte
[ADR-0008](docs/decisions/adr-0008-outbox-in-process-worker.md).

## Configuração

Tudo vem do ambiente. Não há segredos no repositório nem arquivos de
configuração com credenciais.

| Variável                          | Obrigatória | Padrão   | Finalidade |
| --------------------------------- | ----------- | -------- | ---------- |
| `DUMMYPAY_HTTP_ADDR`              | não         | `:8080`  | Endereço de escuta |
| `DUMMYPAY_DATABASE_URL`           | sim         | —        | DSN do PostgreSQL |
| `DUMMYPAY_ACCOUNT_KEY_ID`         | sim         | —        | Usuário Basic auth da conta técnica |
| `DUMMYPAY_ACCOUNT_KEY_SECRET`     | sim         | —        | Senha Basic auth da conta técnica |
| `DUMMYPAY_WEBHOOK_SECRET_ENC_KEY` | sim         | —        | Chave para criptografar segredos de webhook em repouso — **separada das credenciais do banco**, base64, exatamente 32 bytes após decodificação |
| `DUMMYPAY_PROCESSING_DELAY`       | não         | `3s`     | Atraso de liquidação dos cenários `PROCESSING`; `0s` nos testes |
| `DUMMYPAY_IDEMPOTENCY_LEASE`      | não         | `30s`    | Por quanto tempo uma chave de idempotência em andamento fica retida antes de poder ser recuperada |
| `DUMMYPAY_WORKER_POLL_INTERVAL`   | não         | `250ms` | Frequência com que o worker outbox procura trabalho vencido |
| `DUMMYPAY_WEBHOOK_TIMEOUT`        | não         | `5s`    | Timeout de uma tentativa individual de entrega webhook |

As regras completas de validação estão em [spec-v1.md §9](docs/spec-v1.md#9-configuration).
Copie [`.env.example`](.env.example) para `.env` e preencha valores reais —
ele está no `.gitignore`, então seu conteúdo não chega a um commit.

## Execução local

**Somente Docker é necessário.** Clone e execute:

```sh
make run                         # cria a imagem e inicia postgres + dummypay
```

Para parar: `make docker-stop`.

As credenciais da conta padrão são `local-dev-account` /
`change-me-to-a-long-random-value`. Se necessário, edite-as e a chave de
criptografia em `docker-compose.yml`.

Para desenvolvimento baseado em Go (requer Go e `.env`):

```sh
make run-local
```

### Verificação rápida

```sh
auth=$(echo -n 'local-dev-account:change-me-to-a-long-random-value' | base64)

curl http://localhost:8080/health
# {"status":"ok"}

curl -s -X POST http://localhost:8080/v1/payments \
  -H "Authorization: Basic $auth" \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: test-1" \
  -d '{"reference_id":"checkout:verify","amount":10990,"currency":"BRL","payment_token":"card_approved"}' | jq .

curl -s -X POST http://localhost:8080/v1/webhook-subscriptions \
  -H "Authorization: Basic $auth" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/webhook","events":["payment.approved","payment.rejected","payment.processing"]}' | jq .
```

O roteiro completo de smoke test está em [docs/smoke-test.md](docs/smoke-test.md).
Uma collection Insomnia com todos os endpoints e casos de erro está em
[docs/insomnia-collection.yaml](docs/insomnia-collection.yaml).

## Testes

A suíte é executada em dois níveis. Testes de domínio e casos de uso usam fakes
em memória e não exigem nada em execução. Testes de integração usam um
PostgreSQL real do `docker compose`, cada um em seu próprio schema — pois
idempotência e claim do worker são garantias do banco, e um fake aprovaria uma
implementação que sofre corrida em produção. Sem um banco acessível, eles são
ignorados; em CI, um skip é uma falha. Consulte
[ADR-0013](docs/decisions/adr-0013-integration-tests-real-postgres.md).

A suíte deve passar **sem acesso à rede externa**. A cobertura inclui:

- Pagamento aprovado
- Pagamento recusado
- Os dois cenários `PROCESSING`, incluindo a liquidação
- Replay idempotente retornando a transação original
- Mesma chave com corpo diferente
- Requisições concorrentes com a mesma chave retornando `409`
- Geração de assinatura HMAC sobre o corpo bruto
- Falha na entrega webhook e retry bem-sucedido

Execute com:

```sh
go test ./...
```

## Escopo

**No escopo da V1:** venda (autorizar + capturar em uma chamada), os quatro
tokens de cenário, idempotência, assinatura de webhook e entrega assinada com
persistência e retry.

**Fora do escopo:** tokenização, armazenamento de cartões e conformidade PCI
real; auth-only, pré-autorização e captura posterior ou parcial; reembolsos,
chargebacks, 3DS, parcelamento, antifraude, split payments, cobrança recorrente,
Pix e boleto; suporte a múltiplas contas, dashboard e API pública; integração
com qualquer outro serviço.

## Segurança

O DummyPay nunca recebe, armazena ou registra PAN, CVV, data de expiração ou
qualquer dado real de portador de cartão. A API não possui campo para isso. Os
resultados de pagamento são escolhidos somente por tokens de cenário opacos.

Este é um dublê de teste. Ele não processa dinheiro nem se conecta a uma rede
de cartões. Não o coloque em um fluxo de pagamentos de produção.
