package main

import (
	"log"
	"net/http"
	"os"

	"web2api/internal/app"
)

func main() {
	addr := os.Getenv("WEB2API_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	server, err := app.New()
	if err != nil {
		log.Fatalf("init app: %v", err)
	}

	log.Printf("web2api listening on %s", addr)
	if err := http.ListenAndServe(addr, server.Router()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
