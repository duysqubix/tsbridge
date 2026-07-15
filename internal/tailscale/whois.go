package tailscale

import (
	"context"
	"fmt"
	"time"

	"github.com/jtdowney/tsbridge/internal/metrics"
	tsnetpkg "github.com/jtdowney/tsbridge/internal/tsnet"
	"tailscale.com/client/tailscale/apitype"
)

// WhoisClientAdapter adapts a TSNetServer to implement the middleware.WhoisClient interface
type WhoisClientAdapter struct {
	server  tsnetpkg.TSNetServer
	metrics *metrics.Collector
	service string
}

// NewWhoisClientAdapter creates an adapter for the given TSNetServer. The
// collector and service name record successful whois lookup duration; metrics
// may be nil to disable recording.
func NewWhoisClientAdapter(server tsnetpkg.TSNetServer, metrics *metrics.Collector, service string) *WhoisClientAdapter {
	return &WhoisClientAdapter{server: server, metrics: metrics, service: service}
}

// WhoIs performs a whois lookup for the given remote address
func (w *WhoisClientAdapter) WhoIs(ctx context.Context, remoteAddr string) (*apitype.WhoIsResponse, error) {
	// Get the local client from the tsnet server
	lc, err := w.server.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("getting local client: %w", err)
	}

	// Time successful backend lookups; the middleware caches above us, so this
	// excludes cache hits and failed retry attempts.
	start := time.Now()
	resp, err := lc.WhoIs(ctx, remoteAddr)
	if err == nil && w.metrics != nil {
		w.metrics.RecordWhoisDuration(w.service, time.Since(start))
	}
	return resp, err
}
