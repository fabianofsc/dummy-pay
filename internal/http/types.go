package httpapi

import (
	"github.com/google/uuid"
)

// CreatePaymentRequest is the parsed request body for POST /v1/payments.
type CreatePaymentRequest struct {
	ReferenceID  string `json:"reference_id"`
	Amount       int64  `json:"amount"`
	Currency     string `json:"currency"`
	PaymentToken string `json:"payment_token"`
}

// CreatePaymentResponse is the response body for POST /v1/payments (spec §6.3).
type CreatePaymentResponse struct {
	PaymentID             string `json:"payment_id"`
	ProviderTransactionID string `json:"provider_transaction_id"`
	ReferenceID           string `json:"reference_id"`
	Amount                int64  `json:"amount"`
	Currency              string `json:"currency"`
	Status                string `json:"status"`
	CreatedAt             string `json:"created_at"`
}

// ErrorResponse is the standard error body format (spec §7).
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// encodeUUID adds a type prefix to a UUID for API representation.
// The domain uses bare UUIDs; this function adds the prefix only at the boundary.
func encodeUUID(id uuid.UUID, prefix string) string {
	return prefix + "_" + id.String()
}
