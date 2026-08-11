package httpapi_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	httpapi "dummypay/internal/http"
)

// TestCreateSubscription_InvalidURL_Returns422 validates the url field
// (spec §4.2, spec §7).
func TestCreateSubscription_InvalidURL_Returns422(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	body := map[string]interface{}{
		"url":    "not-a-url",
		"events": []string{"payment.approved"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhook-subscriptions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var errResp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	require.Equal(t, "invalid_request", errResp["code"])
}

// TestCreateSubscription_UnknownEventType_Returns422 validates event types.
func TestCreateSubscription_UnknownEventType_Returns422(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	body := map[string]interface{}{
		"url":    "http://consumer:8080/webhook",
		"events": []string{"payment.bogus"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhook-subscriptions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var errResp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	require.Equal(t, "invalid_request", errResp["code"])
}

// TestCreateSubscription_EmptyEvents_Returns422 validates events is
// non-empty.
func TestCreateSubscription_EmptyEvents_Returns422(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	body := map[string]interface{}{
		"url":    "http://consumer:8080/webhook",
		"events": []string{},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/v1/webhook-subscriptions", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

// TestCreateSubscription_MalformedJSON_Returns400 ensures malformed JSON is
// rejected.
func TestCreateSubscription_MalformedJSON_Returns400(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/webhook-subscriptions", bytes.NewReader([]byte("not json")))
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}
