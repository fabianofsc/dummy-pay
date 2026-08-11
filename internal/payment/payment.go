package payment

import (
	"time"

	"github.com/google/uuid"
)

// Status is a payment's position in the state machine (spec §2). PROCESSING
// is the only non-terminal status; the only transition out of it is Settle.
type Status string

const (
	StatusApproved   Status = "APPROVED"
	StatusRejected   Status = "REJECTED"
	StatusProcessing Status = "PROCESSING"
)

// IsTerminal reports whether s is a final status a payment never leaves.
func (s Status) IsTerminal() bool {
	return s == StatusApproved || s == StatusRejected
}

// Payment is a single sale. Its Status is driven entirely by Token: the
// status it is created with and, for the two processing tokens, the status
// it settles to (spec §2).
type Payment struct {
	ID                    uuid.UUID
	AccountID             uuid.UUID
	ReferenceID           string
	Amount                Amount
	Currency              Currency
	Token                 ScenarioToken
	Status                Status
	ProviderTransactionID uuid.UUID
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// NewPayment constructs a Payment with the creation status token dictates.
func NewPayment(id, accountID, providerTransactionID uuid.UUID, referenceID string, amount Amount, currency Currency, token ScenarioToken, now time.Time) Payment {
	return Payment{
		ID:                    id,
		AccountID:             accountID,
		ReferenceID:           referenceID,
		Amount:                amount,
		Currency:              currency,
		Token:                 token,
		Status:                creationStatus(token),
		ProviderTransactionID: providerTransactionID,
		CreatedAt:             now,
		UpdatedAt:             now,
	}
}

func creationStatus(token ScenarioToken) Status {
	switch token {
	case TokenCardApproved:
		return StatusApproved
	case TokenCardDeclined:
		return StatusRejected
	case TokenCardProcessingApproved, TokenCardProcessingDeclined:
		return StatusProcessing
	default:
		panic("payment: unknown scenario token " + string(token))
	}
}

// Settle transitions a PROCESSING payment to the terminal status its token
// dictates. Settling a payment that is no longer PROCESSING — because it
// never was, or because it was already settled — is a no-op rather than an
// error, so the worker can safely process the same work item twice.
func (p *Payment) Settle(now time.Time) {
	if p.Status != StatusProcessing {
		return
	}

	switch p.Token {
	case TokenCardProcessingApproved:
		p.Status = StatusApproved
	case TokenCardProcessingDeclined:
		p.Status = StatusRejected
	default:
		panic("payment: settle called on payment with non-processing token " + string(p.Token))
	}
	p.UpdatedAt = now
}
