package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/jtdowney/tsbridge/internal/config"
	"github.com/jtdowney/tsbridge/internal/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCLIArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		debugEnv  string
		want      *cliArgs
		errString string
	}{
		{
			name: "defaults",
			want: &cliArgs{
				provider:           "file",
				labelPrefix:        "tsbridge",
				dockerPollInterval: time.Minute,
			},
		},
		{
			name: "all flags",
			args: []string{
				"-config", "config.toml",
				"-provider", "docker",
				"-docker-socket", "tcp://localhost:2375",
				"-docker-label-prefix", "custom",
				"-docker-poll-interval", "30s",
				"-verbose",
				"-help",
				"-version",
				"-validate",
			},
			want: &cliArgs{
				configPath:         "config.toml",
				provider:           "docker",
				dockerEndpoint:     "tcp://localhost:2375",
				labelPrefix:        "custom",
				dockerPollInterval: 30 * time.Second,
				verbose:            true,
				help:               true,
				version:            true,
				validate:           true,
			},
		},
		{
			name:     "short help",
			args:     []string{"-h"},
			debugEnv: "1",
			want: &cliArgs{
				provider:           "file",
				labelPrefix:        "tsbridge",
				dockerPollInterval: time.Minute,
				verbose:            true,
				help:               true,
			},
		},
		{
			name:      "unknown flag",
			args:      []string{"-unknown"},
			errString: "flag provided but not defined",
		},
		{
			name:      "invalid duration",
			args:      []string{"-docker-poll-interval", "later"},
			errString: "invalid value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("TSBRIDGE_DEBUG", tt.debugEnv)

			got, err := parseCLIArgs(tt.args)

			if tt.errString != "" {
				require.Error(t, err)
				assert.ErrorContains(t, err, tt.errString)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRunValidation(t *testing.T) {
	t.Run("accepts valid configuration", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "config.toml")
		require.NoError(t, os.WriteFile(configPath, []byte(`
[tailscale]
auth_key = "test-auth-key"

[[services]]
name = "test-service"
backend_addr = "localhost:8080"
`), 0o600))

		err := run(&cliArgs{
			validate:   true,
			provider:   "file",
			configPath: configPath,
		}, make(chan os.Signal))

		require.NoError(t, err)
	})

	t.Run("reports load failure", func(t *testing.T) {
		err := run(&cliArgs{
			validate:   true,
			provider:   "file",
			configPath: filepath.Join(t.TempDir(), "missing.toml"),
		}, make(chan os.Signal))

		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to load configuration")
	})
}

func TestCreateProvider(t *testing.T) {
	t.Run("creates file provider without Docker", func(t *testing.T) {
		dockerCalled := false
		provider, err := createProvider(&cliArgs{
			provider:   "file",
			configPath: "test.toml",
		}, func(docker.Options) (config.Provider, error) {
			dockerCalled = true
			return nil, nil
		})

		require.NoError(t, err)
		assert.Equal(t, "file", provider.Name())
		assert.False(t, dockerCalled)
	})

	t.Run("forwards Docker options to constructor", func(t *testing.T) {
		wantProvider := config.NewFileProvider("marker")
		wantOptions := docker.Options{
			DockerEndpoint: "unix:///custom/docker.sock",
			LabelPrefix:    "custom",
			PollInterval:   5 * time.Second,
		}

		provider, err := createProvider(&cliArgs{
			provider:           "docker",
			dockerEndpoint:     wantOptions.DockerEndpoint,
			labelPrefix:        wantOptions.LabelPrefix,
			dockerPollInterval: wantOptions.PollInterval,
		}, func(got docker.Options) (config.Provider, error) {
			assert.Equal(t, wantOptions, got)
			return wantProvider, nil
		})

		require.NoError(t, err)
		assert.Same(t, wantProvider, provider)
	})

	t.Run("rejects unknown provider as validation error", func(t *testing.T) {
		provider, err := createProvider(&cliArgs{provider: "unknown"}, func(docker.Options) (config.Provider, error) {
			t.Fatal("Docker constructor must not be called")
			return nil, nil
		})

		assert.Nil(t, provider)
		require.Error(t, err)
		assert.ErrorContains(t, err, "unknown provider type: unknown")
	})
}

type lifecycleApp struct {
	started  chan struct{}
	shutdown chan struct{}
	startErr error
}

func (a *lifecycleApp) Start(context.Context) error {
	close(a.started)
	return a.startErr
}

func (a *lifecycleApp) Shutdown(context.Context) error {
	close(a.shutdown)
	return nil
}

func TestRunApplication(t *testing.T) {
	t.Run("shuts down after signal", func(t *testing.T) {
		application := &lifecycleApp{
			started:  make(chan struct{}),
			shutdown: make(chan struct{}),
		}
		sigCh := make(chan os.Signal, 1)
		errCh := make(chan error, 1)

		go func() {
			errCh <- runApplication(application, sigCh)
		}()

		<-application.started
		sigCh <- os.Interrupt

		require.NoError(t, <-errCh)
		select {
		case <-application.shutdown:
		default:
			t.Fatal("application was not shut down")
		}
	})

	t.Run("returns startup failure", func(t *testing.T) {
		application := &lifecycleApp{
			started:  make(chan struct{}),
			shutdown: make(chan struct{}),
			startErr: errors.New("start failed"),
		}

		err := runApplication(application, make(chan os.Signal))

		require.Error(t, err)
		assert.ErrorContains(t, err, "failed to start application")
		assert.ErrorContains(t, err, "start failed")
		select {
		case <-application.shutdown:
			t.Fatal("application was shut down after startup failure")
		default:
		}
	})
}

func TestMainProcessBoundary(t *testing.T) {
	binPath := filepath.Join(t.TempDir(), "tsbridge")
	build := exec.Command("go", "build", "-o", binPath, ".")
	require.NoError(t, build.Run())

	tests := []struct {
		name       string
		args       []string
		wantExit   int
		wantOutput string
	}{
		{
			name:       "version exits successfully",
			args:       []string{"-version"},
			wantExit:   0,
			wantOutput: "tsbridge version:",
		},
		{
			name:       "parse error exits with usage error",
			args:       []string{"-unknown"},
			wantExit:   2,
			wantOutput: "flag provided but not defined",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(binPath, tt.args...)
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output

			err := cmd.Run()
			exitCode := 0
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, tt.wantExit, exitCode)
			assert.Contains(t, output.String(), tt.wantOutput)
		})
	}
}
