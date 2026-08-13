package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"dummypay/internal/clock"
	httpapi "dummypay/internal/http"
	"dummypay/internal/payment"
)

// TestCreatePayment_FullyWired_ReturnsEncodedResponseBody is a regression
// test for Step 11.1's wiring: it runs the real HTTP handler on top of real
// Postgres adapters — the same construction cmd/dummypay does — rather than
// fakes, and decodes the response as httpapi.CreatePaymentResponse.
//
// This is the failure mode a fake-based or shape-only test cannot catch:
// the handler once wrote json.NewEncoder(w).Encode(result) — the whole
// payment.CreatePaymentResult, with Go's exported field names — instead of
// the already-correctly-encoded result.ResponseBody the use case returned.
// Every prior test either inspected CreatePaymentResponse's shape in
// isolation (Step 6.3) or exercised the use case without going through this
// handler, so the bug was only caught by manually running the wired
// service end to end. Decoding the live HTTP response into the public API
// type is what makes this failure mode visible in an automated test.
func TestCreatePayment_FullyWired_ReturnsEncodedResponseBody(t *testing.T) {
	pool := NewTestDB(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	// httpapi.NewRouterWithUseCase always builds the create-payment handler
	// bound to uuid.Nil — production threads the real seeded account id
	// through ProductionRouterDeps instead (cmd/dummypay). Seed the account
	// under uuid.Nil to satisfy the payments.account_id foreign key, since
	// this test's purpose is the response-body wiring, not account scoping
	// (already covered elsewhere).
	_, err := SeedAccount(ctx, pool, uuid.Nil, "test_account_wiring", now)
	require.NoError(t, err)

	ids := clock.UUIDv7Generator{}
	fakeClock := clock.NewFake(now)

	uc := payment.NewCreatePaymentUseCase(payment.CreatePaymentDeps{
		Tx:               NewTxManager(pool),
		Payments:         NewPaymentRepository(pool),
		Idempotency:      NewIdempotencyStore(pool),
		Outbox:           NewOutboxWriter(pool, ids, fakeClock),
		Subscriptions:    NewSubscriptionRepository(pool, testEncKey()),
		Deliveries:       NewDeliveryRepository(pool, ids),
		Clock:            fakeClock,
		IDs:              ids,
		ProcessingDelay:  3 * time.Second,
		IdempotencyLease: 30 * time.Second,
	})

	router := httpapi.NewRouterWithUseCase(
		httpapi.AuthConfig{AccountKeyID: "test", AccountKeySecret: "secret"},
		uc,
		nil,
	)

	body, _ := json.Marshal(map[string]any{
		"reference_id":  "checkout:wiring-test",
		"amount":        5000,
		"currency":      "BRL",
		"payment_token": "card_approved",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/payments", bytes.NewReader(body))
	req.Header.Set("Authorization", "Basic dGVzdDpzZWNyZXQ=") // test:secret
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "wiring-test-1")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)

	var resp httpapi.CreatePaymentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp),
		"response body must decode as CreatePaymentResponse, not the internal use-case result")

	require.True(t, len(resp.PaymentID) > 4 && resp.PaymentID[:4] == "pay_")
	require.True(t, len(resp.ProviderTransactionID) > 4 && resp.ProviderTransactionID[:4] == "txn_")
	require.Equal(t, "checkout:wiring-test", resp.ReferenceID)
	require.Equal(t, int64(5000), resp.Amount)
	require.Equal(t, "BRL", resp.Currency)
	require.Equal(t, "PROCESSING", resp.Status)
	require.NotEmpty(t, resp.CreatedAt)
}
