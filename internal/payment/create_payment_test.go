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

const (
	testProcessingDelay  = 3 * time.Second
	testIdempotencyLease = 30 * time.Second
)

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
		IdempotencyLease: testIdempotencyLease,
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

// fingerprintOf computes the fingerprint the use case will compute for req,
// so a test can seed an idempotency record that matches — or, by passing a
// changed request, one that deliberately does not.
func fingerprintOf(req CreatePaymentRequest) [32]byte {
	return Fingerprint(req.ReferenceID, req.Amount, req.Currency, req.Token)
}

// requireNothingCreated asserts that the call under test did not enter the
// owner flow: no transaction, no payment, no work, no delivery, and no
// completion of the idempotency record.
func (h *harness) requireNothingCreated(t *testing.T) {
	t.Helper()
	require.Equal(t, 0, h.tx.calls, "no transaction should have been opened")
	require.Empty(t, h.payments.inserted, "no payment should have been inserted")
	require.Empty(t, h.outbox.entries, "no work should have been enqueued")
	require.Empty(t, h.deliveries.drafts, "no delivery should have been created")
	require.Equal(t, 0, h.idempotency.completeCalls, "the idempotency record should not have been completed")
}

func TestCreatePayment_HappyPaths(t *testing.T) {
	tests := []struct {
		token          ScenarioToken
		wantStatus     Status
		wantEvent      EventType
		wantSettleWork bool
	}{
		{TokenCardApproved, StatusProcessing, EventPaymentProcessing, true},
		{TokenCardDeclined, StatusProcessing, EventPaymentProcessing, true},
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

			// Every payment starts PROCESSING, so settlement work is due at
			// exactly now + the configured delay.
			settleWork := h.outbox.entriesOfKind(OutboxSettlePayment)
			if tt.wantSettleWork {
				require.Len(t, settleWork, 1)
				require.Equal(t, got.Payment.ID, settleWork[0].SubjectID)
				wantDueAt := h.start.Add(testProcessingDelay)
				require.True(t, wantDueAt.Equal(settleWork[0].DueAt),
					"settlement due at %s, want exactly %s", settleWork[0].DueAt, wantDueAt)
			} else {
				require.Empty(t, settleWork)
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
	require.Equal(t, StatusProcessing, got.Payment.Status)
	require.Len(t, h.payments.inserted, 1)
}

// A replay — same key, same body, first request already COMPLETED — returns
// the stored response verbatim and creates nothing at all (spec §4.1).
func TestCreatePayment_Replay_ReturnsStoredResponseAndCreatesNothing(t *testing.T) {
	h := newHarness(t)
	req := h.request(t, TokenCardApproved)

	original := NewPayment(uuid.New(), h.accountID, uuid.New(), req.ReferenceID, req.Amount, req.Currency, req.Token, h.start)
	h.payments.seed(original)

	wantStatus, wantBody := testEncoder(original)
	h.idempotency.seed(IdempotencyRecord{
		AccountID:          h.accountID,
		IdempotencyKey:     req.IdempotencyKey,
		RequestFingerprint: fingerprintOf(req),
		State:              IdempotencyCompleted,
		PaymentID:          original.ID,
		ResponseStatus:     wantStatus,
		ResponseBody:       wantBody,
		ClaimedAt:          h.start,
		CompletedAt:        h.start,
	})

	got, err := h.uc.Execute(context.Background(), req, testEncoder)
	require.NoError(t, err)

	require.True(t, got.Replayed)
	require.Equal(t, wantStatus, got.ResponseStatus)
	require.Equal(t, wantBody, got.ResponseBody)
	if diff := cmp.Diff(original, got.Payment); diff != "" {
		t.Errorf("replayed payment (-want +got):\n%s", diff)
	}

	// The whole point: the second request did no work.
	h.requireNothingCreated(t)
}

// The fingerprint is checked before the state, so a mismatched body is a
// client error whatever the original request is doing (spec §4.1).
func TestCreatePayment_KeyReusedWithDifferentBody_IsRejected(t *testing.T) {
	states := []IdempotencyState{IdempotencyInFlight, IdempotencyCompleted}

	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			h := newHarness(t)
			req := h.request(t, TokenCardApproved)

			// The first request used the same key for a different body.
			other := req
			other.ReferenceID = "checkout:999"
			require.NotEqual(t, fingerprintOf(req), fingerprintOf(other))

			rec := IdempotencyRecord{
				AccountID:          h.accountID,
				IdempotencyKey:     req.IdempotencyKey,
				RequestFingerprint: fingerprintOf(other),
				State:              state,
				ClaimedAt:          h.start,
			}
			if state == IdempotencyCompleted {
				original := NewPayment(uuid.New(), h.accountID, uuid.New(), other.ReferenceID, other.Amount, other.Currency, other.Token, h.start)
				h.payments.seed(original)
				rec.PaymentID = original.ID
				rec.ResponseStatus, rec.ResponseBody = testEncoder(original)
				rec.CompletedAt = h.start
			}
			h.idempotency.seed(rec)

			_, err := h.uc.Execute(context.Background(), req, testEncoder)
			require.ErrorIs(t, err, ErrIdempotencyKeyReuse)

			h.requireNothingCreated(t)
			require.Equal(t, 0, h.idempotency.reclaimCalls, "a mismatched fingerprint must never reclaim")
		})
	}
}

