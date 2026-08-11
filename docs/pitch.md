# DummyPay — Pitch

> A card payment provider that isn't real, on purpose.

## The problem

Every team that takes card payments has to write the same code: build a charge
request, handle an approval, handle a decline, survive a timeout, reconcile a
webhook that arrives late, and never charge the customer twice. That code is
some of the highest-consequence code in the product.

It is also some of the hardest code to test.

The usual options all leak:

- **Provider sandboxes** are shared, remote, and occasionally down. A test suite
  that depends on one is a test suite that fails for reasons that have nothing
  to do with your change. CI needs network access and real credentials on disk.
- **Mocked HTTP clients** test that you can parse a response you wrote yourself.
  They never exercise idempotency keys, retries, concurrent requests to the same
  order, or a webhook that shows up before your database transaction commits.
- **The "we'll test it in staging" plan** means the first real exercise of your
  decline path is a customer hitting it.

The result is predictable. Approval and decline get tested. Everything else —
the asynchronous path, the duplicate request, the webhook that failed and needs
replaying — gets discovered in production.

## What DummyPay is

DummyPay is a payment service provider you run yourself. It speaks a small,
honest HTTP API, keeps its own PostgreSQL database, and behaves like a real PSP
in exactly the ways that make integration code hard to get right.

You point your checkout at it instead of a real provider, and you get:

**Deterministic scenarios instead of test cards.** You don't send a card number,
because DummyPay never wants one. You send an opaque scenario token —
`card_approved`, `card_declined`, `card_processing_approved`,
`card_processing_declined` — and the outcome is exactly what the name says,
every single time. No test-card tables, no "this number stopped working last
quarter."

**A real asynchronous path.** Two of the four scenarios return `PROCESSING`
first and settle later. That is the path most integrations get wrong, and it is
usually the one that's hardest to trigger on demand. Here it's a token. The
settlement delay is configured per environment: a few seconds locally so you can
watch it happen, zero in tests so the suite stays fast and deterministic.

**Idempotency and concurrency that actually push back.** Replaying an
`Idempotency-Key` returns the original transaction. Reusing a key with a
different body does not quietly create a second charge. Firing two concurrent
requests with the same key while the first is still in flight gets you a `409`,
not a race. These are the semantics real providers enforce, and the only way to
know your client handles them is to run against something that enforces them.

**Webhooks you can break and repair.** Subscriptions are registered up front,
not attached to individual payments — the way real providers do it. Every
delivery is persisted *before* it is attempted, carries an HMAC-SHA-256
signature over the raw body, and records its own status, attempt count, last
attempt time, and last HTTP status. When your consumer is down, the delivery is
`FAILED` and sitting in a table where you can see it. When it comes back, you
replay it with one call. Signature verification is something you can now write a
real test for.

Delivery order is not guaranteed, deliberately. Real providers don't guarantee
it either, and a consumer that quietly depends on ordering is exactly the kind
of bug worth finding on a laptop rather than in production.

**Nothing sensitive, ever.** DummyPay does not accept a PAN, a CVV, or an expiry
date. There is no field to put them in. It cannot leak cardholder data because
it never has any — which means it can run on a laptop, in CI, in a shared dev
environment, or in a demo, without a compliance conversation.

## What it is not

DummyPay is not a payment processor, and it is not pretending to be one. It
moves no money and touches no card network. It is not a PCI-compliant system,
because it deliberately places itself outside the scope that PCI describes.

It is also intentionally small. No tokenization or card vault. No auth-only,
pre-authorization, or later/partial capture. No refunds, chargebacks, 3DS,
installments, anti-fraud, split payments, subscriptions, Pix, or boleto. No
multi-tenancy, no dashboard, no public API. One technical account per
environment, one active webhook subscription, one operation: sell.

Every one of those omissions is a decision, not a gap. The value of this project
is that the surface stays small enough to trust.

## Who it's for

Backend teams building or maintaining a checkout, who want their payment
integration covered by tests that run offline and pass for the right reasons.

## How it's built

Go, with its own PostgreSQL and no other dependency. Three endpoints behind
`chi`, a domain package that imports no infrastructure, and asynchronous work —
delayed settlement and webhook delivery alike — carried by an outbox table
written in the same transaction as the change that produced it. Nothing is ever
sent that wasn't first recorded.

The two properties everything else serves: **the semantics under test are the
semantics shipped** — idempotency and worker claiming are proved against a real
PostgreSQL, never a fake that would agree with whatever we wrote — and **no test
waits on the clock**, because time is injected and schedule is data.

Each choice, including the ones that were close and what they cost, is recorded
in [decisions/](decisions/).

## What "done" looks like for V1

The service starts and the full test suite passes with no external network. The
suite covers approval, decline, both asynchronous scenarios, idempotent replay,
concurrent duplicate requests, HMAC signature generation, webhook delivery
failure, and successful replay. A developer can clone the repository and have it
running locally by following the README.

---

See the [README](../README.md) for the API contract and local setup, and
[decisions/](decisions/) for the architecture decision records.
