package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"dummypay/internal/payment"
)

// CreateSubscriptionRequest is the parsed request body for
// POST /v1/webhook-subscriptions.
type CreateSubscriptionRequest struct {
	URL    string   `json:"url"`
	Events []string `json:"events"`
}

// CreateSubscriptionResponse is the response body for
// POST /v1/webhook-subscriptions (spec §4.2). Secret is present here and
// nowhere else — this is the one response that ever carries it.
type CreateSubscriptionResponse struct {
	SubscriptionID string   `json:"subscription_id"`
	URL            string   `json:"url"`
	Events         []string `json:"events"`
	Secret         string   `json:"secret"`
	CreatedAt      string   `json:"created_at"`
}

// decodeAndValidateSubscriptionRequest decodes the body with unknown-field
// rejection and validates url and events, writing the appropriate error
// response and returning ok=false on any failure (spec §4.2, spec §6).
func decodeAndValidateSubscriptionRequest(w http.ResponseWriter, r *http.Request) (req CreateSubscriptionRequest, events []payment.EventType, ok bool) {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return CreateSubscriptionRequest{}, nil, false
	}

	if err := payment.ValidateSubscriptionURL(req.URL); err != nil {
		respondError(w, http.StatusUnprocessableEntity, "invalid_request", err.Error())
		return CreateSubscriptionRequest{}, nil, false
	}

	events, err := payment.ValidateEventTypes(req.Events)
	if err != nil {
		respondError(w, http.StatusUnprocessableEntity, "invalid_request", err.Error())
		return CreateSubscriptionRequest{}, nil, false
	}

	return req, events, true
}

// makeHandleCreateSubscription returns a handler that uses the given use
// case and account id. Used for testing with injected fakes, and by the
// production router (Phase 11).
func makeHandleCreateSubscription(uc *payment.CreateSubscriptionUseCase, accountID uuid.UUID) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		req, events, ok := decodeAndValidateSubscriptionRequest(w, r)
		if !ok {
			return
		}

		result, err := uc.Execute(r.Context(), payment.CreateSubscriptionRequest{
			AccountID: accountID,
			URL:       req.URL,
			Events:    events,
		})
		switch {
		case err == nil:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(newCreateSubscriptionResponse(result))
		case errors.Is(err, payment.ErrSubscriptionExists):
			respondError(w, http.StatusConflict, "subscription_exists", err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "internal_error", err.Error())
		}
	}
}

// handleCreateSubscription decodes, validates, and creates a webhook
// subscription. Validation failures map to spec §7 error codes (spec §4.2,
// spec §6).
func handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if _, _, ok := decodeAndValidateSubscriptionRequest(w, r); !ok {
		return
	}

	respondError(w, http.StatusNotImplemented, "internal_error", "not implemented")
}

func newCreateSubscriptionResponse(result payment.CreateSubscriptionResult) CreateSubscriptionResponse {
	events := make([]string, len(result.Events))
	for i, e := range result.Events {
		events[i] = string(e)
	}
	return CreateSubscriptionResponse{
		SubscriptionID: encodeID(idKindSubscription, result.SubscriptionID),
		URL:            result.URL,
		Events:         events,
		Secret:         result.Secret,
		CreatedAt:      result.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}
}
