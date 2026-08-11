package webhook

import (
	"bytes"
	"context"
	"net/http"
	"time"
)

// HTTPSender is the HTTP adapter for payment.Sender (spec §5, spec §8).
type HTTPSender struct {
	client *http.Client
}

// NewHTTPSender constructs an HTTPSender whose client enforces timeout on
// every request, from DUMMYPAY_WEBHOOK_TIMEOUT (spec §9).
func NewHTTPSender(timeout time.Duration) *HTTPSender {
	return &HTTPSender{client: &http.Client{Timeout: timeout}}
}

// Send POSTs body to url with the X-Webhook-Signature header set to the hex
// HMAC-SHA-256 of body under secret (spec §6). A non-2xx response is
// returned as httpStatus with a nil error; only a transport failure —
// connection refused, timeout, DNS — is returned as a non-nil error, with
// httpStatus 0, so callers can tell "the peer rejected it" from "the peer
// was never reached" (spec §5).
func (s *HTTPSender) Send(ctx context.Context, url string, body []byte, secret string) (httpStatus int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Webhook-Signature", SignPayload(secret, body))

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	return resp.StatusCode, nil
}
