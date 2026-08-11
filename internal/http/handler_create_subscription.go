package httpapi

import (
	"encoding/json"
	"net/http"

	"dummypay/internal/payment"
)

// CreateSubscriptionRequest is the parsed request body for
// POST /v1/webhook-subscriptions.
type CreateSubscriptionRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

// handleCreateSubscription decodes, validates, and creates a webhook
// subscription. Validation failures map to spec §7 error codes (spec §4.2,
// spec §6).
func handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	var req CreateSubscriptionRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := payment.ValidateSubscriptionURL(req.URL); err != nil {
		respondError(w, http.StatusUnprocessableEntity, "invalid_request", err.Error())
		return
	}

	if _, err := payment.ValidateEventTypes(req.Events); err != nil {
		respondError(w, http.StatusUnprocessableEntity, "invalid_request", err.Error())
		return
	}

	// Placeholder: wiring to the repository and secret generation happens
	// once the use case is assembled (Phase 11).
	http.Error(w, "Not Implemented", http.StatusNotImplemented)
}
