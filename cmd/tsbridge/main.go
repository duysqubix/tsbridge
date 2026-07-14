// Package main provides the tsbridge CLI application for managing Tailscale proxy services.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"log/slog"

	"github.com/jtdowney/tsbridge/internal/app"
	"github.com/jtdowney/tsbridge/internal/config"
	"github.com/jtdowney/tsbridge/internal/constants"
	"github.com/jtdowney/tsbridge/internal/docker"
	tserrors "github.com/jtdowney/tsbridge/internal/errors"
)

var version = "dev"

// cliArgs holds parsed command-line arguments
type cliArgs struct {
	configPath         string
	provider           string
	dockerEndpoint     string
	labelPrefix        string
	dockerPollInterval time.Duration
	verbose            bool
	help               bool
	version            bool
	validate           bool
}

// parseCLIArgs parses command-line arguments and returns the parsed values
func parseCLIArgs(args []string) (*cliArgs, error) {
	fs := flag.NewFlagSet("tsbridge", flag.ContinueOnError)

	result := &cliArgs{}
	fs.StringVar(&result.configPath, "config", "", "Path to TOML configuration file (required for file provider)")
	fs.StringVar(&result.provider, "provider", "file", "Configuration provider (file or docker)")
	fs.StringVar(&result.dockerEndpoint, "docker-socket", "", "Docker socket endpoint (falls back to DOCKER_HOST env var, then unix:///var/run/docker.sock)")
	fs.StringVar(&result.labelPrefix, "docker-label-prefix", "tsbridge", "Docker label prefix for configuration")
	fs.DurationVar(&result.dockerPollInterval, "docker-poll-interval", constants.DockerPollInterval, "Docker config poll interval (0 to disable)")
	fs.BoolVar(&result.verbose, "verbose", false, "Enable debug logging")
	fs.BoolVar(&result.help, "help", false, "Show usage information")
	fs.BoolVar(&result.help, "h", false, "Show usage information")
	fs.BoolVar(&result.version, "version", false, "Show version information")
	fs.BoolVar(&result.validate, "validate", false, "Validate configuration and exit")

	// Create usage function
	usage := func() {
		fmt.Fprintf(os.Stdout, "Usage of %s:\n", fs.Name())
		fs.PrintDefaults()
	}
	fs.Usage = usage

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Set the global flag.Usage to match
	flag.Usage = usage

	// Check TSBRIDGE_DEBUG environment variable if verbose not explicitly set
	if !result.verbose && os.Getenv("TSBRIDGE_DEBUG") != "" {
		result.verbose = true
	}

	return result, nil
}

// setupLogging configures the global logger based on the verbose flag
func setupLogging(verbose bool) {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	if verbose {
		opts.Level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, opts)))
}

// setupCommon configures logging and validates provider-specific flags
func setupCommon(args *cliArgs) error {
	// Configure logging
	setupLogging(args.verbose)

	// Validate provider-specific flags
	if args.provider == "file" && args.configPath == "" {
		return fmt.Errorf("-config flag is required for file provider")
	}
	return nil
}

// createProvider creates a configuration provider based on the CLI arguments
func createProvider(
	args *cliArgs,
	newDockerProvider func(docker.Options) (config.Provider, error),
) (config.Provider, error) {
	var (
		provider config.Provider
		err      error
	)

	switch args.provider {
	case "file":
		provider = config.NewFileProvider(args.configPath)
	case "docker":
		provider, err = newDockerProvider(docker.Options{
			DockerEndpoint: args.dockerEndpoint,
			LabelPrefix:    args.labelPrefix,
			PollInterval:   args.dockerPollInterval,
		})
	default:
		err = tserrors.NewValidationError("unknown provider type: " + args.provider)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create configuration provider: %w", err)
	}
	return provider, nil
}

func createDockerProvider(opts docker.Options) (config.Provider, error) {
	return docker.NewProvider(opts)
}

// validateConfig validates the configuration and returns an error if invalid
func validateConfig(args *cliArgs) error {
	// Perform common setup
	if err := setupCommon(args); err != nil {
		return err
	}

	slog.Debug("validating configuration", "provider", args.provider)

	// Create configuration provider
	configProvider, err := createProvider(args, createDockerProvider)
	if err != nil {
		return err
	}

	slog.Debug("loading configuration for validation", "provider", configProvider.Name())

	// Load the configuration
	cfg, err := configProvider.Load(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Validate the configuration
	if err := cfg.Validate(args.provider); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	slog.Info("configuration is valid")
	return nil
}

// Application interface for testing
type Application interface {
	Start(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

func runApplication(application Application, sigCh <-chan os.Signal) error {
	appErrCh := make(chan error, constants.DefaultChannelBufferSize)

	go func() {
		if err := application.Start(context.Background()); err != nil {
			appErrCh <- fmt.Errorf("failed to start application: %w", err)
		}
	}()

	select {
	case sig := <-sigCh:
		slog.Info("received signal, shutting down", "signal", sig)
	case err := <-appErrCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), constants.DefaultShutdownTimeout)
	defer cancel()

	if err := application.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}
	return nil
}

// run executes the main application logic
func run(args *cliArgs, sigCh <-chan os.Signal) error {

	if args.help {
		flag.Usage()
		return nil
	}

	if args.version {
		fmt.Printf("tsbridge version: %s\n", version)
		return nil
	}

	// Check if we're in validation mode
	if args.validate {
		return validateConfig(args)
	}

	// Perform common setup
	if err := setupCommon(args); err != nil {
		return err
	}

	slog.Info("starting tsbridge", "version", version, "provider", args.provider)

	// Create configuration provider
	configProvider, err := createProvider(args, createDockerProvider)
	if err != nil {
		return err
	}

	slog.Debug("loading configuration", "provider", configProvider.Name())

	// Create the application with the provider
	slog.Debug("creating application")
	application, err := app.NewAppWithOptions(nil, app.Options{
		Provider: configProvider,
	})
	if err != nil {
		return fmt.Errorf("failed to create application: %w", err)
	}

	return runApplication(application, sigCh)
}

func main() {
	args, err := parseCLIArgs(os.Args[1:])
	if err != nil {
		// Check if this is a help request
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		// Flag parsing errors already printed by flag package
		os.Exit(2)
	}

	// Setup signal handling for graceful shutdown
	sigCh := make(chan os.Signal, constants.DefaultChannelBufferSize)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	if err := run(args, sigCh); err != nil {
		slog.Error("error", "error", err)
		os.Exit(1)
	}

	os.Exit(0)
}
