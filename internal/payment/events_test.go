package payment

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestEventTypeForStatus_MapsEachStatus(t *testing.T) {
	tests := []struct {
		status Status
		want   EventType
	}{
		{StatusApproved, EventPaymentApproved},
		{StatusRejected, EventPaymentRejected},
		{StatusProcessing, EventPaymentProcessing},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			require.Equal(t, tt.want, EventTypeForStatus(tt.status))
		})
	}
}

func TestShouldEmitEvent_SubscriptionListsType_ReturnsTrue(t *testing.T) {
	subscribed := []EventType{EventPaymentApproved, EventPaymentProcessing}

	require.True(t, ShouldEmitEvent(subscribed, EventPaymentApproved))
}

func TestShouldEmitEvent_SubscriptionDoesNotListType_ReturnsFalse(t *testing.T) {
	subscribed := []EventType{EventPaymentProcessing}

	require.False(t, ShouldEmitEvent(subscribed, EventPaymentApproved))
}

func TestShouldEmitEvent_NoSubscription_ReturnsFalse(t *testing.T) {
	require.False(t, ShouldEmitEvent(nil, EventPaymentApproved))
}

// A card_processing_* payment moves PROCESSING -> terminal, and each status
// it ever holds should produce exactly one event when a subscription covers
// every event type: payment.processing at creation, then the terminal event
// at settlement (spec §2).
func TestPaymentLifecycle_ProcessingToken_ProducesExactlyTwoEvents(t *testing.T) {
	createdAt := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	settledAt := createdAt.Add(3 * time.Second)
	subscribed := []EventType{EventPaymentApproved, EventPaymentRejected, EventPaymentProcessing}

	amount, err := NewAmount(10990)
	require.NoError(t, err)
	p := NewPayment(uuid.New(), uuid.New(), uuid.New(), "checkout:123", amount, CurrencyBRL, TokenCardProcessingApproved, createdAt)

	var events []EventType
	if ShouldEmitEvent(subscribed, EventTypeForStatus(p.Status)) {
		events = append(events, EventTypeForStatus(p.Status))
	}

	p.Settle(settledAt)
	if ShouldEmitEvent(subscribed, EventTypeForStatus(p.Status)) {
		events = append(events, EventTypeForStatus(p.Status))
	}

	require.Equal(t, []EventType{EventPaymentProcessing, EventPaymentApproved}, events)
}
