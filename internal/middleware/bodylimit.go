// Package middleware implements HTTP middleware for request processing.
package middleware

import "net/http"

// MaxBytesHandler returns a middleware that limits the size of request bodies.
// If maxBytes is negative, no limit is applied.
func MaxBytesHandler(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if maxBytes < 0 {
			// No limit
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check Content-Length header first for efficiency
			if r.ContentLength > maxBytes {
				http.Error(w, "Request body too large", http.StatusRequestEntityTooLarge)
				return
			}

			// Wrap streamed bodies whose size is not known from Content-Length.
			// Reverse-proxy read failures preserve MaxBytesError and return 413.
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

			next.ServeHTTP(w, r)
		})
	}
}
