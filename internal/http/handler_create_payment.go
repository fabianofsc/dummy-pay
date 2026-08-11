package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"dummypay/internal/payment"
)

// makeHandleCreatePayment returns a handler that uses the given use case.
// Used for testing with injected fakes.
func makeHandleCreatePayment(uc *payment.CreatePaymentUseCase, auth AuthConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Validate Idempotency-Key header first (spec §6.2).
		idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if idempotencyKey == "" {
			respondError(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key required and non-empty")
			return
		}

		// Decode request body with unknown field rejection (spec §6.2, ADR-0002).
		var req CreatePaymentRequest
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "invalid_request", err.Error())
			return
		}

		// Validate amount (spec §6.2, spec §7).
		if req.Amount <= 0 {
			respondError(w, http.StatusUnprocessableEntity, "invalid_amount", "amount must be a positive integer of cents")
			return
		}

		// Validate currency (spec §6.2, spec §7).
		if req.Currency != "BRL" {
			respondError(w, http.StatusUnprocessableEntity, "unsupported_currency", "currency must be BRL")
			return
		}

		// Validate token (spec §6.2, spec §7).
		_, err := payment.NewScenarioToken(req.PaymentToken)
		if err != nil {
			respondError(w, http.StatusUnprocessableEntity, "unknown_payment_token", err.Error())
			return
		}

		// Create the payment via use case.
		amount, _ := payment.NewAmount(req.Amount)
		currency, _ := payment.NewCurrency(req.Currency)
		token, _ := payment.NewScenarioToken(req.PaymentToken)

		// Placeholder account ID — would come from auth context in production.
		// For now, use nil UUID. Will be replaced in real implementation.
		accountID := uuid.Nil

		ucReq := payment.CreatePaymentRequest{
			AccountID:      accountID,
			IdempotencyKey: idempotencyKey,
			ReferenceID:    req.ReferenceID,
			Amount:         amount,
			Currency:       currency,
			Token:          token,
		}

		// Execute use case.
		p, err := uc.Execute(r.Context(), ucReq, func(p payment.Payment) (int, []byte) {
			// Response encoder: returns status and serialized body.
			resp := CreatePaymentResponse{
				PaymentID:             encodeUUID(p.ID, "pay"),
				ProviderTransactionID: encodeUUID(p.ProviderTransactionID, "txn"),
				ReferenceID:           p.ReferenceID,
				Amount:                int64(p.Amount),
				Currency:              "BRL",
				Status:                string(p.Status),
				CreatedAt:             p.CreatedAt.Format("2006-01-02T15:04:05Z"),
			}
			body, _ := json.Marshal(resp)
			return http.StatusCreated, body
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "internal_error", err.Error())
			return
		}

		// Write response.
		_ = json.NewEncoder(w).Encode(p)
	}
}

// handleCreatePayment decodes, validates, and creates a payment.
// Validation failures map to spec §7 error codes and HTTP statuses (spec §6.2).
func handleCreatePayment(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Validate Idempotency-Key header first (spec §6.2).
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey == "" {
		respondError(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key required and non-empty")
		return
	}

	// Decode request body with unknown field rejection (spec §6.2, ADR-0002).
	var req CreatePaymentRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	// Validate amount (spec §6.2, spec §7).
	if req.Amount <= 0 {
		respondError(w, http.StatusUnprocessableEntity, "invalid_amount", "amount must be a positive integer of cents")
		return
	}

	// Validate currency (spec §6.2, spec §7).
	if req.Currency != "BRL" {
		respondError(w, http.StatusUnprocessableEntity, "unsupported_currency", "currency must be BRL")
		return
	}

	// Validate token (spec §6.2, spec §7).
	_, err := payment.NewScenarioToken(req.PaymentToken)
	if err != nil {
		respondError(w, http.StatusUnprocessableEntity, "unknown_payment_token", err.Error())
		return
	}

	// Placeholder: for now, return 501 to indicate the use case is not wired.
	http.Error(w, "Not Implemented", http.StatusNotImplemented)
}

// respondError writes a standard error response.
func respondError(w http.ResponseWriter, status int, code, message string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Code:    code,
		Message: message,
	})
}
