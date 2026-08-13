package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"dummypay/internal/clock"
	httpapi "dummypay/internal/http"
	"dummypay/internal/payment"
	"dummypay/internal/webhook"
)

const processingDelay = 3 * time.Second
const idempotencyLease = 30 * time.Second
const webhookTimeout = 5 * time.Second

func basicAuth(keyID, keySecret string) string {
	return "Basic dGVzdDpzZWNyZXQ="
}

type acceptanceDeps struct {
	Pool      *pgxpool.Pool
	Router    http.Handler
	Worker    *payment.Worker
	AccountID uuid.UUID
	Clock     *clock.Fake
}

func newAcceptanceDeps(t *testing.T, now time.Time) acceptanceDeps {
	t.Helper()
	pool := NewTestDB(t)
	ctx := context.Background()

	accountID, err := SeedAccount(ctx, pool, uuid.New(), "acceptance_test", now)
	require.NoError(t, err)

	ids := clock.UUIDv7Generator{}
	fakeClock := clock.NewFake(now)
	encKey := testEncKey()

	tx := NewTxManager(pool)
	payments := NewPaymentRepository(pool)
	idempotency := NewIdempotencyStore(pool)
	outbox := NewOutboxWriter(pool, ids, fakeClock)
	subscriptions := NewSubscriptionRepository(pool, encKey)
	deliveries := NewDeliveryRepository(pool, ids)

	createPayment := payment.NewCreatePaymentUseCase(payment.CreatePaymentDeps{
		Tx:               tx,
		Payments:         payments,
		Idempotency:      idempotency,
		Outbox:           outbox,
		Subscriptions:    subscriptions,
		Deliveries:       deliveries,
		Clock:            fakeClock,
		IDs:              ids,
		ProcessingDelay:  processingDelay,
		IdempotencyLease: idempotencyLease,
	})

	createSubscription := payment.NewCreateSubscriptionUseCase(payment.CreateSubscriptionDeps{
		Subscriptions: subscriptions,
		Secrets:       webhook.SecretGenerator{},
		IDs:           ids,
		Clock:         fakeClock,
	})

	retryDelivery := payment.NewRetryDeliveryUseCase(payment.RetryDeliveryDeps{
		Tx:         tx,
		Deliveries: deliveries,
		Outbox:     outbox,
		Clock:      fakeClock,
	})

	auth := httpapi.AuthConfig{AccountKeyID: "test", AccountKeySecret: "secret"}
	router := httpapi.NewProductionRouter(httpapi.ProductionRouterDeps{
		Auth:               auth,
		AccountID:          accountID,
		Clock:              fakeClock,
		CreatePayment:      createPayment,
		CreateSubscription: createSubscription,
		RetryDelivery:      retryDelivery,
	})

	worker := payment.NewWorker(payment.WorkerDeps{
		Tx:            tx,
		Claimer:       NewOutboxClaimer(pool),
		Payments:      payments,
		Subscriptions: subscriptions,
		Deliveries:    deliveries,
		Outbox:        outbox,
		Sender:        webhook.NewHTTPSender(webhookTimeout),
		IDs:           ids,
		Clock:         fakeClock,
	})

	return acceptanceDeps{
		Pool:      pool,
		Router:    router,
		Worker:    worker,
		AccountID: accountID,
		Clock:     fakeClock,
	}
}

func newAuthHeader() string {
	return basicAuth("test", "secret")
}

func paymentBody(refID string, amount int64, token string) []byte {
	b, _ := json.Marshal(map[string]any{
		"reference_id":  refID,
		"amount":        amount,
		"currency":      "BRL",
		"payment_token": token,
	})
	return b
}

func postPayment(t *testing.T, router http.Handler, idempotencyKey string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/payments", bytes.NewReader(body))
	req.Header.Set("Authorization", newAuthHeader())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func assertPaymentResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantPaymentStatus string) httpapi.CreatePaymentResponse {
	t.Helper()
	require.Equal(t, wantStatus, rec.Code)
	var resp httpapi.CreatePaymentResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.True(t, strings.HasPrefix(resp.PaymentID, "pay_"), "payment_id must have pay_ prefix")
	require.True(t, strings.HasPrefix(resp.ProviderTransactionID, "txn_"), "provider_transaction_id must have txn_ prefix")
	require.Equal(t, wantPaymentStatus, resp.Status)
	require.NotEmpty(t, resp.CreatedAt)
	return resp
}

