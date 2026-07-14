// Package middleware implements HTTP middleware for request processing.
package middleware

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"log/slog"

	"github.com/cenkalti/backoff/v4"
	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jtdowney/tsbridge/internal/constants"
	tserrors "github.com/jtdowney/tsbridge/internal/errors"
	"tailscale.com/client/tailscale/apitype"
)

var headerCleaner = strings.NewReplacer("\r", "", "\n", "")

func sanitizeHeaderValue(v string) string {
	return headerCleaner.Replace(v)
}

// extractHostFromRemoteAddr extracts just the host portion from a host:port address.
// This is used for cache keys to ensure the same client IP hits the cache regardless
// of which source port they connect from.
func extractHostFromRemoteAddr(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// Fallback to full address if not in host:port format
		return remoteAddr
	}
	return host
}

type WhoisClient interface {
	WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error)
}

func Whois(client WhoisClient, enabled bool, timeout time.Duration, cacheSize int, cacheTTL time.Duration) func(http.Handler) http.Handler {
	var cache *expirable.LRU[string, *apitype.WhoIsResponse]
	if cacheSize > 0 {
		cache = expirable.NewLRU[string, *apitype.WhoIsResponse](cacheSize, nil, cacheTTL)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				next.ServeHTTP(w, r)
				return
			}

			performWhoisLookup(client, timeout, r, cache)

			next.ServeHTTP(w, r)
		})
	}
}

func performWhoisWithRetryLogic(client WhoisClient, timeout time.Duration, r *http.Request) (*apitype.WhoIsResponse, error) {
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = constants.RetryInitialInterval
	b.MaxInterval = constants.RetryMaxInterval
	b.MaxElapsedTime = constants.RetryMaxElapsedTime
	b.Multiplier = constants.RetryMultiplier
	b.RandomizationFactor = constants.RetryRandomizationFactor

	backoffWithRetries := backoff.WithMaxRetries(b, constants.RetryMaxAttempts)
	ctxBackoff := backoff.WithContext(backoffWithRetries, r.Context())

	var response *apitype.WhoIsResponse
	operation := func() error {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()

		var err error
		response, err = client.WhoIs(ctx, r.RemoteAddr)

		switch {
		case err == nil:
			return nil
		case isWhoisRetryableError(err):
			// Retry context errors (timeouts, cancellation) and certain network errors.
			return err
		default:
			// Don't retry authentication or other permanent failures.
			return backoff.Permanent(err)
		}
	}

	err := backoff.Retry(operation, ctxBackoff)
	return response, err
}

// isWhoisRetryableError determines if a whois error is worth retrying
func isWhoisRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Check for our custom network errors with retryable status codes
	var tsbridgeErr *tserrors.Error
	if errors.As(err, &tsbridgeErr) && tsbridgeErr.Type == tserrors.ErrTypeNetwork {
		// Retry on 5xx server errors if status code is set
		if tsbridgeErr.HTTPStatusCode >= 500 && tsbridgeErr.HTTPStatusCode < 600 {
			return true
		}
	}

	// Retry on timeout and context cancellation errors
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}

	// Check for standard library network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		// Retry on timeouts
		return netErr.Timeout()
	}

	// Check for specific syscall errors
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return true
		}
		// Check for connection refused
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) {
			return true
		}
	}

	// Check for DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		// Retry on DNS not found errors (might be transient)
		return dnsErr.IsNotFound
	}

	// As a last resort, check for specific error messages
	// This is less reliable but catches errors from libraries that don't use standard types
	errStr := err.Error()
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "network is unreachable")
}

func performWhoisLookup(client WhoisClient, timeout time.Duration, r *http.Request, cache *expirable.LRU[string, *apitype.WhoIsResponse]) {
	var cacheKey string
	if cache != nil {
		cacheKey = extractHostFromRemoteAddr(r.RemoteAddr)
		if cached, ok := cache.Get(cacheKey); ok {
			addUserHeaders(r, cached)
			addAddressHeaders(r, cached)
			return
		}
	}

	resp, err := performWhoisWithRetryLogic(client, timeout, r)
	if err != nil {
		logWhoisError(err, r.RemoteAddr, timeout)
		return
	}

	if resp == nil {
		return
	}

	if cache != nil {
		cache.Add(cacheKey, resp)
	}

	addUserHeaders(r, resp)
	addAddressHeaders(r, resp)
}

// logWhoisError logs the appropriate error message based on the error type
func logWhoisError(err error, remoteAddr string, timeout time.Duration) {
	if errors.Is(err, context.DeadlineExceeded) {
		slog.Warn("whois lookup timed out", "remote_addr", remoteAddr, "timeout", timeout)
	} else {
		slog.Warn("whois lookup failed", "remote_addr", remoteAddr, "error", err)
	}
}

// addUserHeaders adds user-related headers from the whois response
func addUserHeaders(r *http.Request, resp *apitype.WhoIsResponse) {
	if resp.UserProfile == nil {
		return
	}

	if resp.UserProfile.LoginName != "" {
		loginName := sanitizeHeaderValue(resp.UserProfile.LoginName)
		r.Header.Set("X-Tailscale-User", loginName)
		r.Header.Set("X-Tailscale-Login", loginName)
	}
	if resp.UserProfile.DisplayName != "" {
		r.Header.Set("X-Tailscale-Name", sanitizeHeaderValue(resp.UserProfile.DisplayName))
	}
	if resp.UserProfile.ProfilePicURL != "" {
		r.Header.Set("X-Tailscale-Profile-Picture", sanitizeHeaderValue(resp.UserProfile.ProfilePicURL))
	}
}

// addAddressHeaders adds address-related headers from the whois response
func addAddressHeaders(r *http.Request, resp *apitype.WhoIsResponse) {
	if resp.Node == nil || len(resp.Node.Addresses) == 0 {
		return
	}

	// Convert prefixes to IP addresses and join with comma
	var addresses []string
	for _, prefix := range resp.Node.Addresses {
		addresses = append(addresses, prefix.Addr().String())
	}
	r.Header.Set("X-Tailscale-Addresses", strings.Join(addresses, ","))
}
