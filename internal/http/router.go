// Package httpapi is the HTTP adapter: routing, decoding, validation, and
// status-code mapping. It depends on internal/payment through the ports the
// domain declares; the domain never depends on this package.
package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"dummypay/internal/payment"
)

// AuthConfig holds the technical account credentials used to authenticate
// all requests under /v1 (ADR-0005, ADR-0010).
type AuthConfig struct {
	AccountKeyID     string
	AccountKeySecret string
}

// NewRouterWithUseCase builds a router with an injected create-payment use
// case for testing. Production uses NewProductionRouter.
func NewRouterWithUseCase(auth AuthConfig, uc *payment.CreatePaymentUseCase, accountRepo interface { /* payment repository or compatible */
}) *chi.Mux {
	r := chi.NewRouter()

	r.Get("/health", handleHealth)

	r.Route("/v1", func(v1 chi.Router) {
		v1.Use(basicAuthMiddleware(auth))

		v1.Post("/payments", makeHandleCreatePayment(uc, uuid.Nil))
		v1.Post("/webhook-subscriptions", handleCreateSubscription)
		v1.Post("/webhook-deliveries/{delivery_id}/retry", handleRetryDelivery)
	})

	return r
}

// NewRouter builds the top-level router with boundary validation active but
// no use case wired — every request that passes validation gets 501. Used by
// handler-level tests that only exercise decoding, validation, and
// authentication. Production uses NewProductionRouter.
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

// ProductionRouterDeps bundles every dependency the fully wired production
// router needs (Phase 11 assembly). V1 has exactly one technical account
// (spec §1 "Technical account"), so AccountID is a single value threaded
// into every handler rather than derived per-request. Clock backs the
// request logger's timing — internal/http never calls time.Now() itself
// (ADR-0012); the caller (cmd/dummypay) supplies the real clock.
type ProductionRouterDeps struct {
	Auth               AuthConfig
	AccountID          uuid.UUID
	Clock              payment.Clock
	CreatePayment      *payment.CreatePaymentUseCase
	CreateSubscription *payment.CreateSubscriptionUseCase
	RetryDelivery      *payment.RetryDeliveryUseCase
}

// NewProductionRouter builds the router cmd/dummypay actually serves: every
// /v1 handler backed by its real use case, all sharing the one technical
// account (spec §6.1, ADR-0005).
func NewProductionRouter(deps ProductionRouterDeps) *chi.Mux {
	r := chi.NewRouter()

	r.Use(requestLogger(deps.Clock))

	r.Get("/health", handleHealth)

	r.Route("/v1", func(v1 chi.Router) {
		v1.Use(basicAuthMiddleware(deps.Auth))

		v1.Post("/payments", makeHandleCreatePayment(deps.CreatePayment, deps.AccountID))
		v1.Post("/webhook-subscriptions", makeHandleCreateSubscription(deps.CreateSubscription, deps.AccountID))
		v1.Post("/webhook-deliveries/{delivery_id}/retry", makeHandleRetryDeliveryForAccount(deps.RetryDelivery, deps.AccountID))
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
				respondError(w, http.StatusUnauthorized, "unauthorized", "missing Authorization header")
				return
			}

			const prefix = "Basic "
			if !strings.HasPrefix(authHeader, prefix) {
				respondError(w, http.StatusUnauthorized, "unauthorized", "invalid Authorization scheme")
				return
			}

			decoded, err := base64.StdEncoding.DecodeString(authHeader[len(prefix):])
			if err != nil {
				respondError(w, http.StatusUnauthorized, "unauthorized", "invalid Authorization encoding")
				return
			}

			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) != 2 {
				respondError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials format")
				return
			}

			keyID, keySecret := parts[0], parts[1]
			if keyID != auth.AccountKeyID || keySecret != auth.AccountKeySecret {
				respondError(w, http.StatusUnauthorized, "unauthorized", "invalid credentials")
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

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// requestLogger returns logging middleware timed from clk rather than
// time.Now() — internal/http is not one of the packages ADR-0012 permits to
// read wall-clock time directly, so the caller (cmd/dummypay, which holds
// the real clock) supplies it.
func requestLogger(clk payment.Clock) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := clk.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)
			log.Printf("%s %s %d %s", r.Method, r.URL.Path, rec.status, clk.Now().Sub(start).Round(time.Microsecond))
		})
	}
}
