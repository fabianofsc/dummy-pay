package payment

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ResponseEncoder renders a Payment into the exact HTTP status and body bytes
// to store and return. It is supplied by the caller — the HTTP adapter — so
// internal/payment never depends on JSON encoding or identifier prefixing,
// which are boundary concerns. The use case calls it once per request, inside
// its transaction, and persists the result through IdempotencyStore so a
// replay returns those exact bytes rather than re-encoding them.
type ResponseEncoder func(Payment) (status int, body []byte)

// CreatePaymentRequest is the validated input to CreatePaymentUseCase.
// Validation — positive amount, known currency, known token, non-empty
// idempotency key — happens before this point, at the HTTP boundary or
// through the value objects' own constructors.
type CreatePaymentRequest struct {
	AccountID      uuid.UUID
	IdempotencyKey string
	ReferenceID    string
	Amount         Amount
	Currency       Currency
	Token          ScenarioToken
}

// CreatePaymentResult is what Execute returns on success — either a freshly
// created payment or a replayed one.
type CreatePaymentResult struct {
	Payment        Payment
	ResponseStatus int
	ResponseBody   []byte
	Replayed       bool
}

// CreatePaymentDeps are the ports and configuration CreatePaymentUseCase
// needs. ProcessingDelay and IdempotencyLease come from internal/config in
// the real service; tests set them directly.
type CreatePaymentDeps struct {
	Tx               TxManager
	Payments         PaymentRepository
	Idempotency      IdempotencyStore
	Outbox           OutboxWriter
	Subscriptions    SubscriptionRepository
	Deliveries       DeliveryRepository
	Clock            Clock
	IDs              IDGenerator
	ProcessingDelay  time.Duration
	IdempotencyLease time.Duration
}

// CreatePaymentUseCase orchestrates the create-payment flow of spec §4.1.
type CreatePaymentUseCase struct {
	deps CreatePaymentDeps
}

func NewCreatePaymentUseCase(deps CreatePaymentDeps) *CreatePaymentUseCase {
	return &CreatePaymentUseCase{deps: deps}
}

// Execute creates a payment idempotently: the caller that wins the claim on
// the idempotency key does the work, in one transaction, and records the
// exact response bytes for a later replay (spec §4.1).
func (uc *CreatePaymentUseCase) Execute(ctx context.Context, req CreatePaymentRequest, encode ResponseEncoder) (CreatePaymentResult, error) {
	fp := Fingerprint(req.ReferenceID, req.Amount, req.Currency, req.Token)
	now := uc.deps.Clock.Now()

	claimed, err := uc.deps.Idempotency.Claim(ctx, req.AccountID, req.IdempotencyKey, fp, now)
	if err != nil {
		return CreatePaymentResult{}, fmt.Errorf("claim idempotency key: %w", err)
	}
	if !claimed {
		// Someone else owns this key. Reading the existing record and
		// branching on it — replay, key reuse, expired-lease reclaim — is
		// spec §4.1's table, implemented in a later step. Until then the
		// conservative answer is the in-flight one.
		return CreatePaymentResult{}, ErrIdempotencyConflict
	}

	var result CreatePaymentResult
	err = uc.deps.Tx.Within(ctx, func(ctx context.Context) error {
		p := NewPayment(uc.deps.IDs.NewID(), req.AccountID, uc.deps.IDs.NewID(), req.ReferenceID, req.Amount, req.Currency, req.Token, now)

		if err := uc.deps.Payments.Insert(ctx, p); err != nil {
			return fmt.Errorf("insert payment: %w", err)
		}

		if p.Status == StatusProcessing {
			if err := uc.deps.Outbox.Enqueue(ctx, OutboxSettlePayment, p.ID, now.Add(uc.deps.ProcessingDelay)); err != nil {
				return fmt.Errorf("enqueue settlement: %w", err)
			}
		}

		sub, active, err := uc.deps.Subscriptions.LoadActive(ctx, req.AccountID)
		if err != nil {
			return fmt.Errorf("load active subscription: %w", err)
		}

		eventType := EventTypeForStatus(p.Status)
		if active && ShouldEmitEvent(sub.Events, eventType) {
			deliveryID, err := uc.deps.Deliveries.Create(ctx, DeliveryDraft{
				EventID:               uc.deps.IDs.NewID(),
				EventType:             eventType,
				SubscriptionID:        sub.ID,
				PaymentID:             p.ID,
				ReferenceID:           p.ReferenceID,
				Status:                p.Status,
				ProviderTransactionID: p.ProviderTransactionID,
				CreatedAt:             now,
			})
			if err != nil {
				return fmt.Errorf("create delivery: %w", err)
			}
			if err := uc.deps.Outbox.Enqueue(ctx, OutboxDeliverWebhook, deliveryID, now); err != nil {
				return fmt.Errorf("enqueue delivery: %w", err)
			}
		}

		status, body := encode(p)
		if err := uc.deps.Idempotency.Complete(ctx, req.AccountID, req.IdempotencyKey, p.ID, status, body, now); err != nil {
			return fmt.Errorf("complete idempotency key: %w", err)
		}

		result = CreatePaymentResult{Payment: p, ResponseStatus: status, ResponseBody: body, Replayed: false}
		return nil
	})
	if err != nil {
		return CreatePaymentResult{}, err
	}

	return result, nil
}
