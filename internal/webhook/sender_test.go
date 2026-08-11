package webhook_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"dummypay/internal/webhook"
)

// TestHTTPSender_Send_SignatureVerifiesOverRawBody verifies that the sender
// posts the exact body bytes with the signature header, and that a consumer
// recomputing the HMAC over the raw bytes it received gets the same value —
// exactly the verification the README instructs a real consumer to perform
// (spec §6).
func TestHTTPSender_Send_SignatureVerifiesOverRawBody(t *testing.T) {
	const secret = "whsec_test_sender_secret"
	body := []byte(`{"event_id":"evt_1","type":"payment.approved"}`)
	signature := webhook.SignPayload(secret, body)

	var (
		gotBody []byte
		gotSig  string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Webhook-Signature")
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		gotBody = b
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := webhook.NewHTTPSender(5 * time.Second)
	status, err := sender.Send(context.Background(), server.URL, body, signature)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	require.Equal(t, body, gotBody)
	require.Equal(t, signature, gotSig)

	// Consumer-side verification: recompute the HMAC over the raw bytes
	// received and compare to the header, as the README instructs.
	recomputed := webhook.SignPayload(secret, gotBody)
	require.Equal(t, signature, recomputed)
}

// TestHTTPSender_Send_NonTwoXX_ReturnsStatusWithNoError verifies a non-2xx
// response is reported via the status, not an error — the delivery reached
// its destination, it just wasn't accepted (spec §5).
func TestHTTPSender_Send_NonTwoXX_ReturnsStatusWithNoError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	sender := webhook.NewHTTPSender(5 * time.Second)
	status, err := sender.Send(context.Background(), server.URL, []byte("{}"), "sig")

	require.NoError(t, err)
	require.Equal(t, http.StatusInternalServerError, status)
}

// TestHTTPSender_Send_TransportError_ReturnsZeroStatusWithError verifies
// that a connection failure is distinguishable from a non-2xx response: an
// error with status 0, never a status the peer never actually sent (spec §5).
func TestHTTPSender_Send_TransportError_ReturnsZeroStatusWithError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := server.URL
	server.Close() // closed before any request reaches it: connection refused

	sender := webhook.NewHTTPSender(5 * time.Second)
	status, err := sender.Send(context.Background(), url, []byte("{}"), "sig")

	require.Error(t, err)
	require.Equal(t, 0, status)
}

// TestHTTPSender_Send_HonoursTimeout verifies the sender does not wait
// indefinitely for a server that never responds. The handler blocks on a
// channel rather than sleeping — the client's own configured timeout is what
// ends the request, not a test-controlled delay (ADR-0012: no test sleeps).
//
// The channel must close before server.Close() runs: Close() waits for
// in-flight connections to finish, and the handler goroutine is still
// blocked in <-block even after the client gives up (the client's timeout
// only ends its own wait, it does not unblock the handler). Both happen in
// one deferred func, in order, rather than relying on defer/t.Cleanup
// ordering across two separate registrations.
func TestHTTPSender_Send_HonoursTimeout(t *testing.T) {
	block := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-block
	}))
	defer func() {
		close(block)
		server.Close()
	}()

	sender := webhook.NewHTTPSender(20 * time.Millisecond)
	status, err := sender.Send(context.Background(), server.URL, []byte("{}"), "sig")

	require.Error(t, err)
	require.Equal(t, 0, status)
}