func assertErrorResponse(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	require.Equal(t, wantStatus, rec.Code)
	var errResp httpapi.ErrorResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	require.Equal(t, wantCode, errResp.Code)
}

func queryPaymentStatus(t *testing.T, pool *pgxpool.Pool, paymentID string) string {
	t.Helper()
	rawID := strings.TrimPrefix(paymentID, "pay_")
	id, err := uuid.Parse(rawID)
	require.NoError(t, err)

	var status string
	err = pool.QueryRow(context.Background(),
		"SELECT status FROM payments WHERE id = $1", id,
	).Scan(&status)
	require.NoError(t, err)
	return status
}

func queryDeliveryForPayment(t *testing.T, pool *pgxpool.Pool, paymentID string) (uuid.UUID, string, int, int) {
	t.Helper()
	rawID := strings.TrimPrefix(paymentID, "pay_")
	id, err := uuid.Parse(rawID)
	require.NoError(t, err)

	var deliveryID uuid.UUID
	var status string
	var attemptCount int
	var lastHTTPStatus int
	err = pool.QueryRow(context.Background(),
		`SELECT id, status, attempt_count, COALESCE(last_http_status, 0)
		 FROM webhook_deliveries WHERE payment_id = $1`, id,
	).Scan(&deliveryID, &status, &attemptCount, &lastHTTPStatus)
	require.NoError(t, err)
	return deliveryID, status, attemptCount, lastHTTPStatus
}

func subscriptionBody(url string, events []string) []byte {
	b, _ := json.Marshal(map[string]any{
		"url":    url,
		"events": events,
	})
	return b
}

func postSubscription(t *testing.T, router http.Handler, url string, events []string) *httptest.ResponseRecorder {
	t.Helper()
	body := subscriptionBody(url, events)
	req := httptest.NewRequest(http.MethodPost, "/v1/webhook-subscriptions", bytes.NewReader(body))
	req.Header.Set("Authorization", newAuthHeader())
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func postRetry(t *testing.T, router http.Handler, deliveryID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/webhook-deliveries/"+deliveryID+"/retry", nil)
	req.Header.Set("Authorization", newAuthHeader())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func encodeDeliveryID(id uuid.UUID) string {
	return "dlv_" + id.String()
}

// --- Acceptance criteria ---

func TestAcceptance_CardApproved_StartsProcessingThenSettlesApproved(t *testing.T) {
	deps := newAcceptanceDeps(t, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()

	rec := postPayment(t, deps.Router, "accept-card-approved",
		paymentBody("checkout:approved", 10990, "card_approved"))

	resp := assertPaymentResponse(t, rec, http.StatusCreated, "PROCESSING")
	require.Equal(t, int64(10990), resp.Amount)
	require.Equal(t, "BRL", resp.Currency)
	require.Equal(t, "PROCESSING", queryPaymentStatus(t, deps.Pool, resp.PaymentID))

	deps.Clock.Advance(processingDelay)
	require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))

	require.Equal(t, "APPROVED", queryPaymentStatus(t, deps.Pool, resp.PaymentID))
}

func TestAcceptance_CardDeclined_StartsProcessingThenSettlesRejected(t *testing.T) {
	deps := newAcceptanceDeps(t, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()

	rec := postPayment(t, deps.Router, "accept-card-declined",
		paymentBody("checkout:declined", 5000, "card_declined"))

	resp := assertPaymentResponse(t, rec, http.StatusCreated, "PROCESSING")
	require.Equal(t, int64(5000), resp.Amount)
	require.Equal(t, "PROCESSING", queryPaymentStatus(t, deps.Pool, resp.PaymentID))

	deps.Clock.Advance(processingDelay)
	require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))

	require.Equal(t, "REJECTED", queryPaymentStatus(t, deps.Pool, resp.PaymentID))
}

func TestAcceptance_CardProcessingApproved_SettlesToApproved(t *testing.T) {
	deps := newAcceptanceDeps(t, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()

	rec := postPayment(t, deps.Router, "accept-proc-approved",
		paymentBody("checkout:processing-ok", 10990, "card_processing_approved"))

	resp := assertPaymentResponse(t, rec, http.StatusCreated, "PROCESSING")
	require.Equal(t, "PROCESSING", queryPaymentStatus(t, deps.Pool, resp.PaymentID))

	deps.Clock.Advance(processingDelay)
	require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))

	require.Equal(t, "APPROVED", queryPaymentStatus(t, deps.Pool, resp.PaymentID))
}

