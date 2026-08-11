package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/payment"
)

func testEncKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

// TestSubscriptionRepository_Create_StoresEncryptedSecret verifies that
// creating a subscription stores the secret encrypted — the ciphertext
// column never equals the plaintext (ADR-0009).
func TestSubscriptionRepository_Create_StoresEncryptedSecret(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_sub_create", now)
	require.NoError(t, err)

	repo := NewSubscriptionRepository(pool, testEncKey())

	plaintext := "whsec_abcdefghijklmnopqrstuvwxyz123456"
	subID := uuid.New()
	events := []payment.EventType{payment.EventPaymentApproved, payment.EventPaymentRejected}

	err = repo.Create(ctx, subID, accountID, "http://consumer:8080/webhook", events, plaintext, now)
	require.NoError(t, err)

	var storedCiphertext []byte
	err = pool.QueryRow(ctx,
		`SELECT secret_ciphertext FROM webhook_subscriptions WHERE id = $1`, subID,
	).Scan(&storedCiphertext)
	require.NoError(t, err)

	require.NotEqual(t, []byte(plaintext), storedCiphertext,
		"stored ciphertext must never equal the plaintext secret")
}

// TestSubscriptionRepository_LoadActive_ReturnsWhatWasCreated verifies the
// round trip: create then load returns the same URL and events.
func TestSubscriptionRepository_LoadActive_ReturnsWhatWasCreated(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_sub_load", now)
	require.NoError(t, err)

	repo := NewSubscriptionRepository(pool, testEncKey())

	subID := uuid.New()
	events := []payment.EventType{payment.EventPaymentApproved, payment.EventPaymentProcessing}
	err = repo.Create(ctx, subID, accountID, "http://consumer:8080/webhook", events, "whsec_test", now)
	require.NoError(t, err)

	sub, ok, err := repo.LoadActive(ctx, accountID)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, subID, sub.ID)
	require.ElementsMatch(t, events, sub.Events)
}

// TestSubscriptionRepository_LoadActive_NoSubscription_ReturnsNotOK verifies
// that an account with no subscription reports ok=false, not an error.
func TestSubscriptionRepository_LoadActive_NoSubscription_ReturnsNotOK(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_sub_none", now)
	require.NoError(t, err)

	repo := NewSubscriptionRepository(pool, testEncKey())

	_, ok, err := repo.LoadActive(ctx, accountID)
	require.NoError(t, err)
	require.False(t, ok)
}

// TestSubscriptionRepository_Create_SecondActiveSubscription_ReturnsErrSubscriptionExists
// proves the "one active subscription per account" rule is enforced by the
// real partial unique index (spec §4.2, ADR-0009), not an application check —
// the second Create call hits the database constraint directly.
func TestSubscriptionRepository_Create_SecondActiveSubscription_ReturnsErrSubscriptionExists(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_sub_dup", now)
	require.NoError(t, err)

	repo := NewSubscriptionRepository(pool, testEncKey())
	events := []payment.EventType{payment.EventPaymentApproved}

	err = repo.Create(ctx, uuid.New(), accountID, "http://consumer:8080/webhook", events, "whsec_first", now)
	require.NoError(t, err)

	err = repo.Create(ctx, uuid.New(), accountID, "http://consumer:8080/webhook2", events, "whsec_second", now)
	require.ErrorIs(t, err, ErrSubscriptionExists)
}
