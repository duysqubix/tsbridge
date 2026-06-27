package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

// requestIDKey is the context key for storing request IDs
type requestIDKey struct{}

// RequestID is a middleware that ensures each request has a unique request ID.
// If the incoming request has an X-Request-ID header, it uses that value.
// Otherwise, it generates a new UUID. The request ID is added to the request
// context and included in the response headers.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if request already has a request ID
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			// Generate a new UUID
			requestID = uuid.New().String()
			// Add it to the request headers so downstream handlers can see it
			r.Header.Set("X-Request-ID", requestID)
		}

		// Add request ID to context
		ctx := context.WithValue(r.Context(), requestIDKey{}, requestID)
		r = r.WithContext(ctx)

		// Add request ID to response headers
		w.Header().Set("X-Request-ID", requestID)

		// Call the next handler
		next.ServeHTTP(w, r)
	})
}

// GetRequestID extracts the request ID from the context.
// Returns empty string if no request ID is found.
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey{}).(string); ok {
		return requestID
	}
	return ""
}

// LogWithRequestID returns a logger that includes the request ID from the context
func LogWithRequestID(ctx context.Context) *slog.Logger {
	requestID := GetRequestID(ctx)
	if requestID == "" {
		return slog.Default()
	}
	return slog.With("request_id", requestID)
}
