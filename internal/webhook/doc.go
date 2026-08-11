// Package webhook builds event payloads, signs them with HMAC-SHA-256, and
// sends them to a subscribed URL. Construction and signing are kept with the
// outbound client because the signature is computed over the exact bytes
// sent, and splitting the two invites disagreement about what "the body" is.
// See docs/spec-v1.md §6.
package webhook
