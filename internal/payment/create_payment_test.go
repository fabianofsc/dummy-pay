package payment

import (
	"context"
	"testing"
	"time"

	"dummypay/internal/clock"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const testProcessingDelay = 3 * time.Second

// testEncoder stands in for the HTTP adapter's response encoder: it renders
// a Payment into the exact status and bytes the use case must store and
// return. It is deliberately not JSON-library-shaped — the use case must
// treat the bytes as opaque.
func testEncoder(p Payment) (int, []byte) {
	return 201, []byte("payment=" + p.ID.String() + ";status=" + string(p.Status))
}

// harness bundles the fakes with the use case built on top of them, so a
// test can act and then assert on what each port recorded.
type harness struct {
	uc            *CreatePaymentUseCase
	clock         *clock.Fake
	tx            *fakeTxManager
	payments      *fakePaymentRepository
	idempotency   *fakeIdempotencyStore
	outbox        *fakeOutboxWriter
	subscriptions *fakeSubscriptionRepository
	deliveries    *fakeDeliveryRepository
	accountID     uuid.UUID
	start         time.Time
}

// newHarness wires the use case against fakes with an active subscription
// covering all three event types, so events are actually produced.
func newHarness(t *testing.T) *harness {
	t.Helper()

	start := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	h := &harness{
		clock:       clock.NewFake(start),
		tx:          &fakeTxManager{},
		payments:    &fakePaymentRepository{},
		idempotency: newFakeIdempotencyStore(),
		outbox:      &fakeOutboxWriter{},
		subscriptions: &fakeSubscriptionRepository{
			active: true,
			sub: Subscription{
				ID:     uuid.New(),
				Events: []EventType{EventPaymentApproved, EventPaymentRejected, EventPaymentProcessing},
			},
		},
		deliveries: &fakeDeliveryRepository{},
		accountID:  uuid.New(),
		start:      start,
	}

	h.uc = NewCreatePaymentUseCase(CreatePaymentDeps{
		Tx:               h.tx,
		Payments:         h.payments,
		Idempotency:      h.idempotency,
		Outbox:           h.outbox,
		Subscriptions:    h.subscriptions,
		Deliveries:       h.deliveries,
		Clock:            h.clock,
		IDs:              clock.UUIDv7Generator{},
		ProcessingDelay:  testProcessingDelay,
		IdempotencyLease: 30 * time.Second,
	})
	return h
}

func (h *harness) request(t *testing.T, token ScenarioToken) CreatePaymentRequest {
	t.Helper()
	amount, err := NewAmount(10990)
	require.NoError(t, err)

	return CreatePaymentRequest{
		AccountID:      h.accountID,
		IdempotencyKey: "key-" + string(token),
		ReferenceID:    "checkout:123",
		Amount:         amount,
		Currency:       CurrencyBRL,
		Token:          token,
	}
}

func TestCreatePayment_HappyPaths(t *testing.T) {
	tests := []struct {
		token          ScenarioToken
		wantStatus     Status
		wantEvent      EventType
		wantSettleWork bool
	}{
		{TokenCardApproved, StatusApproved, EventPaymentApproved, false},
		{TokenCardDeclined, StatusRejected, EventPaymentRejected, false},
		{TokenCardProcessingApproved, StatusProcessing, EventPaymentProcessing, true},
		{TokenCardProcessingDeclined, StatusProcessing, EventPaymentProcessing, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.token), func(t *testing.T) {
			h := newHarness(t)
			req := h.request(t, tt.token)

			got, err := h.uc.Execute(context.Background(), req, testEncoder)
			require.NoError(t, err)

			// The payment itself.
			require.Equal(t, tt.wantStatus, got.Payment.Status)
			require.Equal(t, tt.token, got.Payment.Token)
			require.Equal(t, req.ReferenceID, got.Payment.ReferenceID)
			require.Equal(t, req.Amount, got.Payment.Amount)
			require.Equal(t, req.Currency, got.Payment.Currency)
			require.Equal(t, h.accountID, got.Payment.AccountID)
			require.NotEqual(t, uuid.Nil, got.Payment.ID)
			require.NotEqual(t, uuid.Nil, got.Payment.ProviderTransactionID)
			require.NotEqual(t, got.Payment.ID, got.Payment.ProviderTransactionID)
			require.True(t, h.start.Equal(got.Payment.CreatedAt))
			require.False(t, got.Replayed)

			// It was persisted, once, inside the transaction.
			require.Equal(t, 1, h.tx.calls)
			require.Len(t, h.payments.inserted, 1)
			if diff := cmp.Diff(got.Payment, h.payments.inserted[0]); diff != "" {
				t.Errorf("inserted payment differs from the returned one (-returned +inserted):\n%s", diff)
			}

			// Exactly one event, of the type the status dictates.
			require.Len(t, h.deliveries.drafts, 1)
			wantDraft := DeliveryDraft{
				EventID:               h.deliveries.drafts[0].EventID,
				EventType:             tt.wantEvent,
				SubscriptionID:        h.subscriptions.sub.ID,
				PaymentID:             got.Payment.ID,
				ReferenceID:           req.ReferenceID,
				Status:                tt.wantStatus,
				ProviderTransactionID: got.Payment.ProviderTransactionID,
				CreatedAt:             h.start,
			}
			if diff := cmp.Diff(wantDraft, h.deliveries.drafts[0]); diff != "" {
				t.Errorf("delivery draft (-want +got):\n%s", diff)
			}
			require.NotEqual(t, uuid.Nil, h.deliveries.drafts[0].EventID)

			// The delivery is enqueued for immediate sending.
			deliverWork := h.outbox.entriesOfKind(OutboxDeliverWebhook)
			require.Len(t, deliverWork, 1)
			require.Equal(t, h.deliveries.ids[0], deliverWork[0].SubjectID)
			require.True(t, h.start.Equal(deliverWork[0].DueAt))

			// Settlement work exists only for the PROCESSING tokens, and is
			// due at exactly now + the configured delay.
			settleWork := h.outbox.entriesOfKind(OutboxSettlePayment)
			if !tt.wantSettleWork {
				require.Empty(t, settleWork)
			} else {
				require.Len(t, settleWork, 1)
				require.Equal(t, got.Payment.ID, settleWork[0].SubjectID)
				wantDueAt := h.start.Add(testProcessingDelay)
				require.True(t, wantDueAt.Equal(settleWork[0].DueAt),
					"settlement due at %s, want exactly %s", settleWork[0].DueAt, wantDueAt)
			}

			// The response was encoded once and recorded for replay.
			wantStatusCode, wantBody := testEncoder(got.Payment)
			require.Equal(t, wantStatusCode, got.ResponseStatus)
			require.Equal(t, wantBody, got.ResponseBody)

			rec, err := h.idempotency.Load(context.Background(), h.accountID, req.IdempotencyKey)
			require.NoError(t, err)
			require.Equal(t, IdempotencyCompleted, rec.State)
			require.Equal(t, got.Payment.ID, rec.PaymentID)
			require.Equal(t, wantStatusCode, rec.ResponseStatus)
			require.Equal(t, wantBody, rec.ResponseBody)
			require.True(t, h.start.Equal(rec.CompletedAt))
		})
	}
}

