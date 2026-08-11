package payment

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// WorkerDeps are the ports Worker needs (spec §5, spec §8).
type WorkerDeps struct {
	Tx            TxManager
	Claimer       OutboxClaimer
	Payments      PaymentRepository
	Subscriptions SubscriptionRepository
	Deliveries    DeliveryRepository
	Outbox        OutboxWriter
	IDs           IDGenerator
	Clock         Clock
}

// Worker processes due outbox_work items: settling PROCESSING payments and
// — once Step 9.3 adds it — sending webhook deliveries. It is a plain method
// that processes one batch and returns; cmd/dummypay wraps it in a ticker,
// and tests call it directly after advancing a test clock — no timer, no
// test sleep (ADR-0012).
type Worker struct {
	deps WorkerDeps
}

// NewWorker constructs a Worker against deps.
func NewWorker(deps WorkerDeps) *Worker {
	return &Worker{deps: deps}
}

// ProcessBatch claims up to batch due work items and processes each in turn.
// A claim failure aborts the whole batch; a single item's processing failure
// also aborts rather than silently skipping — surfacing it to the caller
// (the ticker loop in cmd/dummypay) is more useful than losing it.
func (w *Worker) ProcessBatch(ctx context.Context, batch int) error {
	now := w.deps.Clock.Now()

	claimed, err := w.deps.Claimer.ClaimDue(ctx, now, batch)
	if err != nil {
		return fmt.Errorf("claim due work: %w", err)
	}

	for _, item := range claimed {
		switch item.Kind {
		case OutboxSettlePayment:
			if err := w.settlePayment(ctx, item.SubjectID, now); err != nil {
				return fmt.Errorf("settle payment %s: %w", item.SubjectID, err)
			}
		case OutboxDeliverWebhook:
			// Step 9.3.
		default:
			return fmt.Errorf("unknown outbox kind %q", item.Kind)
		}
	}

	return nil
}

// settlePayment loads the payment, transitions it if it is still
// PROCESSING, and — in the same transaction — creates the delivery and
// enqueues DELIVER_WEBHOOK if an active subscription covers the resulting
// event. A payment no longer PROCESSING (already settled, or somehow
// claimed twice) is a no-op, not an error (spec §5).
func (w *Worker) settlePayment(ctx context.Context, paymentID uuid.UUID, now time.Time) error {
	return w.deps.Tx.Within(ctx, func(ctx context.Context) error {
		p, err := w.deps.Payments.FindByID(ctx, paymentID)
		if err != nil {
			return fmt.Errorf("load payment: %w", err)
		}

		if p.Status != StatusProcessing {
			// Already settled — or was never PROCESSING to begin with.
			// Idempotent no-op, per spec §5.
			return nil
		}

		p.Settle(now)
		if err := w.deps.Payments.Update(ctx, p); err != nil {
			return fmt.Errorf("update payment: %w", err)
		}

		sub, active, err := w.deps.Subscriptions.LoadActive(ctx, p.AccountID)
		if err != nil {
			return fmt.Errorf("load active subscription: %w", err)
		}

		eventType := EventTypeForStatus(p.Status)
		if active && ShouldEmitEvent(sub.Events, eventType) {
			deliveryID, err := w.deps.Deliveries.Create(ctx, DeliveryDraft{
				EventID:               w.deps.IDs.NewID(),
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
			if err := w.deps.Outbox.Enqueue(ctx, OutboxDeliverWebhook, deliveryID, now); err != nil {
				return fmt.Errorf("enqueue delivery: %w", err)
			}
		}

		return nil
	})
}
