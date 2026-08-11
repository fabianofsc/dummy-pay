package payment

import (
	"crypto/sha256"
	"encoding/json"
)

// Fingerprint computes a stable SHA-256 over the four validated request
// fields in a fixed order, so a retry differing only in incidental framing
// (raw JSON key order, whitespace) fingerprints identically to the
// original, while any change to a field's actual value fingerprints
// differently (spec §4.1).
func Fingerprint(referenceID string, amount Amount, currency Currency, token ScenarioToken) [32]byte {
	canonical := struct {
		ReferenceID string `json:"reference_id"`
		Amount      int64  `json:"amount"`
		Currency    string `json:"currency"`
		Token       string `json:"payment_token"`
	}{referenceID, int64(amount), string(currency), string(token)}

	b, err := json.Marshal(canonical)
	if err != nil {
		panic("payment: fingerprint marshal of a plain struct cannot fail: " + err.Error())
	}
	return sha256.Sum256(b)
}
