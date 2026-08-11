package payment

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/clock"
)

func newTestRetryUseCase(deliveries *fakeDeliveryRepository, outbox *fakeOutboxWriter, now time.Time) *RetryDeliveryUseCase {
	return NewRetryDeliveryUseCase(RetryDeliveryDeps{
		Tx:         &fakeTxManager{},
		Deliveries: deliveries,
		Outbox:     outbox,
		Clock:      clock.NewFake(now),
	})
}

// TestRetryDeliveryUseCase_FailedDelivery_ReEnqueuesAndReturnsState verifies
// that retrying a FAILED delivery sets it back to PENDING, enqueues a
// DELIVER_WEBHOOK work item due immediately, and returns the updated state
// (spec §4.3).
func TestRetryDeliveryUseCase_FailedDelivery_ReEnqueuesAndReturnsState(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	accountID := uuid.New()
	deliveryID := uuid.New()

	deliveries := &fakeDeliveryRepository{}
	d := newTestDelivery(deliveryID, uuid.New())
	d.Status = DeliveryFailed
	d.AttemptCount = 1
	deliveries.seedDelivery(d)

	outbox := &fakeOutboxWriter{}
	uc := newTestRetryUseCase(deliveries, outbox, now)

	got, err := uc.Execute(context.Background(), accountID, deliveryID)
	require.NoError(t, err)
	require.Equal(t, DeliveryPending, got.Status)

	updated, err := deliveries.FindByID(context.Background(), deliveryID)
	require.NoError(t, err)
	require.Equal(t, DeliveryPending, updated.Status)

	entries := outbox.entriesOfKind(OutboxDeliverWebhook)
	require.Len(t, entries, 1)
	require.Equal(t, deliveryID, entries[0].SubjectID)
	require.True(t, now.Equal(entries[0].DueAt), "retry must enqueue due immediately")
}

// TestRetryDeliveryUseCase_SentDelivery_ReturnsErrDeliveryNotRetryable
// verifies retrying an already-SENT delivery is rejected and enqueues
// nothing (spec §4.3, 409 delivery_not_retryable).
func TestRetryDeliveryUseCase_SentDelivery_ReturnsErrDeliveryNotRetryable(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	accountID := uuid.New()
	deliveryID := uuid.New()

	deliveries := &fakeDeliveryRepository{}
	d := newTestDelivery(deliveryID, uuid.New())
	d.Status = DeliverySent
	d.AttemptCount = 1
	deliveries.seedDelivery(d)

	outbox := &fakeOutboxWriter{}
	uc := newTestRetryUseCase(deliveries, outbox, now)

	_, err := uc.Execute(context.Background(), accountID, deliveryID)
	require.ErrorIs(t, err, ErrDeliveryNotRetryable)

	require.Empty(t, outbox.entriesOfKind(OutboxDeliverWebhook))
	require.Equal(t, 0, deliveries.recordAttemptCalls)
}

// TestRetryDeliveryUseCase_UnknownDelivery_ReturnsErrDeliveryNotFound
// verifies an unknown id is reported as not found.
func TestRetryDeliveryUseCase_UnknownDelivery_ReturnsErrDeliveryNotFound(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	deliveries := &fakeDeliveryRepository{}
	outbox := &fakeOutboxWriter{}
	uc := newTestRetryUseCase(deliveries, outbox, now)

	_, err := uc.Execute(context.Background(), uuid.New(), uuid.New())
	require.ErrorIs(t, err, ErrDeliveryNotFound)
}

// TestRetryThenWorker_ResendsByteIdenticalPayloadWithIncrementedAttemptCount
// proves the stored-payload decision from spec §3: a retry re-sends the
// exact bytes stored at creation, not a re-serialisation, and the worker
// records a second attempt on top of the first rather than resetting the
// count.
func TestRetryThenWorker_ResendsByteIdenticalPayloadWithIncrementedAttemptCount(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	accountID := uuid.New()
	subID := uuid.New()
	deliveryID := uuid.New()

	deliveries := &fakeDeliveryRepository{}
	d := newTestDelivery(deliveryID, subID)
	d.Status = DeliveryFailed
	d.AttemptCount = 1
	d.LastHTTPStatus = 500
	deliveries.seedDelivery(d)
	originalPayload := d.Payload

	outbox := &fakeOutboxWriter{}
	retryUC := newTestRetryUseCase(deliveries, outbox, now)

	_, err := retryUC.Execute(context.Background(), accountID, deliveryID)
	require.NoError(t, err)

	entries := outbox.entriesOfKind(OutboxDeliverWebhook)
	require.Len(t, entries, 1)

	claimer := &fakeOutboxClaimer{pending: []ClaimedWork{
		{ID: uuid.New(), Kind: OutboxDeliverWebhook, SubjectID: entries[0].SubjectID},
	}}
	subs := &fakeSubscriptionRepository{
		deliveryTargetURL:    "http://consumer.example/webhook",
		deliveryTargetSecret: "whsec_test",
	}
	sender := &fakeSender{result: 200}

	w := newTestWorkerWithSender(claimer, &fakePaymentRepository{}, subs, deliveries, &fakeOutboxWriter{}, sender, now)
	require.NoError(t, w.ProcessBatch(context.Background(), 10))

	require.Equal(t, originalPayload, sender.gotBody, "the retry must send byte-identical bytes to the first attempt")

	final, err := deliveries.FindByID(context.Background(), deliveryID)
	require.NoError(t, err)
	require.Equal(t, DeliverySent, final.Status)
	require.Equal(t, 2, final.AttemptCount, "attempt_count must increment from the first attempt, not reset")
}
