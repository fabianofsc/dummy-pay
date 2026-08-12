package httpapi_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	httpapi "dummypay/internal/http"
)

// TestV1Routes_WithoutCredentials_Returns401 verifies that every route under
// /v1 requires Basic authentication. The test covers the known routes that will
// be implemented: POST /v1/payments, POST /v1/webhook-subscriptions, POST
// /v1/webhook-deliveries/{id}/retry (spec §6.1, ADR-0005).
func TestV1Routes_WithoutCredentials_Returns401(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test-account",
		AccountKeySecret: "test-secret",
	})

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/payments"},
		{http.MethodPost, "/v1/webhook-subscriptions"},
		{http.MethodPost, "/v1/webhook-deliveries/dlv_00000000-0000-0000-0000-000000000000/retry"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code,
				"route %s %s must return 401 without credentials", tt.method, tt.path)
			require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			var errResp httpapi.ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
			require.Equal(t, "unauthorized", errResp.Code)
		})
	}
}

// TestV1Routes_WithWrongCredentials_Returns401 ensures invalid credentials are
// rejected for all /v1 routes.
func TestV1Routes_WithWrongCredentials_Returns401(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test-account",
		AccountKeySecret: "test-secret",
	})

	tests := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/v1/payments"},
		{http.MethodPost, "/v1/webhook-subscriptions"},
		{http.MethodPost, "/v1/webhook-deliveries/dlv_00000000-0000-0000-0000-000000000000/retry"},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("wrong:creds")))
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusUnauthorized, rec.Code,
				"route %s %s must return 401 with wrong credentials", tt.method, tt.path)
			require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

			var errResp httpapi.ErrorResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
			require.Equal(t, "unauthorized", errResp.Code)
		})
	}
}

// TestHealth_NoAuthenticationRequired verifies that /health remains
// unauthenticated, for liveness checks without credentials.
func TestHealth_NoAuthenticationRequired(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test-account",
		AccountKeySecret: "test-secret",
	})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
}
