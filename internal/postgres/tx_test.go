package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/payment"
)

// TestTxManager_Within_CommitsWhenFnSucceeds is the counterweight to the
// rollback tests: without it, a TxManager that rolled everything back
// unconditionally — or never committed at all — would pass them.
func TestTxManager_Within_CommitsWhenFnSucceeds(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_tx_commit", now)
	require.NoError(t, err)

	repo := NewPaymentRepository(pool)
	p := newTestPayment(t, accountID, payment.TokenCardApproved, now)

	err = NewTxManager(pool).Within(ctx, func(ctx context.Context) error {
		return repo.Insert(ctx, p)
	})
	require.NoError(t, err)

	got, err := repo.FindByID(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, p.ID, got.ID)
}

// TestTxManager_Within_RollsBackWhenFnFails covers the same rollback the
// create-payment atomicity test covers, but at the level of TxManager itself,
// with no use case in the way — so a failure here says the transaction is
// wrong rather than the flow.
func TestTxManager_Within_RollsBackWhenFnFails(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_tx_rollback", now)
	require.NoError(t, err)

	repo := NewPaymentRepository(pool)
	p := newTestPayment(t, accountID, payment.TokenCardApproved, now)

	err = NewTxManager(pool).Within(ctx, func(ctx context.Context) error {
		if err := repo.Insert(ctx, p); err != nil {
			return err
		}
		// The row is visible to this transaction before it is undone.
		if _, err := repo.FindByID(ctx, p.ID); err != nil {
			return err
		}
		return errOutboxUnavailable
	})
	require.ErrorIs(t, err, errOutboxUnavailable)

	_, err = repo.FindByID(ctx, p.ID)
	require.ErrorIs(t, err, payment.ErrPaymentNotFound)
}
