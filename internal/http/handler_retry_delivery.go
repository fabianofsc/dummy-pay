package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"dummypay/internal/payment"
)

// makeHandleRetryDeliveryForAccount returns a handler that uses the given
// use case and account id. Used for testing with injected fakes, and by the
// production router (Phase 11).
func makeHandleRetryDeliveryForAccount(uc *payment.RetryDeliveryUseCase, accountID uuid.UUID) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		deliveryID, ok := decodeRetryDeliveryID(w, r)
		if !ok {
			return
		}

		d, err := uc.Execute(r.Context(), accountID, deliveryID)
		switch {
		case err == nil:
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(newDeliveryStateResponse(deliveryID, d))
		case errors.Is(err, payment.ErrDeliveryNotFound):
			respondError(w, http.StatusNotFound, "not_found", "delivery not found")
		case errors.Is(err, payment.ErrDeliveryNotRetryable):
			respondError(w, http.StatusConflict, "delivery_not_retryable", err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
	}
}

// handleRetryDelivery decodes and validates the delivery id.
// Validation failures map to spec §7 error codes (spec §4.3).
func handleRetryDelivery(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if _, ok := decodeRetryDeliveryID(w, r); !ok {
		return
	}

	// Placeholder: for now, return 501 to indicate the use case is not wired.
	http.Error(w, "Not Implemented", http.StatusNotImplemented)
}

// decodeRetryDeliveryID parses and verifies the dlv_ prefix of the
// delivery_id route parameter. A well-formed UUID with the wrong prefix, or
// a malformed UUID, is a 404 not_found — never a lookup (spec §4.3,
// ADR-0006). ok is false when a response has already been written.
func decodeRetryDeliveryID(w http.ResponseWriter, r *http.Request) (deliveryID uuid.UUID, ok bool) {
	raw := chi.URLParam(r, "delivery_id")
	id, err := decodeID(idKindDelivery, raw)
	if err != nil {
		respondError(w, http.StatusNotFound, "not_found", "delivery not found")
		return uuid.UUID{}, false
	}
	return id, true
}

// deliveryStateResponse is the response body for a successful retry: the
// delivery's updated state (spec §4.3).
type deliveryStateResponse struct {
	DeliveryID      string `json:"delivery_id"`
	Status          string `json:"status"`
	AttemptCount    int    `json:"attempt_count"`
	LastAttemptedAt string `json:"last_attempted_at,omitempty"`
	LastHTTPStatus  *int   `json:"last_http_status,omitempty"`
}

func newDeliveryStateResponse(id uuid.UUID, d payment.Delivery) deliveryStateResponse {
	resp := deliveryStateResponse{
		DeliveryID:   encodeID(idKindDelivery, id),
		Status:       string(d.Status),
		AttemptCount: d.AttemptCount,
	}
	if !d.LastAttemptedAt.IsZero() {
		resp.LastAttemptedAt = d.LastAttemptedAt.Format("2006-01-02T15:04:05Z")
	}
	if d.LastHTTPStatus != 0 {
		s := d.LastHTTPStatus
		resp.LastHTTPStatus = &s
	}
	return resp
}
