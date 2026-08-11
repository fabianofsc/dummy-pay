package webhook_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dummypay/internal/webhook"
)

// fixturePayload is the EventPayload used across Step 8.1's tests: fixed
// identifiers and a fixed clock so the serialised bytes are reproducible.
func fixturePayload() webhook.EventPayload {
	return webhook.EventPayload{
		EventID:               "evt_0199a1f4-4b17-70f2-a35d-8c1e64907bda",
		Type:                  "payment.approved",
		CreatedAt:             time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC),
		PaymentID:             "pay_0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b",
		ReferenceID:           "checkout:123",
		Status:                "APPROVED",
		ProviderTransactionID: "txn_0199a1f4-3c83-7a04-8f21-6d3b0e57c91a",
	}
}

// fixtureExpectedBody is the byte-for-byte expected serialisation of
// fixturePayload(), matching the README example's field order and values
// (spec §6).
const fixtureExpectedBody = `{"event_id":"evt_0199a1f4-4b17-70f2-a35d-8c1e64907bda","type":"payment.approved","created_at":"2026-08-10T12:00:00Z","data":{"payment_id":"pay_0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b","reference_id":"checkout:123","status":"APPROVED","provider_transaction_id":"txn_0199a1f4-3c83-7a04-8f21-6d3b0e57c91a"}}`

// TestBuildPayload_MatchesREADMEExampleByteForByte verifies the serialised
// payload for a fixed input and clock is byte-identical to the README
// example (spec §6).
func TestBuildPayload_MatchesREADMEExampleByteForByte(t *testing.T) {
	body := webhook.BuildPayload(fixturePayload())
	require.Equal(t, fixtureExpectedBody, string(body))
}

// TestSignPayload_MatchesIndependentlyComputedFixture verifies the HMAC
// against a value computed outside this codebase, so a bug shared between
// the implementation and the test cannot hide.
//
// Fixture derivation: with the fixed body above and secret
// "whsec_test_fixture_secret", computed via:
//
//	echo -n '<fixtureExpectedBody>' | openssl dgst -sha256 -hmac "whsec_test_fixture_secret" -hex
func TestSignPayload_MatchesIndependentlyComputedFixture(t *testing.T) {
	const secret = "whsec_test_fixture_secret"
	const wantSignature = "0566fb4833cf69d6dfb18c1dd8a1bcc8e3e4c5718b0caa765baca7189f0918b0"

	body := webhook.BuildPayload(fixturePayload())
	got := webhook.SignPayload(secret, body)

	require.Equal(t, wantSignature, got)
}

// TestSignPayload_OneByteChange_ChangesSignature verifies the signature is
// sensitive to every byte of the body — changing one byte must not produce
// the same signature (spec §6).
func TestSignPayload_OneByteChange_ChangesSignature(t *testing.T) {
	const secret = "whsec_test_fixture_secret"

	original := webhook.BuildPayload(fixturePayload())
	originalSig := webhook.SignPayload(secret, original)

	tampered := make([]byte, len(original))
	copy(tampered, original)
	// Flip the last character of the JSON (the closing brace) is invariant
	// across all events, so flip a character inside the reference_id value
	// instead — position doesn't matter, any single byte suffices.
	tampered[len(tampered)-2] = 'X'
	tamperedSig := webhook.SignPayload(secret, tampered)

	require.NotEqual(t, originalSig, tamperedSig)
}
