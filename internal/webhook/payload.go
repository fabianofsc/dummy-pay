package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// EventPayload is the input to BuildPayload: already-encoded (prefixed)
// identifiers and canonical field values matching the README example
// exactly (spec §6). Callers supply prefixed strings — this package does not
// know about identifier prefixing, which is an HTTP-boundary concern
// (spec §1.3).
type EventPayload struct {
	EventID               string
	Type                  string
	CreatedAt             time.Time
	PaymentID             string
	ReferenceID           string
	Status                string
	ProviderTransactionID string
}

// eventPayloadJSON mirrors EventPayload with json tags in the exact field
// order the README example and spec §6 document. Field order matters here:
// this is the wire format a real consumer parses, and the fixture tests
// assert on the resulting bytes.
type eventPayloadJSON struct {
	EventID   string           `json:"event_id"`
	Type      string           `json:"type"`
	CreatedAt string           `json:"created_at"`
	Data      eventPayloadData `json:"data"`
}

type eventPayloadData struct {
	PaymentID             string `json:"payment_id"`
	ReferenceID           string `json:"reference_id"`
	Status                string `json:"status"`
	ProviderTransactionID string `json:"provider_transaction_id"`
}

// BuildPayload serialises p into the exact bytes to be stored, signed, and
// sent. It is called once at delivery creation; nothing downstream
// re-serialises (spec §3, spec §6).
func BuildPayload(p EventPayload) []byte {
	wire := eventPayloadJSON{
		EventID:   p.EventID,
		Type:      p.Type,
		CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339),
		Data: eventPayloadData{
			PaymentID:             p.PaymentID,
			ReferenceID:           p.ReferenceID,
			Status:                p.Status,
			ProviderTransactionID: p.ProviderTransactionID,
		},
	}
	// json.Marshal cannot fail on this concrete, non-cyclic struct of plain
	// strings and a nested struct of plain strings.
	body, _ := json.Marshal(wire)
	return body
}

// SignPayload returns the lowercase hex encoding of
// HMAC-SHA-256(secret, body) — the value sent as X-Webhook-Signature
// (spec §6).
func SignPayload(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