// With no active subscription there is nothing to deliver to, so no delivery
// is recorded and nothing is enqueued for sending (spec §2). Everything else
// about the payment — including its settlement work — happens as normal.
func TestCreatePayment_NoActiveSubscription_CreatesNoDelivery(t *testing.T) {
	h := newHarness(t)
	h.subscriptions.active = false
	req := h.request(t, TokenCardProcessingApproved)

	got, err := h.uc.Execute(context.Background(), req, testEncoder)
	require.NoError(t, err)

	require.Empty(t, h.deliveries.drafts)
	require.Empty(t, h.outbox.entriesOfKind(OutboxDeliverWebhook))

	require.Equal(t, StatusProcessing, got.Payment.Status)
	require.Len(t, h.payments.inserted, 1)

	settleWork := h.outbox.entriesOfKind(OutboxSettlePayment)
	require.Len(t, settleWork, 1)
	require.Equal(t, got.Payment.ID, settleWork[0].SubjectID)
	wantDueAt := h.start.Add(testProcessingDelay)
	require.True(t, wantDueAt.Equal(settleWork[0].DueAt))

	rec, err := h.idempotency.Load(context.Background(), h.accountID, req.IdempotencyKey)
	require.NoError(t, err)
	require.Equal(t, IdempotencyCompleted, rec.State)
}

// A subscription that exists but does not list the event type produced is
// treated exactly like no subscription at all (spec §2).
func TestCreatePayment_SubscriptionNotCoveringEvent_CreatesNoDelivery(t *testing.T) {
	h := newHarness(t)
	h.subscriptions.sub.Events = []EventType{EventPaymentRejected}
	req := h.request(t, TokenCardApproved)

	got, err := h.uc.Execute(context.Background(), req, testEncoder)
	require.NoError(t, err)

	require.Empty(t, h.deliveries.drafts)
	require.Empty(t, h.outbox.entriesOfKind(OutboxDeliverWebhook))
	require.Equal(t, StatusApproved, got.Payment.Status)
	require.Len(t, h.payments.inserted, 1)
}
