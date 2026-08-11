// Package httpapi is the HTTP adapter: routing, decoding, validation, and
// status-code mapping. It depends on internal/payment through the ports the
// domain declares; the domain never depends on this package.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// NewRouter builds the top-level router. /health is unauthenticated and
// exists for local and orchestrator liveness checks; every route under /v1
// requires the technical account's credentials once that group exists.
func NewRouter() *chi.Mux {
	r := chi.NewRouter()

	r.Get("/health", handleHealth)

	return r
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}
