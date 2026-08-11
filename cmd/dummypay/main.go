// Command dummypay runs the DummyPay HTTP service. This is the only place
// concrete adapters are constructed and wired together (ADR-0003).
package main

import (
	"log"
	"net/http"
	"os"

	httpapi "dummypay/internal/http"
)

func main() {
	addr := os.Getenv("DUMMYPAY_HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	keyID := os.Getenv("DUMMYPAY_ACCOUNT_KEY_ID")
	keySecret := os.Getenv("DUMMYPAY_ACCOUNT_KEY_SECRET")

	router := httpapi.NewRouter(httpapi.AuthConfig{
		AccountKeyID:     keyID,
		AccountKeySecret: keySecret,
	})

	log.Printf("dummypay listening on %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}
