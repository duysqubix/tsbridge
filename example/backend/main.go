package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"
)

type Response struct {
	Service   string              `json:"service"`
	Timestamp string              `json:"timestamp"`
	Message   string              `json:"message"`
	Headers   map[string][]string `json:"headers,omitempty"`
}

func main() {
	serviceName := os.Getenv("SERVICE_NAME")
	if serviceName == "" {
		serviceName = "backend"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Log the request
		slog.Info("request received",
			"service", serviceName,
			"method", r.Method,
			"path", r.URL.Path,
			"remote_addr", r.RemoteAddr)

		// Check for Tailscale headers
		tailscaleHeaders := make(map[string][]string)
		for name, values := range r.Header {
			if strings.HasPrefix(name, "X-Tailscale") {
				tailscaleHeaders[name] = values
			}
		}

		response := Response{
			Service:   serviceName,
			Timestamp: time.Now().Format(time.RFC3339),
			Message:   fmt.Sprintf("Hello from %s backend!", serviceName),
		}

		if len(tailscaleHeaders) > 0 {
			response.Headers = tailscaleHeaders
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(response); err != nil {
			slog.Error("error encoding response", "error", err)
		}
	})

	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("healthy")); err != nil {
			slog.Error("error writing response", "error", err)
		}
	})

	slog.Info("starting server", "service", serviceName, "port", port)
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           nil,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
	}
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}
