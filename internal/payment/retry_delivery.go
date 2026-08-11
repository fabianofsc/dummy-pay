package payment

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
)

// ErrDeliveryNotRetryable is returned when a retry is attempted on a
// delivery whose status is already SENT — there is nothing to retry
// (spec §4.3, 409 delivery_not_retryable).
var ErrDeliveryNotRetryable = errors.New("delivery is already SENT")

// RetryDeliveryDeps are the ports RetryDeliveryUseCase needs (spec §4.3,
// spec §8).
type RetryDeliveryDeps struct {
	Tx         TxManager
	Deliveries DeliveryRepository
	Outbox     OutboxWriter
	Clock      Clock
}

// RetryDeliveryUseCase re-enqueues a failed delivery for another attempt
// (spec §4.3).
type RetryDeliveryUseCase struct {
	deps RetryDeliveryDeps
}

// NewRetryDeliveryUseCase constructs a RetryDeliveryUseCase against deps.
func NewRetryDeliveryUseCase(deps RetryDeliveryDeps) *RetryDeliveryUseCase {
	return &RetryDeliveryUseCase{deps: deps}
}

// Execute loads the delivery, scoped to accountID, and — unless it is
// already SENT — sets it back to PENDING and enqueues a DELIVER_WEBHOOK work
// item due immediately, in one transaction. Retry re-enqueues; it does not
// send. The actual send travels the ordinary worker path (spec §4.3).
func (uc *RetryDeliveryUseCase) Execute(ctx context.Context, accountID, deliveryID uuid.UUID) (Delivery, error) {
	d, err := uc.deps.Deliveries.FindByIDForAccount(ctx, accountID, deliveryID)
	if err != nil {
		return Delivery{}, err
	}

	if d.Status == DeliverySent {
		return Delivery{}, ErrDeliveryNotRetryable
	}

	now := uc.deps.Clock.Now()
	err = uc.deps.Tx.Within(ctx, func(ctx context.Context) error {
		if err := uc.deps.Deliveries.UpdateStatus(ctx, deliveryID, DeliveryPending); err != nil {
			return fmt.Errorf("update delivery status: %w", err)
		}
		if err := uc.deps.Outbox.Enqueue(ctx, OutboxDeliverWebhook, deliveryID, now); err != nil {
			return fmt.Errorf("enqueue delivery: %w", err)
		}
		return nil
	})
	if err != nil {
		return Delivery{}, err
	}

	d.Status = DeliveryPending
	return d, nil
}