// A second request arriving while the first is still in flight, within its
// lease, is a conflict (spec §4.1). The lease boundary is inclusive: a
// record claimed exactly one lease ago is still held.
func TestCreatePayment_InFlightWithinLease_Conflicts(t *testing.T) {
	tests := []struct {
		name       string
		claimedAgo time.Duration
	}{
		{"just claimed", 0},
		{"halfway through the lease", testIdempotencyLease / 2},
		{"exactly at the lease boundary", testIdempotencyLease},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			req := h.request(t, TokenCardApproved)

			h.idempotency.seed(IdempotencyRecord{
				AccountID:          h.accountID,
				IdempotencyKey:     req.IdempotencyKey,
				RequestFingerprint: fingerprintOf(req),
				State:              IdempotencyInFlight,
				ClaimedAt:          h.start.Add(-tt.claimedAgo),
			})

			_, err := h.uc.Execute(context.Background(), req, testEncoder)
			require.ErrorIs(t, err, ErrIdempotencyConflict)

			h.requireNothingCreated(t)
			require.Equal(t, 0, h.idempotency.reclaimCalls, "a held lease must not be reclaimed")
		})
	}
}

// An IN_FLIGHT record whose lease has expired — the process that claimed it
// died mid-flight — is reclaimable, and the reclaiming request proceeds as
// the owner (spec §4.1).
func TestCreatePayment_ExpiredLease_ReclaimsAndProceedsAsOwner(t *testing.T) {
	h := newHarness(t)
	req := h.request(t, TokenCardProcessingApproved)

	h.idempotency.seed(IdempotencyRecord{
		AccountID:          h.accountID,
		IdempotencyKey:     req.IdempotencyKey,
		RequestFingerprint: fingerprintOf(req),
		State:              IdempotencyInFlight,
		ClaimedAt:          h.start.Add(-testIdempotencyLease - time.Nanosecond),
	})

	got, err := h.uc.Execute(context.Background(), req, testEncoder)
	require.NoError(t, err)

	require.Equal(t, 1, h.idempotency.reclaimCalls)
	require.False(t, got.Replayed, "a reclaimed key produces a new payment, not a replay")

	// The full owner flow ran: payment, settlement work, delivery, response.
	require.Equal(t, 1, h.tx.calls)
	require.Len(t, h.payments.inserted, 1)
	require.Equal(t, StatusProcessing, got.Payment.Status)
	if diff := cmp.Diff(got.Payment, h.payments.inserted[0]); diff != "" {
		t.Errorf("inserted payment differs from the returned one (-returned +inserted):\n%s", diff)
	}

	settleWork := h.outbox.entriesOfKind(OutboxSettlePayment)
	require.Len(t, settleWork, 1)
	require.Equal(t, got.Payment.ID, settleWork[0].SubjectID)
	require.True(t, h.start.Add(testProcessingDelay).Equal(settleWork[0].DueAt))
	require.Len(t, h.deliveries.drafts, 1)

	wantStatus, wantBody := testEncoder(got.Payment)
	require.Equal(t, wantStatus, got.ResponseStatus)
	require.Equal(t, wantBody, got.ResponseBody)

	require.Equal(t, 1, h.idempotency.completeCalls)
	rec, err := h.idempotency.Load(context.Background(), h.accountID, req.IdempotencyKey)
	require.NoError(t, err)
	require.Equal(t, IdempotencyCompleted, rec.State)
	require.Equal(t, got.Payment.ID, rec.PaymentID)
	require.Equal(t, wantBody, rec.ResponseBody)
}

// A request whose work outlives its lease can have its key reclaimed while it
// is still running. Complete refuses it, and Execute must surface that as an
// error rather than reporting a success whose response was never recorded —
// the failure returns from inside Tx.Within, so the payment it created rolls
// back with it (proved against a real database in Step 5.3).
func TestCreatePayment_CompleteLosesOwnership_FailsRatherThanReportingSuccess(t *testing.T) {
	h := newHarness(t)
	req := h.request(t, TokenCardApproved)
	h.idempotency.completeLosesOwnership = true

	got, err := h.uc.Execute(context.Background(), req, testEncoder)
	require.ErrorIs(t, err, ErrIdempotencyOwnershipLost)
	require.Equal(t, CreatePaymentResult{}, got, "a failed Execute must return no result")
}

// Two requests can race to reclaim the same abandoned key. The loser is told
// the key is in flight — because, thanks to the winner, it now is.
func TestCreatePayment_ExpiredLease_LosingTheReclaimRace_Conflicts(t *testing.T) {
	h := newHarness(t)
	req := h.request(t, TokenCardApproved)

	h.idempotency.seed(IdempotencyRecord{
		AccountID:          h.accountID,
		IdempotencyKey:     req.IdempotencyKey,
		RequestFingerprint: fingerprintOf(req),
		State:              IdempotencyInFlight,
		ClaimedAt:          h.start.Add(-2 * testIdempotencyLease),
	})
	h.idempotency.reclaimLoses = true

	_, err := h.uc.Execute(context.Background(), req, testEncoder)
	require.ErrorIs(t, err, ErrIdempotencyConflict)

	require.Equal(t, 1, h.idempotency.reclaimCalls, "the expired lease should have been reclaimed")
	h.requireNothingCreated(t)
}
