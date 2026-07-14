// Package helpers provides common test utilities for integration tests.
package helpers

import (
	"testing"
	"time"

	"github.com/jtdowney/tsbridge/internal/config"
)

// TestFixture provides a standard configuration builder for tests
type TestFixture struct {
	t   *testing.T
	cfg *config.Config
}

// NewTestFixture creates a new test fixture with minimal defaults
func NewTestFixture(t *testing.T) *TestFixture {
	t.Helper()

	cfg := baseTestConfig(t)
	cfg.Services = []config.Service{defaultService("test-service", "localhost:8080")}

	return &TestFixture{
		t:   t,
		cfg: cfg,
	}
}

// WithService adds or updates a service configuration
func (f *TestFixture) WithService(name, backendAddr string) *TestFixture {
	// Check if service exists
	for i, svc := range f.cfg.Services {
		if svc.Name == name {
			f.cfg.Services[i].BackendAddr = backendAddr
			return f
		}
	}

	// Add new service
	f.cfg.Services = append(f.cfg.Services, defaultService(name, backendAddr))
	return f
}

// WithOAuth configures OAuth authentication
func (f *TestFixture) WithOAuth(clientID, clientSecret string) *TestFixture {
	f.cfg.Tailscale.AuthKey = config.RedactedString("")
	f.cfg.Tailscale.OAuthClientID = clientID
	f.cfg.Tailscale.OAuthClientSecret = config.RedactedString(clientSecret)
	return f
}

// WithTimeout sets a specific timeout value
func (f *TestFixture) WithTimeout(name string, duration time.Duration) *TestFixture {
	switch name {
	case "read":
		f.cfg.Global.ReadHeaderTimeout = new(duration)
	case "write":
		f.cfg.Global.WriteTimeout = new(duration)
	case "idle":
		f.cfg.Global.IdleTimeout = new(duration)
	case "shutdown":
		f.cfg.Global.ShutdownTimeout = new(duration)
	}
	return f
}

// Build returns the configured Config
func (f *TestFixture) Build() *config.Config {
	return f.cfg
}
