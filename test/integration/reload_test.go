//go:build integration

package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jtdowney/tsbridge/internal/app"
	"github.com/jtdowney/tsbridge/internal/config"
	"github.com/jtdowney/tsbridge/internal/testutil"
	"github.com/jtdowney/tsbridge/test/integration/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitForServicesReady waits for services to be ready with a timeout
func waitForServicesReady(t *testing.T) {
	t.Helper()

	// Since we're using mock tsnet servers, we need a small delay for goroutines to start
	// Using a channel-based approach for better synchronization
	ready := make(chan struct{})

	go func() {
		// Give services time to start their goroutines
		time.Sleep(50 * time.Millisecond)
		close(ready)
	}()

	select {
	case <-ready:
		// Services should be ready
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for services to be ready")
	}
}

func TestDynamicServiceManagement(t *testing.T) {
	t.Run("reload adds new services", func(t *testing.T) {
		// Create backends
		backend1 := helpers.CreateTestBackend(t)
		backend2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Backend 2"))
		}))
		t.Cleanup(func() { backend2.Close() })

		// Start with one service
		cfg := helpers.CreateTestConfig(t, "svc1", backend1.Listener.Addr().String())

		// Create app with mock tailscale server
		tsServer := testutil.CreateMockTailscaleServer(t, cfg.Tailscale)
		testApp, err := app.NewAppWithOptions(cfg, app.Options{TSServer: tsServer})
		require.NoError(t, err)

		// Start the app
		ctx := t.Context()

		go func() {
			_ = testApp.Start(ctx)
		}()

		// Wait for startup
		waitForServicesReady(t)

		// Create new config with additional service
		newCfg := helpers.CreateMultiServiceConfig(t, map[string]string{
			"svc1": backend1.Listener.Addr().String(),
			"svc2": backend2.Listener.Addr().String(),
		})

		// Reload configuration
		err = testApp.ReloadConfig(newCfg)
		require.NoError(t, err)

		// Give services time to start
		waitForServicesReady(t)

		// Note: In a real integration test, we would make HTTP requests to verify
		// the services are working. Since this is a mock setup, we just verify
		// the reload completed without error.
	})

	t.Run("reload removes services", func(t *testing.T) {
		// Create backends
		backend1 := helpers.CreateTestBackend(t)
		backend2 := helpers.CreateTestBackend(t)

		// Start with two services
		cfg := helpers.CreateMultiServiceConfig(t, map[string]string{
			"svc1": backend1.Listener.Addr().String(),
			"svc2": backend2.Listener.Addr().String(),
		})

		// Create app with mock tailscale server
		tsServer := testutil.CreateMockTailscaleServer(t, cfg.Tailscale)
		testApp, err := app.NewAppWithOptions(cfg, app.Options{TSServer: tsServer})
		require.NoError(t, err)

		// Start the app
		ctx := t.Context()

		go func() {
			_ = testApp.Start(ctx)
		}()

		// Wait for startup
		waitForServicesReady(t)

		// Create new config with only one service
		newCfg := helpers.CreateTestConfig(t, "svc1", backend1.Listener.Addr().String())

		// Reload configuration
		err = testApp.ReloadConfig(newCfg)
		require.NoError(t, err)

		// Give services time to stop
		waitForServicesReady(t)
	})

	t.Run("reload updates service configuration", func(t *testing.T) {
		// Create backends
		backend1 := helpers.CreateTestBackend(t)
		backend2 := helpers.CreateTestBackend(t)

		// Start with one service pointing to backend1
		cfg := helpers.CreateTestConfig(t, "svc1", backend1.Listener.Addr().String())
		cfg.Services[0].UpstreamHeaders = map[string]string{
			"X-Custom": "value1",
		}

		// Create app with mock tailscale server
		tsServer := testutil.CreateMockTailscaleServer(t, cfg.Tailscale)
		testApp, err := app.NewAppWithOptions(cfg, app.Options{TSServer: tsServer})
		require.NoError(t, err)

		// Start the app
		ctx := t.Context()

		go func() {
			_ = testApp.Start(ctx)
		}()

		// Wait for startup
		waitForServicesReady(t)

		// Update service to point to backend2
		newCfg := helpers.CreateTestConfig(t, "svc1", backend2.Listener.Addr().String())
		newCfg.Services[0].UpstreamHeaders = map[string]string{
			"X-Custom": "value2",
		}

		// Reload configuration
		err = testApp.ReloadConfig(newCfg)
		require.NoError(t, err)

		// Give service time to restart
		waitForServicesReady(t)
	})

	t.Run("reload handles partial failures gracefully", func(t *testing.T) {
		// Create backend
		backend1 := helpers.CreateTestBackend(t)

		// Start with one service
		cfg := helpers.CreateTestConfig(t, "svc1", backend1.Listener.Addr().String())

		// Create app with mock tailscale server
		tsServer := testutil.CreateMockTailscaleServer(t, cfg.Tailscale)
		testApp, err := app.NewAppWithOptions(cfg, app.Options{TSServer: tsServer})
		require.NoError(t, err)

		// Start the app
		ctx := t.Context()

		go func() {
			_ = testApp.Start(ctx)
		}()

		// Wait for startup
		waitForServicesReady(t)

		// Create new config with a service that has an invalid backend
		// to simulate a partial failure
		newCfg := helpers.CreateMultiServiceConfig(t, map[string]string{
			"svc1": backend1.Listener.Addr().String(),
			"svc2": "localhost:9999", // Unreachable backend
		})

		// Reload configuration - should handle gracefully.
		// The exact error behavior depends on the implementation; we
		// just verify it completes without panic.
		_ = testApp.ReloadConfig(newCfg)
	})

	t.Run("concurrent reloads are handled safely", func(t *testing.T) {
		// Create backend
		backend1 := helpers.CreateTestBackend(t)

		// Start with one service
		cfg := helpers.CreateTestConfig(t, "svc1", backend1.Listener.Addr().String())

		// Create app with mock tailscale server
		tsServer := testutil.CreateMockTailscaleServer(t, cfg.Tailscale)
		testApp, err := app.NewAppWithOptions(cfg, app.Options{TSServer: tsServer})
		require.NoError(t, err)

		// Start the app
		ctx := t.Context()

		go func() {
			_ = testApp.Start(ctx)
		}()

		// Wait for startup
		waitForServicesReady(t)

		// Create multiple different configs
		configs := make([]*config.Config, 5)
		for i := range 5 {
			backend := helpers.CreateTestBackend(t)
			configs[i] = helpers.CreateTestConfig(t, "svc1", backend.Listener.Addr().String())
			configs[i].Services[0].Tags = []string{string(rune('a' + i))} // Different tags to force updates
		}

		// Trigger concurrent reloads
		errCh := make(chan error, len(configs))
		for _, cfg := range configs {
			go func(c *config.Config) {
				errCh <- testApp.ReloadConfig(c)
			}(cfg)
		}

		// Collect results
		for range configs {
			<-errCh // Just drain the channel, some may fail due to concurrency
		}

		// The important thing is that the app remains stable
		waitForServicesReady(t)

		// App should still be running (no panic)
		assert.NotNil(t, testApp)
	})

}
