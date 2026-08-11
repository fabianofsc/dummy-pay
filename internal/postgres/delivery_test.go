package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/payment"
)

func TestDeliveryRepository_Create_ThenFindByID_ReturnsStoredPayload(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_delivery_create", now)
	require.NoError(t, err)

	subRepo := NewSubscriptionRepository(pool, testEncKey())
	subID := uuid.New()
	require.NoError(t, subRepo.Create(ctx, subID, accountID, "http://consumer.example/webhook",
		[]payment.EventType{payment.EventPaymentApproved}, "whsec_delivery_test", now))

	p := newTestPayment(t, accountID, payment.TokenCardApproved, now)
	require.NoError(t, NewPaymentRepository(pool).Insert(ctx, p))

	repo := NewDeliveryRepository(pool, &recordingIDGenerator{})
	draft := payment.DeliveryDraft{
		EventID:               uuid.New(),
		EventType:             payment.EventPaymentApproved,
		SubscriptionID:        subID,
		PaymentID:             p.ID,
		ReferenceID:           p.ReferenceID,
		Status:                payment.StatusApproved,
		ProviderTransactionID: p.ProviderTransactionID,
		CreatedAt:             now,
	}

	deliveryID, err := repo.Create(ctx, draft)
	require.NoError(t, err)

	got, err := repo.FindByID(ctx, deliveryID)
	require.NoError(t, err)

	require.Equal(t, deliveryID, got.ID)
	require.Equal(t, subID, got.SubscriptionID)
	require.Equal(t, p.ID, got.PaymentID)
	require.Equal(t, draft.EventID, got.EventID)
	require.Equal(t, payment.EventPaymentApproved, got.EventType)
	require.Equal(t, payment.DeliveryPending, got.Status)
	require.Equal(t, 0, got.AttemptCount)
	require.Equal(t, 0, got.LastHTTPStatus)
	require.True(t, got.LastAttemptedAt.IsZero())

	require.Contains(t, string(got.Payload), `"pay_`+p.ID.String())
	require.Contains(t, string(got.Payload), `"txn_`+p.ProviderTransactionID.String())
}

func TestDeliveryRepository_RecordAttempt_SentSetsStatusAndHTTPStatus(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_delivery_attempt", now)
	require.NoError(t, err)

	subRepo := NewSubscriptionRepository(pool, testEncKey())
	subID := uuid.New()
	require.NoError(t, subRepo.Create(ctx, subID, accountID, "http://consumer.example/webhook",
		[]payment.EventType{payment.EventPaymentApproved}, "whsec_delivery_test", now))

	p := newTestPayment(t, accountID, payment.TokenCardApproved, now)
	require.NoError(t, NewPaymentRepository(pool).Insert(ctx, p))

	repo := NewDeliveryRepository(pool, &recordingIDGenerator{})
	deliveryID, err := repo.Create(ctx, payment.DeliveryDraft{
		EventID:               uuid.New(),
		EventType:             payment.EventPaymentApproved,
		SubscriptionID:        subID,
		PaymentID:             p.ID,
		ReferenceID:           p.ReferenceID,
		Status:                payment.StatusApproved,
		ProviderTransactionID: p.ProviderTransactionID,
		CreatedAt:             now,
	})
	require.NoError(t, err)

	attemptedAt := now.Add(1 * time.Second)
	require.NoError(t, repo.RecordAttempt(ctx, deliveryID, payment.DeliverySent, 200, attemptedAt))

	got, err := repo.FindByID(ctx, deliveryID)
	require.NoError(t, err)
	require.Equal(t, payment.DeliverySent, got.Status)
	require.Equal(t, 1, got.AttemptCount)
	require.Equal(t, 200, got.LastHTTPStatus)
	require.True(t, attemptedAt.Equal(got.LastAttemptedAt))
}

// TestDeliveryRepository_RecordAttempt_TransportFailure_StoresNullHTTPStatus
// proves httpStatus 0 is stored as a real SQL NULL, not the integer zero —
// necessary so a transport failure is distinguishable from a (nonexistent)
// HTTP status 0 (spec §5).
func TestDeliveryRepository_RecordAttempt_TransportFailure_StoresNullHTTPStatus(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_delivery_transport_fail", now)
	require.NoError(t, err)

	subRepo := NewSubscriptionRepository(pool, testEncKey())
	subID := uuid.New()
	require.NoError(t, subRepo.Create(ctx, subID, accountID, "http://consumer.example/webhook",
		[]payment.EventType{payment.EventPaymentApproved}, "whsec_delivery_test", now))

	p := newTestPayment(t, accountID, payment.TokenCardApproved, now)
	require.NoError(t, NewPaymentRepository(pool).Insert(ctx, p))

	repo := NewDeliveryRepository(pool, &recordingIDGenerator{})
	deliveryID, err := repo.Create(ctx, payment.DeliveryDraft{
		EventID:               uuid.New(),
		EventType:             payment.EventPaymentApproved,
		SubscriptionID:        subID,
		PaymentID:             p.ID,
		ReferenceID:           p.ReferenceID,
		Status:                payment.StatusApproved,
		ProviderTransactionID: p.ProviderTransactionID,
		CreatedAt:             now,
	})
	require.NoError(t, err)

	require.NoError(t, repo.RecordAttempt(ctx, deliveryID, payment.DeliveryFailed, 0, now.Add(1*time.Second)))

	var rawStatus *int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT last_http_status FROM webhook_deliveries WHERE id = $1`, deliveryID,
	).Scan(&rawStatus))
	require.Nil(t, rawStatus, "last_http_status must be SQL NULL for a transport failure")

	got, err := repo.FindByID(ctx, deliveryID)
	require.NoError(t, err)
	require.Equal(t, 0, got.LastHTTPStatus)
}

func TestSubscriptionRepository_LoadDeliveryTarget_ReturnsURLAndDecryptedSecret(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_delivery_target", now)
	require.NoError(t, err)

	repo := NewSubscriptionRepository(pool, testEncKey())
	subID := uuid.New()
	require.NoError(t, repo.Create(ctx, subID, accountID, "http://consumer.example/target",
		[]payment.EventType{payment.EventPaymentApproved}, "whsec_target_secret", now))

	url, secret, err := repo.LoadDeliveryTarget(ctx, subID)
	require.NoError(t, err)
	require.Equal(t, "http://consumer.example/target", url)
	require.Equal(t, "whsec_target_secret", secret)
}
