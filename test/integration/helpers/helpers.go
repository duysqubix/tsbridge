// Package helpers provides common test utilities for integration tests.
package helpers

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jtdowney/tsbridge/internal/config"
	"github.com/stretchr/testify/require"
)

// RequestTracker tracks requests to a test backend server.
type RequestTracker struct {
	RequestsStarted       int32
	RequestsCompleted     int32
	MaxConcurrentRequests int32
	activeRequests        int32
}

// RecordStart records the start of a request.
func (rt *RequestTracker) RecordStart() {
	atomic.AddInt32(&rt.RequestsStarted, 1)
	active := atomic.AddInt32(&rt.activeRequests, 1)

	// Update max concurrent if needed
	for {
		current := atomic.LoadInt32(&rt.MaxConcurrentRequests)
		if active <= current || atomic.CompareAndSwapInt32(&rt.MaxConcurrentRequests, current, active) {
			break
		}
	}
}

// RecordComplete records the completion of a request.
func (rt *RequestTracker) RecordComplete() {
	atomic.AddInt32(&rt.activeRequests, -1)
	atomic.AddInt32(&rt.RequestsCompleted, 1)
}

// CreateTestBackend creates a simple test backend server that returns OK.
func CreateTestBackend(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	t.Cleanup(func() { server.Close() })
	return server
}

// CreateTrackingBackend creates a backend server that tracks requests.
func CreateTrackingBackend(t *testing.T) (*httptest.Server, *RequestTracker) {
	t.Helper()

	tracker := &RequestTracker{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tracker.RecordStart()
		defer tracker.RecordComplete()

		// Handle common test paths
		switch r.URL.Path {
		case "/slow":
			time.Sleep(100 * time.Millisecond)
		case "/error":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("Internal Server Error"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))

	t.Cleanup(func() { server.Close() })
	return server, tracker
}

// baseTestConfig returns a Config with standard Tailscale and Global defaults
// and no services.
func baseTestConfig(t *testing.T) *config.Config {
	t.Helper()

	return &config.Config{
		Tailscale: config.Tailscale{
			AuthKey:  config.RedactedString("tskey-auth-test123"),
			StateDir: t.TempDir(),
		},
		Global: config.Global{
			MetricsAddr:       "localhost:0",
			ReadHeaderTimeout: new(30 * time.Second),
			WriteTimeout:      new(30 * time.Second),
			IdleTimeout:       new(120 * time.Second),
			ShutdownTimeout:   new(10 * time.Second),
		},
	}
}

// defaultService returns a Service with standard test defaults.
func defaultService(name, backendAddr string) config.Service {
	return config.Service{
		Name:         name,
		BackendAddr:  backendAddr,
		TLSMode:      "off",
		WhoisEnabled: new(false),
	}
}

// CreateTestConfig creates a standard test configuration.
func CreateTestConfig(t *testing.T, serviceName string, backendAddr string) *config.Config {
	t.Helper()

	cfg := baseTestConfig(t)
	cfg.Services = []config.Service{defaultService(serviceName, backendAddr)}
	return cfg
}

// CreateMultiServiceConfig creates a test configuration with multiple services.
func CreateMultiServiceConfig(t *testing.T, services map[string]string) *config.Config {
	t.Helper()

	cfg := baseTestConfig(t)
	for name, addr := range services {
		cfg.Services = append(cfg.Services, defaultService(name, addr))
	}
	return cfg
}

// BuildTestBinary builds the tsbridge binary for testing.
func BuildTestBinary(t *testing.T) string {
	t.Helper()

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "tsbridge")

	// Build using relative path from test directory
	cmd := exec.Command("go", "build", "-o", binPath, "../../cmd/tsbridge")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build binary: %v\n%s", err, output)
	}

	return binPath
}

// TSBridgeProcess wraps an exec.Cmd for tsbridge with helper methods.
type TSBridgeProcess struct {
	cmd      *exec.Cmd
	t        *testing.T
	shutdown bool

	mu     sync.Mutex
	output strings.Builder
	done   chan struct{}
}

// StartTSBridge starts a tsbridge process with common setup.
func StartTSBridge(t *testing.T, configPath string, extraEnv ...string) *TSBridgeProcess {
	t.Helper()

	binPath := BuildTestBinary(t)

	ctx, cancel := context.WithCancel(context.Background()) //nolint:gosec // G118 - cancel called via t.Cleanup
	t.Cleanup(func() { cancel() })

	cmd := exec.CommandContext(ctx, binPath, "-config", configPath, "-verbose")

	// Set up environment
	cmd.Env = append(os.Environ(), "TSBRIDGE_TEST_MODE=1")
	cmd.Env = append(cmd.Env, extraEnv...)

	// Capture output
	outputPipe, err := cmd.StdoutPipe()
	require.NoError(t, err, "failed to create stdout pipe")

	errPipe, err := cmd.StderrPipe()
	require.NoError(t, err, "failed to create stderr pipe")

	// Start the process
	err = cmd.Start()
	require.NoError(t, err, "failed to start tsbridge")

	process := &TSBridgeProcess{
		cmd:  cmd,
		t:    t,
		done: make(chan struct{}),
	}

	// Stream stdout and stderr concurrently into a shared buffer while the
	// process runs, so WaitForStartup can watch for the startup marker (which
	// tsbridge logs to stderr).
	var wg sync.WaitGroup
	wg.Add(2)
	go process.readPipe(&wg, outputPipe)
	go process.readPipe(&wg, errPipe)
	go func() {
		wg.Wait()
		close(process.done)
	}()

	// Wait for startup instead of fixed sleep
	process.WaitForStartup()

	// Set up cleanup
	t.Cleanup(func() {
		process.Shutdown()
	})

	return process
}