func TestAcceptance_CardProcessingDeclined_SettlesToRejected(t *testing.T) {
	deps := newAcceptanceDeps(t, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()

	rec := postPayment(t, deps.Router, "accept-proc-declined",
		paymentBody("checkout:processing-no", 25000, "card_processing_declined"))

	resp := assertPaymentResponse(t, rec, http.StatusCreated, "PROCESSING")
	require.Equal(t, "PROCESSING", queryPaymentStatus(t, deps.Pool, resp.PaymentID))

	deps.Clock.Advance(processingDelay)
	require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))

	require.Equal(t, "REJECTED", queryPaymentStatus(t, deps.Pool, resp.PaymentID))
}

func TestAcceptance_IdempotentReplay_ReturnsOriginalTransaction(t *testing.T) {
	deps := newAcceptanceDeps(t, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))

	body := paymentBody("checkout:replay", 10990, "card_approved")
	key := "accept-idem-replay"

	first := postPayment(t, deps.Router, key, body)
	firstResp := assertPaymentResponse(t, first, http.StatusCreated, "PROCESSING")

	second := postPayment(t, deps.Router, key, body)
	secondResp := assertPaymentResponse(t, second, http.StatusCreated, "PROCESSING")

	require.Equal(t, firstResp.PaymentID, secondResp.PaymentID)
	require.Equal(t, firstResp.ProviderTransactionID, secondResp.ProviderTransactionID)
	require.Equal(t, first.Body.Bytes(), second.Body.Bytes(), "replay response must be byte-identical")
}

func TestAcceptance_SameKeyDifferentBody_CreatesNothing(t *testing.T) {
	deps := newAcceptanceDeps(t, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))

	key := "accept-idem-reuse"

	first := postPayment(t, deps.Router, key,
		paymentBody("checkout:original", 10990, "card_approved"))
	assertPaymentResponse(t, first, http.StatusCreated, "PROCESSING")

	second := postPayment(t, deps.Router, key,
		paymentBody("checkout:different", 5000, "card_declined"))
	assertErrorResponse(t, second, http.StatusUnprocessableEntity, "idempotency_key_reuse")
}

func TestAcceptance_ConcurrentDuplicateRequests_Return409(t *testing.T) {
	deps := newAcceptanceDeps(t, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()

	body := paymentBody("checkout:concurrent", 10990, "card_approved")
	key := "accept-idem-concurrent"

	// Pre-claim the key as IN_FLIGHT so every concurrent request will see
	// IN_FLIGHT (not COMPLETED) and return 409 — exactly the scenario the
	// acceptance criterion describes: the first request is still in flight.
	store := NewIdempotencyStore(deps.Pool)
	now := deps.Clock.Now()
	amount, _ := payment.NewAmount(10990)
	currency, _ := payment.NewCurrency("BRL")
	token, _ := payment.NewScenarioToken("card_approved")
	fp := payment.Fingerprint("checkout:concurrent", amount, currency, token)
	ok, err := store.Claim(ctx, deps.AccountID, key, fp, now)
	require.NoError(t, err)
	require.True(t, ok)

	const goroutines = 10
	var (
		ready   sync.WaitGroup
		done    sync.WaitGroup
		results = make(chan int, goroutines)
		start   = make(chan struct{})
	)

	ready.Add(goroutines)
	done.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer done.Done()
			ready.Done()
			<-start
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/v1/payments", bytes.NewReader(body))
			req.Header.Set("Authorization", newAuthHeader())
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", key)
			deps.Router.ServeHTTP(rec, req)
			results <- rec.Code
		}()
	}

	ready.Wait()
	close(start)
	done.Wait()
	close(results)

	var conflicts int
	for code := range results {
		if code == http.StatusConflict {
			conflicts++
		}
	}
	require.Equal(t, goroutines, conflicts, "all requests with an in-flight key must return 409")
}

