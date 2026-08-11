package payment

import "slices"

// EventType is the type of a webhook event, driven entirely by the status a
// payment enters (spec §2).
type EventType string

const (
	EventPaymentApproved   EventType = "payment.approved"
	EventPaymentRejected   EventType = "payment.rejected"
	EventPaymentProcessing EventType = "payment.processing"
)

// EventTypeForStatus returns the event type produced when a payment enters s.
func EventTypeForStatus(s Status) EventType {
	switch s {
	case StatusApproved:
		return EventPaymentApproved
	case StatusRejected:
		return EventPaymentRejected
	case StatusProcessing:
		return EventPaymentProcessing
	default:
		panic("payment: no event type for status " + string(s))
	}
}

// ShouldEmitEvent reports whether eventType should be produced, given the
// event types an active subscription lists. A nil or empty subscribedEvents
// covers both "no active subscription" and "subscription doesn't cover this
// type" — spec §2 treats them identically: no event, nothing sent.
func ShouldEmitEvent(subscribedEvents []EventType, eventType EventType) bool {
	return slices.Contains(subscribedEvents, eventType)
}
