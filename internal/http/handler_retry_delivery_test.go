package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	httpapi "dummypay/internal/http"
)

// TestRetryDelivery_WrongPrefix_Returns404 verifies that a well-formed UUID
// carrying the wrong identifier prefix is rejected as not found rather than
// looked up (spec §4.3, ADR-0006).
func TestRetryDelivery_WrongPrefix_Returns404(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	// A well-formed UUID with the payment prefix instead of the delivery one.
	req := httptest.NewRequest(http.MethodPost, "/v1/webhook-deliveries/pay_0199a1f4-3c82-7d19-b4e6-2f8a91c05d3b/retry", nil)
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
	var errResp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &errResp))
	require.Equal(t, "not_found", errResp["code"])
}

// TestRetryDelivery_MalformedID_Returns404 verifies a malformed UUID is
// rejected as not found.
func TestRetryDelivery_MalformedID_Returns404(t *testing.T) {
	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     "test",
		AccountKeySecret: "secret",
	})

	req := httptest.NewRequest(http.MethodPost, "/v1/webhook-deliveries/dlv_not-a-uuid/retry", nil)
	req.Header.Set("Authorization", basicAuth("test", "secret"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}
