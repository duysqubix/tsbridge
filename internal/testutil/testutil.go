// Package testutil provides common test utilities for tsbridge tests.
package testutil

import (
	"net"
	"os"
	"path/filepath"
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

	dir, err := os.MkdirTemp("", "tsb")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	socketPath := filepath.Join(dir, "s.sock")
	listener, err := net.Listen("unix", socketPath)
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })

	// Accept and immediately close connections so callers can dial the socket.
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

// CreateTestTCPBackend creates a TCP backend that accepts and closes connections.
func CreateTestTCPBackend(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { listener.Close() })

	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()

	return listener.Addr().String()
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

	factory := func(string) tsnet.TSNetServer {
		return tsnet.NewMockTSNetServer()
	}

	server, err := tailscale.NewServerWithFactory(cfg, factory)
	require.NoError(t, err)

	return server
}
