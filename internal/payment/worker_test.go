package payment

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/clock"
)

func newTestWorker(claimer *fakeOutboxClaimer, payments *fakePaymentRepository, subs *fakeSubscriptionRepository, deliveries *fakeDeliveryRepository, outbox *fakeOutboxWriter, now time.Time) *Worker {
	return NewWorker(WorkerDeps{
		Tx:            &fakeTxManager{},
		Claimer:       claimer,
		Payments:      payments,
		Subscriptions: subs,
		Deliveries:    deliveries,
		Outbox:        outbox,
		IDs:           clock.UUIDv7Generator{},
		Clock:         clock.NewFake(now),
	})
}

// TestWorker_ProcessBatch_SettlesProcessingPaymentAndEnqueuesEvent covers the
// PROCESSING → APPROVED path: claiming a due SETTLE_PAYMENT item transitions
// the payment and, when an active subscription covers the resulting event,
// creates a delivery and enqueues DELIVER_WEBHOOK (spec §5).
func TestWorker_ProcessBatch_SettlesProcessingPaymentAndEnqueuesEvent(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 3, 0, time.UTC) // past the 3s delay

	p := Payment{
		ID:                    uuid.New(),
		AccountID:             uuid.New(),
		ReferenceID:           "checkout:123",
		Amount:                10990,
		Currency:              CurrencyBRL,
		Token:                 TokenCardProcessingApproved,
		Status:                StatusProcessing,
		ProviderTransactionID: uuid.New(),
		CreatedAt:             now.Add(-3 * time.Second),
		UpdatedAt:             now.Add(-3 * time.Second),
	}

	payments := &fakePaymentRepository{}
	payments.seed(p)

	claimer := &fakeOutboxClaimer{pending: []ClaimedWork{
		{ID: uuid.New(), Kind: OutboxSettlePayment, SubjectID: p.ID},
	}}
	subs := &fakeSubscriptionRepository{active: true, sub: Subscription{
		ID:     uuid.New(),
		Events: []EventType{EventPaymentApproved},
	}}
	deliveries := &fakeDeliveryRepository{}
	outbox := &fakeOutboxWriter{}

	w := newTestWorker(claimer, payments, subs, deliveries, outbox, now)

	err := w.ProcessBatch(context.Background(), 10)
	require.NoError(t, err)

	got, err := payments.FindByID(context.Background(), p.ID)
	require.NoError(t, err)
	require.Equal(t, StatusApproved, got.Status)
	require.True(t, now.Equal(got.UpdatedAt))

	require.Len(t, deliveries.drafts, 1)
	require.Equal(t, EventPaymentApproved, deliveries.drafts[0].EventType)
	require.Equal(t, p.ID, deliveries.drafts[0].PaymentID)

	deliverEntries := outbox.entriesOfKind(OutboxDeliverWebhook)
	require.Len(t, deliverEntries, 1)
	require.Equal(t, deliveries.ids[0], deliverEntries[0].SubjectID)
}

// TestWorker_ProcessBatch_NoActiveSubscription_SettlesWithoutEvent verifies
// settlement still happens with no subscription, and no delivery is created
// (spec §2, spec §5).
func TestWorker_ProcessBatch_NoActiveSubscription_SettlesWithoutEvent(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 3, 0, time.UTC)

	p := Payment{
		ID:                    uuid.New(),
		AccountID:             uuid.New(),
		Token:                 TokenCardProcessingDeclined,
		Status:                StatusProcessing,
		ProviderTransactionID: uuid.New(),
	}
	payments := &fakePaymentRepository{}
	payments.seed(p)

	claimer := &fakeOutboxClaimer{pending: []ClaimedWork{
		{ID: uuid.New(), Kind: OutboxSettlePayment, SubjectID: p.ID},
	}}
	subs := &fakeSubscriptionRepository{active: false}
	deliveries := &fakeDeliveryRepository{}
	outbox := &fakeOutboxWriter{}

	w := newTestWorker(claimer, payments, subs, deliveries, outbox, now)

	err := w.ProcessBatch(context.Background(), 10)
	require.NoError(t, err)

	got, err := payments.FindByID(context.Background(), p.ID)
	require.NoError(t, err)
	require.Equal(t, StatusRejected, got.Status)

	require.Empty(t, deliveries.drafts)
	require.Empty(t, outbox.entriesOfKind(OutboxDeliverWebhook))
}

// TestWorker_ProcessBatch_PaymentNoLongerProcessing_IsNoOp verifies that
// settling an already-settled payment does nothing — no status change, no
// event, no error — so claiming the same work twice is safe (spec §5).
func TestWorker_ProcessBatch_PaymentNoLongerProcessing_IsNoOp(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 3, 0, time.UTC)

	p := Payment{
		ID:                    uuid.New(),
		AccountID:             uuid.New(),
		Token:                 TokenCardProcessingApproved,
		Status:                StatusApproved, // already settled
		ProviderTransactionID: uuid.New(),
		UpdatedAt:             now.Add(-1 * time.Minute),
	}
	payments := &fakePaymentRepository{}
	payments.seed(p)

	claimer := &fakeOutboxClaimer{pending: []ClaimedWork{
		{ID: uuid.New(), Kind: OutboxSettlePayment, SubjectID: p.ID},
	}}
	subs := &fakeSubscriptionRepository{active: true, sub: Subscription{Events: []EventType{EventPaymentApproved}}}
	deliveries := &fakeDeliveryRepository{}
	outbox := &fakeOutboxWriter{}

	w := newTestWorker(claimer, payments, subs, deliveries, outbox, now)

	err := w.ProcessBatch(context.Background(), 10)
	require.NoError(t, err)

	got, err := payments.FindByID(context.Background(), p.ID)
	require.NoError(t, err)
	require.Equal(t, StatusApproved, got.Status)
	require.True(t, got.UpdatedAt.Equal(now.Add(-1*time.Minute)),
		"UpdatedAt must not change on a no-op settle")

	require.Empty(t, deliveries.drafts, "a no-op settle must not create a delivery")
}

// TestWorker_ProcessBatch_NothingDue_DoesNothing verifies that when the
// claimer reports no due work, ProcessBatch is a clean no-op.
func TestWorker_ProcessBatch_NothingDue_DoesNothing(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	payments := &fakePaymentRepository{}
	claimer := &fakeOutboxClaimer{} // nothing pending
	subs := &fakeSubscriptionRepository{}
	deliveries := &fakeDeliveryRepository{}
	outbox := &fakeOutboxWriter{}

	w := newTestWorker(claimer, payments, subs, deliveries, outbox, now)

	err := w.ProcessBatch(context.Background(), 10)
	require.NoError(t, err)

	require.Equal(t, 1, claimer.calls)
	require.Empty(t, deliveries.drafts)
}
