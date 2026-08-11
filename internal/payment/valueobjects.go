package payment

import "errors"

var (
	// ErrInvalidAmount is returned when an amount is not a positive integer
	// of cents.
	ErrInvalidAmount = errors.New("amount must be a positive integer of cents")
	// ErrUnsupportedCurrency is returned for any currency other than BRL.
	ErrUnsupportedCurrency = errors.New("unsupported currency")
	// ErrUnknownPaymentToken is returned for a token outside the four known
	// scenario tokens.
	ErrUnknownPaymentToken = errors.New("unknown payment token")
)

// Amount is a strictly positive number of cents. It can only be constructed
// through NewAmount, so an invalid amount cannot exist.
type Amount int64

// NewAmount validates cents and returns it as an Amount.
func NewAmount(cents int64) (Amount, error) {
	if cents <= 0 {
		return 0, ErrInvalidAmount
	}
	return Amount(cents), nil
}

// Currency is a supported settlement currency. V1 supports BRL only.
type Currency string

// CurrencyBRL is the only currency DummyPay accepts.
const CurrencyBRL Currency = "BRL"

// NewCurrency validates code and returns it as a Currency.
func NewCurrency(code string) (Currency, error) {
	if code != string(CurrencyBRL) {
		return "", ErrUnsupportedCurrency
	}
	return CurrencyBRL, nil
}

// ScenarioToken selects a payment's outcome deterministically (ADR-0002).
type ScenarioToken string

const (
	TokenCardApproved           ScenarioToken = "card_approved"
	TokenCardDeclined           ScenarioToken = "card_declined"
	TokenCardProcessingApproved ScenarioToken = "card_processing_approved"
	TokenCardProcessingDeclined ScenarioToken = "card_processing_declined"
)

// NewScenarioToken validates s against the closed set of scenario tokens.
func NewScenarioToken(s string) (ScenarioToken, error) {
	switch ScenarioToken(s) {
	case TokenCardApproved, TokenCardDeclined, TokenCardProcessingApproved, TokenCardProcessingDeclined:
		return ScenarioToken(s), nil
	default:
		return "", ErrUnknownPaymentToken
	}
}
