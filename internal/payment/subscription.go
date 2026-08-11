package payment

import (
	"errors"
	"net/url"
)

var (
	// ErrInvalidSubscriptionURL is returned when a subscription URL does not
	// parse as an absolute http or https URL (spec §4.2).
	ErrInvalidSubscriptionURL = errors.New("url must be an absolute http or https URL")
	// ErrEmptySubscriptionEvents is returned when a subscription lists no
	// event types.
	ErrEmptySubscriptionEvents = errors.New("events must not be empty")
	// ErrUnknownEventType is returned when a subscription lists an event type
	// outside the three known types.
	ErrUnknownEventType = errors.New("unknown event type")
)

// ValidateSubscriptionURL validates that raw is an absolute http or https URL
// (spec §4.2).
func ValidateSubscriptionURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") {
		return ErrInvalidSubscriptionURL
	}
	return nil
}

// ValidateEventTypes validates that events is non-empty and every entry is a
// known EventType, returning the parsed slice (spec §4.2).
func ValidateEventTypes(events []string) ([]EventType, error) {
	if len(events) == 0 {
		return nil, ErrEmptySubscriptionEvents
	}
	result := make([]EventType, 0, len(events))
	for _, e := range events {
		switch EventType(e) {
		case EventPaymentApproved, EventPaymentRejected, EventPaymentProcessing:
			result = append(result, EventType(e))
		default:
			return nil, ErrUnknownEventType
		}
	}
	return result, nil
}
