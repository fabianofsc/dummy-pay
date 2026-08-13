package payment

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func newTestPayment(t *testing.T, token ScenarioToken, now time.Time) Payment {
	t.Helper()
	amount, err := NewAmount(10990)
	require.NoError(t, err)

	return NewPayment(uuid.New(), uuid.New(), uuid.New(), "checkout:123", amount, CurrencyBRL, token, now)
}

func TestNewPayment_CreationStatus(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		token      ScenarioToken
		wantStatus Status
	}{
		{TokenCardApproved, StatusProcessing},
		{TokenCardDeclined, StatusProcessing},
		{TokenCardProcessingApproved, StatusProcessing},
		{TokenCardProcessingDeclined, StatusProcessing},
	}

	for _, tt := range tests {
		t.Run(string(tt.token), func(t *testing.T) {
			p := newTestPayment(t, tt.token, now)

			require.Equal(t, tt.wantStatus, p.Status)
			require.True(t, now.Equal(p.CreatedAt))
			require.True(t, now.Equal(p.UpdatedAt))
		})
	}
}

func TestPayment_Settle_BecomesTokenTerminalOutcome(t *testing.T) {
	createdAt := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	settledAt := createdAt.Add(3 * time.Second)

	tests := []struct {
		token      ScenarioToken
		wantStatus Status
	}{
		{TokenCardApproved, StatusApproved},
		{TokenCardDeclined, StatusRejected},
		{TokenCardProcessingApproved, StatusApproved},
		{TokenCardProcessingDeclined, StatusRejected},
	}

	for _, tt := range tests {
		t.Run(string(tt.token), func(t *testing.T) {
			p := newTestPayment(t, tt.token, createdAt)

			p.Settle(settledAt)

			require.Equal(t, tt.wantStatus, p.Status)
			require.True(t, settledAt.Equal(p.UpdatedAt))
		})
	}
}

// Settling a payment already in a terminal state is a no-op, so duplicate
// settlement work cannot change it or refresh UpdatedAt.
func TestPayment_Settle_AlreadyTerminal_IsNoOp(t *testing.T) {
	createdAt := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	laterAt := createdAt.Add(3 * time.Second)

	tests := []struct {
		name   string
		token  ScenarioToken
		status Status
	}{
		{"approved", TokenCardApproved, StatusApproved},
		{"rejected", TokenCardDeclined, StatusRejected},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := newTestPayment(t, tt.token, createdAt)
			before.Status = tt.status
			after := before

			after.Settle(laterAt)

			if diff := cmp.Diff(before, after); diff != "" {
				t.Errorf("Settle on a terminal payment changed it (-before +after):\n%s", diff)
			}
		})
	}

	t.Run("already settled once", func(t *testing.T) {
		p := newTestPayment(t, TokenCardProcessingApproved, createdAt)
		p.Settle(laterAt)
		settledOnce := p

		p.Settle(laterAt.Add(3 * time.Second))

		if diff := cmp.Diff(settledOnce, p); diff != "" {
			t.Errorf("second Settle changed an already-settled payment (-before +after):\n%s", diff)
		}
	})
}
