package payment_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"dummypay/internal/payment"
)

func TestValidateSubscriptionURL_AbsoluteHTTPOrHTTPS_Accepted(t *testing.T) {
	tests := []string{
		"http://consumer:8080/internal/provider-events",
		"https://example.com/webhook",
	}
	for _, url := range tests {
		t.Run(url, func(t *testing.T) {
			require.NoError(t, payment.ValidateSubscriptionURL(url))
		})
	}
}

func TestValidateSubscriptionURL_Invalid_Rejected(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"relative path", "/webhook"},
		{"no scheme", "consumer:8080/webhook"},
		{"ftp scheme", "ftp://example.com/webhook"},
		{"malformed", "http://[::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.ErrorIs(t, payment.ValidateSubscriptionURL(tt.url), payment.ErrInvalidSubscriptionURL)
		})
	}
}

func TestValidateEventTypes_KnownTypes_Accepted(t *testing.T) {
	events, err := payment.ValidateEventTypes([]string{
		"payment.approved", "payment.rejected", "payment.processing",
	})
	require.NoError(t, err)
	require.Equal(t, []payment.EventType{
		payment.EventPaymentApproved,
		payment.EventPaymentRejected,
		payment.EventPaymentProcessing,
	}, events)
}

func TestValidateEventTypes_Empty_Rejected(t *testing.T) {
	_, err := payment.ValidateEventTypes([]string{})
	require.ErrorIs(t, err, payment.ErrEmptySubscriptionEvents)
}

func TestValidateEventTypes_UnknownType_Rejected(t *testing.T) {
	_, err := payment.ValidateEventTypes([]string{"payment.approved", "payment.bogus"})
	require.ErrorIs(t, err, payment.ErrUnknownEventType)
}
