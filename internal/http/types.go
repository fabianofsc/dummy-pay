package httpapi

// CreatePaymentRequest is the parsed request body for POST /v1/payments.
type CreatePaymentRequest struct {
	ReferenceID  string `json:"reference_id"`
	Amount       int64  `json:"amount"`
	Currency     string `json:"currency"`
	PaymentToken string `json:"payment_token"`
}

// ErrorResponse is the standard error body format (spec §7).
type ErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
