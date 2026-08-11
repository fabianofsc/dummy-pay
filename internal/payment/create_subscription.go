package payment

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// CreateSubscriptionRequest is the validated input to
// CreateSubscriptionUseCase — url and events have already passed
// ValidateSubscriptionURL/ValidateEventTypes at the HTTP boundary (spec §4.2).
type CreateSubscriptionRequest struct {
	AccountID uuid.UUID
	URL       string
	Events    []EventType
}

// CreateSubscriptionResult is what Execute returns on success. Secret is the
// plaintext, present here and only here — the caller must return it in the
// response and never again (spec §4.2, ADR-0009).
type CreateSubscriptionResult struct {
	SubscriptionID uuid.UUID
	URL            string
	Events         []EventType
	Secret         string
	CreatedAt      time.Time
}

// CreateSubscriptionDeps are the ports CreateSubscriptionUseCase needs
// (spec §4.2, spec §8).
type CreateSubscriptionDeps struct {
	Subscriptions SubscriptionCreator
	Secrets       SecretGenerator
	IDs           IDGenerator
	Clock         Clock
}

// CreateSubscriptionUseCase orchestrates subscription creation (spec §4.2).
type CreateSubscriptionUseCase struct {
	deps CreateSubscriptionDeps
}

// NewCreateSubscriptionUseCase constructs a CreateSubscriptionUseCase
// against deps.
func NewCreateSubscriptionUseCase(deps CreateSubscriptionDeps) *CreateSubscriptionUseCase {
	return &CreateSubscriptionUseCase{deps: deps}
}

// Execute generates a secret, persists the subscription, and returns the
// plaintext secret alongside the stored fields. A second active subscription
// for the account surfaces ErrSubscriptionExists unchanged, straight from
// the repository — the database's partial unique index is what actually
// enforces the rule (spec §4.2, ADR-0009).
func (uc *CreateSubscriptionUseCase) Execute(ctx context.Context, req CreateSubscriptionRequest) (CreateSubscriptionResult, error) {
	secret, err := uc.deps.Secrets.NewSecret()
	if err != nil {
		return CreateSubscriptionResult{}, fmt.Errorf("generate secret: %w", err)
	}

	now := uc.deps.Clock.Now()
	id := uc.deps.IDs.NewID()

	if err := uc.deps.Subscriptions.Create(ctx, id, req.AccountID, req.URL, req.Events, secret, now); err != nil {
		return CreateSubscriptionResult{}, err
	}

	return CreateSubscriptionResult{
		SubscriptionID: id,
		URL:            req.URL,
		Events:         req.Events,
		Secret:         secret,
		CreatedAt:      now,
	}, nil
}
