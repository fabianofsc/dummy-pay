package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"dummypay/internal/payment"
)

// makeHandleCreatePayment returns a handler that uses the given use case and
// account id. Used for testing with injected fakes, and by the production
// router (Phase 11).
func makeHandleCreatePayment(uc *payment.CreatePaymentUseCase, accountID uuid.UUID) http.HandlerFunc {
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

		ucReq := payment.CreatePaymentRequest{
			AccountID:      accountID,
			IdempotencyKey: idempotencyKey,
			ReferenceID:    req.ReferenceID,
			Amount:         amount,
			Currency:       currency,
			Token:          token,
		}

		// Execute use case.
		result, err := uc.Execute(r.Context(), ucReq, func(p payment.Payment) (int, []byte) {
			// Response encoder: returns status and serialized body. Called
			// once, inside the use case's transaction, and the exact bytes
			// returned here are what a replay gets back verbatim — never
			// re-encoded (spec §4.1, spec §6.3).
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

		// Write exactly the bytes the encoder produced — a fresh creation and
		// a replay both go through this same path, so both return
		// byte-identical bodies for the same idempotency key (spec §6.3).
		w.WriteHeader(result.ResponseStatus)
		_, _ = w.Write(result.ResponseBody)
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