// Shutdown gracefully shuts down the tsbridge process.
func (p *TSBridgeProcess) Shutdown() {
	p.t.Helper()

	if p.shutdown {
		return
	}
	p.shutdown = true

	// Send interrupt signal
	if err := p.cmd.Process.Signal(os.Interrupt); err != nil {
		p.t.Logf("failed to send interrupt signal: %v", err)
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited
	case <-time.After(5 * time.Second):
		p.t.Log("tsbridge did not shut down within timeout, killing")
		_ = p.cmd.Process.Kill()
	}
}

// readPipe streams a process pipe line by line into the shared output buffer
// until the pipe closes (i.e. the process exits).
func (p *TSBridgeProcess) readPipe(wg *sync.WaitGroup, r io.Reader) {
	defer wg.Done()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		p.mu.Lock()
		p.output.WriteString(scanner.Text())
		p.output.WriteByte('\n')
		p.mu.Unlock()
	}
}

// outputContains reports whether the output captured so far contains s.
func (p *TSBridgeProcess) outputContains(s string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Contains(p.output.String(), s)
}

// WaitForStartup blocks until tsbridge reports that a service is serving,
// polling the captured log output with exponential backoff. It returns as soon
// as the marker appears or the process exits; if neither happens within the
// budget it returns anyway so the caller fails on a clear assertion rather than
// hanging.
func (p *TSBridgeProcess) WaitForStartup() {
	p.t.Helper()

	const readyMarker = "started service"
	const maxWait = 6 * time.Second

	var elapsed time.Duration
	for delay := 50 * time.Millisecond; elapsed < maxWait; {
		if p.outputContains(readyMarker) {
			return
		}
		select {
		case <-p.done: // process exited before it became ready
			return
		case <-time.After(delay):
		}
		elapsed += delay
		if delay < time.Second {
			delay *= 2
		}
	}
	if !p.outputContains(readyMarker) {
		p.t.Logf("WaitForStartup: %q not observed within %s; continuing", readyMarker, maxWait)
	}
}

// GetOutput returns the captured output from the process.
// Should be called after Shutdown to ensure all output is captured.
func (p *TSBridgeProcess) GetOutput() string {
	// If process is still running, shutdown first
	if p.cmd.Process != nil {
		p.Shutdown()
	}

	select {
	case <-p.done:
	case <-time.After(1 * time.Second):
		return "Failed to get output"
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	return p.output.String()
}

// WriteConfigFile writes a TOML config file for testing.
func WriteConfigFile(t *testing.T, cfg *config.Config) string {
	t.Helper()

	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test-config.toml")

	// Simple TOML generation for common test cases
	var content strings.Builder
	fmt.Fprintf(&content, `[tailscale]
state_dir = "%s"`, cfg.Tailscale.StateDir)

	// Add auth configuration
	if cfg.Tailscale.AuthKey.Value() != "" {
		fmt.Fprintf(&content, `
auth_key = "%s"`, cfg.Tailscale.AuthKey.Value())
	}
	if cfg.Tailscale.OAuthClientID != "" {
		fmt.Fprintf(&content, `
oauth_client_id = "%s"
oauth_client_secret = "%s"`, cfg.Tailscale.OAuthClientID, cfg.Tailscale.OAuthClientSecret.Value())
	}

	// Add default_tags if present
	if len(cfg.Tailscale.DefaultTags) > 0 {
		content.WriteString(`
default_tags = [`)
		for i, tag := range cfg.Tailscale.DefaultTags {
			if i > 0 {
				content.WriteString(", ")
			}
			fmt.Fprintf(&content, `"%s"`, tag)
		}
		content.WriteString(`]`)
	}

	// Build global section
	fmt.Fprintf(&content, `

[global]
metrics_addr = "%s"`,
		cfg.Global.MetricsAddr)

	if cfg.Global.ReadHeaderTimeout != nil {
		fmt.Fprintf(&content, `
read_header_timeout = "%s"`, *cfg.Global.ReadHeaderTimeout)
	}
	if cfg.Global.WriteTimeout != nil {
		fmt.Fprintf(&content, `
write_timeout = "%s"`, *cfg.Global.WriteTimeout)
	}
	if cfg.Global.IdleTimeout != nil {
		fmt.Fprintf(&content, `
idle_timeout = "%s"`, *cfg.Global.IdleTimeout)
	}
	if cfg.Global.ShutdownTimeout != nil {
		fmt.Fprintf(&content, `
shutdown_timeout = "%s"`, *cfg.Global.ShutdownTimeout)
	}

	content.WriteString(`

`)

	// Add services
	for _, svc := range cfg.Services {
		whoisEnabled := "false"
		if svc.WhoisEnabled != nil && *svc.WhoisEnabled {
			whoisEnabled = "true"
		}

		fmt.Fprintf(&content, `[[services]]
name = "%s"
backend_addr = "%s"
tls_mode = "%s"
whois_enabled = %s
`, svc.Name, svc.BackendAddr, svc.TLSMode, whoisEnabled)

		// Add optional fields
		if svc.WhoisTimeout != nil && *svc.WhoisTimeout > 0 {
			fmt.Fprintf(&content, `whois_timeout = "%s"
`, *svc.WhoisTimeout)
		}

		// Add tags if present
		if len(svc.Tags) > 0 {
			content.WriteString(`tags = [`)
			for i, tag := range svc.Tags {
				if i > 0 {
					content.WriteString(", ")
				}
				fmt.Fprintf(&content, `"%s"`, tag)
			}
			content.WriteString(`]
`)
		}
	}

	err := os.WriteFile(configPath, []byte(content.String()), 0600)
	require.NoError(t, err, "failed to write config file")

	return configPath
}
