package httpapi_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	httpapi "dummypay/internal/http"
)

// TestCreatePaymentResponse_HasCorrectShape verifies the response body format
// matches the spec §6.3 and README example: prefixed identifiers, all required
// fields, correct types.
func TestCreatePaymentResponse_HasCorrectShape(t *testing.T) {
	// Simulate a successful payment response.
	resp := httpapi.CreatePaymentResponse{
		PaymentID:             "pay_0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b",
		ProviderTransactionID: "txn_0199a1f4-3c83-7a04-8f21-6d3b0e57c91a",
		ReferenceID:           "checkout:123",
		Amount:                10990,
		Currency:              "BRL",
		Status:                "PROCESSING",
		CreatedAt:             "2026-08-10T12:00:00Z",
	}

	// Marshal and validate shape.
	body, _ := json.Marshal(resp)

	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &decoded))

	// Verify all required fields are present.
	requiredFields := []string{
		"payment_id", "provider_transaction_id", "reference_id",
		"amount", "currency", "status", "created_at",
	}
	for _, field := range requiredFields {
		require.Contains(t, decoded, field, "response must have field %s", field)
	}

	// Verify prefixes.
	paymentID := decoded["payment_id"].(string)
	require.True(t, len(paymentID) > 4 && paymentID[:4] == "pay_",
		"payment_id must have pay_ prefix")

	txnID := decoded["provider_transaction_id"].(string)
	require.True(t, len(txnID) > 4 && txnID[:4] == "txn_",
		"provider_transaction_id must have txn_ prefix")

	// Verify types.
	require.Equal(t, "checkout:123", decoded["reference_id"].(string))
	require.Equal(t, 10990.0, decoded["amount"].(float64))
	require.Equal(t, "BRL", decoded["currency"].(string))
	require.Equal(t, "PROCESSING", decoded["status"].(string))
	require.Equal(t, "2026-08-10T12:00:00Z", decoded["created_at"].(string))
}
