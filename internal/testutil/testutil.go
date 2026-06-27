// Package testutil provides common test utilities for tsbridge tests.
package testutil

import (
	"net"
	"os"
	"strings"
	"testing"

	"github.com/jtdowney/tsbridge/internal/config"
	"github.com/jtdowney/tsbridge/internal/tailscale"
	"github.com/jtdowney/tsbridge/internal/tsnet"
	"github.com/stretchr/testify/require"
)

// CreateTestUnixSocket creates a temporary unix socket for testing.
// The socket is automatically cleaned up when the test completes.
func CreateTestUnixSocket(t *testing.T) string {
	t.Helper()

	// Use a shorter path to avoid macOS unix socket path length limits
	// Replace slashes with dashes to make valid filename
	safeName := strings.ReplaceAll(t.Name(), "/", "-")
	socketPath := "/tmp/tsb-" + safeName + ".sock"

	// Remove any existing socket file
	os.Remove(socketPath)

	// Create a simple unix socket server
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		listener.Close()
		os.Remove(socketPath)
	})

	// Start a simple server in the background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	return socketPath
}

// CreateMockTailscaleServer creates a mock tailscale server for testing.
// If cfg is empty, it will use default test values.
func CreateMockTailscaleServer(t *testing.T, cfg config.Tailscale) *tailscale.Server {
	t.Helper()

	// Set defaults if not provided
	if cfg.AuthKey == "" {
		cfg.AuthKey = "test-key"
	}
	if cfg.StateDir == "" {
		cfg.StateDir = t.TempDir()
	}

	factory := func(serviceName string) tsnet.TSNetServer {
		return tsnet.NewMockTSNetServer()
	}

	server, err := tailscale.NewServerWithFactory(cfg, factory)
	require.NoError(t, err)

	return server
}
