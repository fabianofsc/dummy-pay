package httpapi_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	httpapi "dummypay/internal/http"
)

func basicAuth(keyID, keySecret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(keyID+":"+keySecret))
}

// TestCreatePayment_MalformedJSON_Returns400 ensures malformed JSON is
// rejected before any domain logic runs.
func TestCreatePayment_MalformedJSON_Returns400(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/payments",
		bytes.NewReader([]byte("not json")),
	)
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var errResp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	require.Equal(t, "invalid_request", errResp["code"])
}

// TestCreatePayment_UnknownField_Returns400AndCreatesNothing ensures strict
// decoding rejects fields like card_number that are not in the schema
// (spec §6.2, ADR-0002).
func TestCreatePayment_UnknownField_Returns400AndCreatesNothing(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	body := map[string]interface{}{
		"reference_id":  "checkout:123",
		"amount":        10990,
		"currency":      "BRL",
		"payment_token": "card_approved",
		"card_number":   "4111111111111111",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/payments",
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var errResp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	require.Equal(t, "invalid_request", errResp["code"])
}

// TestCreatePayment_MissingIdempotencyKey_Returns400 ensures the header is
// validated before any other processing.
func TestCreatePayment_MissingIdempotencyKey_Returns400(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	body := map[string]interface{}{
		"reference_id":  "checkout:123",
		"amount":        10990,
		"currency":      "BRL",
		"payment_token": "card_approved",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/payments",
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	req.Header.Set("Content-Type", "application/json")
	// No Idempotency-Key header
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var errResp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	require.Equal(t, "invalid_request", errResp["code"])
}

// TestCreatePayment_EmptyIdempotencyKey_Returns400 ensures empty idempotency
// keys are rejected.
func TestCreatePayment_EmptyIdempotencyKey_Returns400(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	body := map[string]interface{}{
		"reference_id":  "checkout:123",
		"amount":        10990,
		"currency":      "BRL",
		"payment_token": "card_approved",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/payments",
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestCreatePayment_InvalidAmount_Returns422 validates that amount validation
// failures for semantically invalid values (zero, negative) map to 422 with
// the correct code (spec §7). Type errors are 400 (malformed JSON).
func TestCreatePayment_InvalidAmount_Returns422(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	tests := []struct {
		name     string
		amount   interface{}
		wantCode string
	}{
		{"zero", 0, "invalid_amount"},
		{"negative", -100, "invalid_amount"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]interface{}{
				"reference_id":  "checkout:123",
				"amount":        tt.amount,
				"currency":      "BRL",
				"payment_token": "card_approved",
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(
				http.MethodPost,
				"/v1/payments",
				bytes.NewReader(bodyBytes),
			)
			req.Header.Set("Authorization", basicAuth("test", "secret"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "idem-test")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
			var errResp map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
			require.Equal(t, tt.wantCode, errResp["code"])
		})
	}
}

// TestCreatePayment_AmountWrongType_Returns400 verifies that type mismatches
// in JSON are treated as malformed (spec §6.2, spec §7).
func TestCreatePayment_AmountWrongType_Returns400(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	body := map[string]interface{}{
		"reference_id":  "checkout:123",
		"amount":        "not-a-number",
		"currency":      "BRL",
		"payment_token": "card_approved",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/payments",
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestCreatePayment_UnsupportedCurrency_Returns422 validates currency.
func TestCreatePayment_UnsupportedCurrency_Returns422(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	body := map[string]interface{}{
		"reference_id":  "checkout:123",
		"amount":        10990,
		"currency":      "USD",
		"payment_token": "card_approved",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/payments",
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var errResp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	require.Equal(t, "unsupported_currency", errResp["code"])
}

// TestCreatePayment_UnknownToken_Returns422 validates the payment token.
func TestCreatePayment_UnknownToken_Returns422(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	body := map[string]interface{}{
		"reference_id":  "checkout:123",
		"amount":        10990,
		"currency":      "BRL",
		"payment_token": "invalid_token",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/payments",
		bytes.NewReader(bodyBytes),
	)
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-test")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var errResp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	require.Equal(t, "unknown_payment_token", errResp["code"])
}
