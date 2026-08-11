// Command dummypay runs the DummyPay HTTP service. This is the only place
// concrete adapters are constructed and wired together (ADR-0003).
package main

import (
	"log"
	"net/http"

	httpapi "dummypay/internal/http"
)

func main() {
	addr := ":8080"
	router := httpapi.NewRouter()

	log.Printf("dummypay listening on %s", addr)
	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}