func TestAcceptance_HMACSignature_VerifiesOverRawBody(t *testing.T) {
	deps := newAcceptanceDeps(t, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()

	var receivedSecret string
	var receivedBody []byte
	var receivedSig string
	ready := make(chan struct{}, 1)

	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody = nil
		receivedSig = r.Header.Get("X-Webhook-Signature")
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		receivedBody = buf.Bytes()
		ready <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	subRec := postSubscription(t, deps.Router, consumer.URL, []string{"payment.approved"})
	require.Equal(t, http.StatusCreated, subRec.Code)
	var subResp httpapi.CreateSubscriptionResponse
	require.NoError(t, json.Unmarshal(subRec.Body.Bytes(), &subResp))
	require.True(t, strings.HasPrefix(subResp.Secret, "whsec_"))
	require.True(t, strings.HasPrefix(subResp.SubscriptionID, "sub_"))

	receivedSecret = subResp.Secret

	rec := postPayment(t, deps.Router, "accept-hmac",
		paymentBody("checkout:hmac", 10990, "card_approved"))
	assertPaymentResponse(t, rec, http.StatusCreated, "PROCESSING")

	deps.Clock.Advance(processingDelay)
	require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))
	require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))
	<-ready

	require.NotEmpty(t, receivedBody, "consumer must receive the webhook body")
	require.NotEmpty(t, receivedSig, "consumer must receive X-Webhook-Signature")

	expectedSig := webhook.SignPayload(receivedSecret, receivedBody)
	require.Equal(t, expectedSig, receivedSig, "HMAC signature over the raw body must match")

	var event map[string]any
	require.NoError(t, json.Unmarshal(receivedBody, &event))
	require.Equal(t, "payment.approved", event["type"])
}

func TestAcceptance_WebhookFailure_RecordsFailedWithMetadata(t *testing.T) {
	deps := newAcceptanceDeps(t, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()

	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer consumer.Close()

	subRec := postSubscription(t, deps.Router, consumer.URL, []string{"payment.approved"})
	require.Equal(t, http.StatusCreated, subRec.Code)

	rec := postPayment(t, deps.Router, "accept-webhook-fail",
		paymentBody("checkout:fail", 10990, "card_approved"))
	resp := assertPaymentResponse(t, rec, http.StatusCreated, "PROCESSING")

	deps.Clock.Advance(processingDelay)
	require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))
	require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))

	_, status, attemptCount, lastHTTPStatus := queryDeliveryForPayment(t, deps.Pool, resp.PaymentID)
	require.Equal(t, "FAILED", status)
	require.Equal(t, 1, attemptCount, "attempt_count must be incremented")
	require.Equal(t, http.StatusInternalServerError, lastHTTPStatus, "last_http_status must record the 500")
}

func TestAcceptance_Retry_ResendsAndSucceeds(t *testing.T) {
	deps := newAcceptanceDeps(t, time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()

	var attempts int
	var firstBody []byte

	consumer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		bodyBytes := buf.Bytes()

		if attempts == 1 {
			firstBody = append([]byte(nil), bodyBytes...)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		require.Equal(t, firstBody, bodyBytes,
			"retried webhook body must be byte-identical to the first attempt")
		w.WriteHeader(http.StatusOK)
	}))
	defer consumer.Close()

	subRec := postSubscription(t, deps.Router, consumer.URL, []string{"payment.approved"})
	require.Equal(t, http.StatusCreated, subRec.Code)

	rec := postPayment(t, deps.Router, "accept-retry",
		paymentBody("checkout:retry", 10990, "card_approved"))
	resp := assertPaymentResponse(t, rec, http.StatusCreated, "PROCESSING")

	deps.Clock.Advance(processingDelay)
	require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))
	require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))

	deliveryID, status, attemptCount, lastHTTPStatus := queryDeliveryForPayment(t, deps.Pool, resp.PaymentID)
	require.Equal(t, "FAILED", status)
	require.Equal(t, 1, attemptCount)
	require.Equal(t, http.StatusInternalServerError, lastHTTPStatus)

	retryRec := postRetry(t, deps.Router, encodeDeliveryID(deliveryID))
	require.Equal(t, http.StatusOK, retryRec.Code)
	var retryResp struct {
		DeliveryID      string `json:"delivery_id"`
		Status          string `json:"status"`
		AttemptCount    int    `json:"attempt_count"`
		LastAttemptedAt string `json:"last_attempted_at,omitempty"`
		LastHTTPStatus  *int   `json:"last_http_status,omitempty"`
	}
	require.NoError(t, json.Unmarshal(retryRec.Body.Bytes(), &retryResp))
	require.Equal(t, encodeDeliveryID(deliveryID), retryResp.DeliveryID)
	require.Equal(t, "PENDING", retryResp.Status)
	require.Equal(t, 1, retryResp.AttemptCount)

	require.NoError(t, deps.Worker.ProcessBatch(ctx, 50))

	_, status, attemptCount, _ = queryDeliveryForPayment(t, deps.Pool, resp.PaymentID)
	require.Equal(t, "SENT", status)
	require.Equal(t, 2, attemptCount)
}
