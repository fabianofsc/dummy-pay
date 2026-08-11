package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"dummypay/internal/payment"
)

func newTestPayment(t *testing.T, accountID uuid.UUID, token payment.ScenarioToken, now time.Time) payment.Payment {
	t.Helper()
	amount, err := payment.NewAmount(10990)
	require.NoError(t, err)

	return payment.NewPayment(uuid.New(), accountID, uuid.New(), "checkout:123", amount, payment.CurrencyBRL, token, now)
}

func TestPaymentRepository_InsertThenFindByID_ReturnsEqualPayment(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account", now)
	require.NoError(t, err)

	repo := NewPaymentRepository(pool)
	p := newTestPayment(t, accountID, payment.TokenCardApproved, now)

	err = repo.Insert(ctx, p)
	require.NoError(t, err)

	got, err := repo.FindByID(ctx, p.ID)
	require.NoError(t, err)

	if diff := cmp.Diff(p, got); diff != "" {
		t.Errorf("FindByID after Insert returned a different payment (-want +got):\n%s", diff)
	}
}

func TestPaymentRepository_FindByID_UnknownID_ReturnsErrPaymentNotFound(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()

	repo := NewPaymentRepository(pool)

	_, err := repo.FindByID(ctx, uuid.New())
	require.ErrorIs(t, err, payment.ErrPaymentNotFound)
}

func TestPaymentRepository_Update_PersistsStatusTransitionAndUpdatedAt(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	createdAt := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	settledAt := createdAt.Add(3 * time.Second)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_update", createdAt)
	require.NoError(t, err)

	repo := NewPaymentRepository(pool)
	p := newTestPayment(t, accountID, payment.TokenCardProcessingApproved, createdAt)

	require.NoError(t, repo.Insert(ctx, p))
	require.Equal(t, payment.StatusProcessing, p.Status)

	p.Settle(settledAt)
	require.Equal(t, payment.StatusApproved, p.Status)

	require.NoError(t, repo.Update(ctx, p))

	got, err := repo.FindByID(ctx, p.ID)
	require.NoError(t, err)

	if diff := cmp.Diff(p, got); diff != "" {
		t.Errorf("FindByID after Update returned a different payment (-want +got):\n%s", diff)
	}
	require.Equal(t, payment.StatusApproved, got.Status)
	require.True(t, settledAt.Equal(got.UpdatedAt))
}

// TestPayments_CheckConstraints_RejectInvalidRows proves the CHECK
// constraints on amount_cents and currency reject invalid rows even when
// the domain layer is bypassed entirely — a raw SQL INSERT against the pool,
// not through PaymentRepository.Insert (which only ever receives an
// already-valid domain Payment and so could never exercise this path).
func TestPayments_CheckConstraints_RejectInvalidRows(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "test_account_checks", now)
	require.NoError(t, err)

	insert := `INSERT INTO payments
		(id, account_id, reference_id, amount_cents, currency, payment_token,
		 status, provider_transaction_id, created_at, updated_at)
		VALUES ($1, $2, 'checkout:123', $3, $4, 'card_approved', 'APPROVED', $5, $6, $6)`

	tests := []struct {
		name     string
		amount   int64
		currency string
	}{
		{"zero amount", 0, "BRL"},
		{"negative amount", -100, "BRL"},
		{"unsupported currency", 10990, "USD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, insert,
				uuid.New(), accountID, tt.amount, tt.currency, uuid.New(), now)
			require.Error(t, err)

			var pgErr *pgconn.PgError
			require.True(t, errors.As(err, &pgErr), "expected a *pgconn.PgError, got %T: %v", err, err)
			require.Equal(t, "23514", pgErr.Code, "expected check_violation (23514), got %s: %s", pgErr.Code, pgErr.Message)
		})
	}
}
