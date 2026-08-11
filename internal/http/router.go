// Package httpapi is the HTTP adapter: routing, decoding, validation, and
// status-code mapping. It depends on internal/payment through the ports the
// domain declares; the domain never depends on this package.
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// AuthConfig holds the technical account credentials used to authenticate
// all requests under /v1 (ADR-0005, ADR-0010).
type AuthConfig struct {
	AccountKeyID     string
	AccountKeySecret string
}

// NewRouter builds the top-level router. /health is unauthenticated and
// exists for local and orchestrator liveness checks; every route under /v1
// requires the technical account's credentials (spec §6.1, ADR-0005).
func NewRouter(auth AuthConfig) *chi.Mux {
	r := chi.NewRouter()

	r.Get("/health", handleHealth)

	// /v1 group with authentication middleware.
	r.Route("/v1", func(v1 chi.Router) {
		v1.Use(basicAuthMiddleware(auth))

		v1.Post("/payments", handleCreatePayment)
		v1.Post("/webhook-subscriptions", handleCreateSubscription)
		v1.Post("/webhook-deliveries/{delivery_id}/retry", handleRetryDelivery)
	})

	return r
}

// basicAuthMiddleware verifies HTTP Basic authentication against the
// configured account credentials. A missing or invalid header returns 401
// before any handler is called.
func basicAuthMiddleware(auth AuthConfig) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			const prefix = "Basic "
			if !strings.HasPrefix(authHeader, prefix) {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			decoded, err := base64.StdEncoding.DecodeString(authHeader[len(prefix):])
			if err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) != 2 {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			keyID, keySecret := parts[0], parts[1]
			if keyID != auth.AccountKeyID || keySecret != auth.AccountKeySecret {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func handleCreateSubscription(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not Implemented", http.StatusNotImplemented)
}

func handleRetryDelivery(w http.ResponseWriter, r *http.Request) {
	http.Error(w, "Not Implemented", http.StatusNotImplemented)
}
