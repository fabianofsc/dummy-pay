package payment

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAmount_PositiveCents_Accepted(t *testing.T) {
	got, err := NewAmount(10990)

	require.NoError(t, err)
	require.Equal(t, Amount(10990), got)
}

func TestNewAmount_Zero_Rejected(t *testing.T) {
	_, err := NewAmount(0)

	require.ErrorIs(t, err, ErrInvalidAmount)
}

func TestNewAmount_Negative_Rejected(t *testing.T) {
	_, err := NewAmount(-1)

	require.ErrorIs(t, err, ErrInvalidAmount)
}

func TestNewCurrency_BRL_Accepted(t *testing.T) {
	got, err := NewCurrency("BRL")

	require.NoError(t, err)
	require.Equal(t, CurrencyBRL, got)
}

func TestNewCurrency_USD_Rejected(t *testing.T) {
	_, err := NewCurrency("USD")

	require.ErrorIs(t, err, ErrUnsupportedCurrency)
}

func TestNewScenarioToken_KnownTokens_Accepted(t *testing.T) {
	tokens := []string{
		"card_approved",
		"card_declined",
		"card_processing_approved",
		"card_processing_declined",
	}

	for _, s := range tokens {
		t.Run(s, func(t *testing.T) {
			got, err := NewScenarioToken(s)

			require.NoError(t, err)
			require.Equal(t, ScenarioToken(s), got)
		})
	}
}

func TestNewScenarioToken_Unknown_Rejected(t *testing.T) {
	_, err := NewScenarioToken("card_maybe")

	require.ErrorIs(t, err, ErrUnknownPaymentToken)
}
