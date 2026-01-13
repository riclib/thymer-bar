// Quick WebSocket test server - no Wails dependency
package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"thymer-bar/internal/thymer"
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	client := thymer.New()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", client.Handler(log))
	mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"connections":%d}`, client.ConnectionCount())
	})

	port := 8496
	fmt.Printf("Test server listening on :%d\n", port)
	fmt.Printf("  WebSocket: ws://127.0.0.1:%d/ws\n", port)
	fmt.Printf("  Status:    http://127.0.0.1:%d/status\n", port)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), mux); err != nil {
		log.Error("Server failed", "error", err)
	}
}
