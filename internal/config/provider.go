// Package config handles configuration parsing and validation for tsbridge.
package config

import "context"

// Provider defines the interface for configuration providers
type Provider interface {
	// Load retrieves the configuration from the provider
	Load(ctx context.Context) (*Config, error)

	// Watch returns a channel that emits new configurations when they change.
	// The channel is closed when the context is cancelled.
	// Returns nil if the provider does not support watching.
	Watch(ctx context.Context) (<-chan *Config, error)

	// Name returns the provider name for logging purposes
	Name() string
}

// FileProvider implements Provider for file-based configuration
type FileProvider struct {
	path string
}

// NewFileProvider creates a new file-based configuration provider
func NewFileProvider(path string) *FileProvider {
	return &FileProvider{path: path}
}

// Load reads and parses the configuration from the file
func (p *FileProvider) Load(ctx context.Context) (*Config, error) {
	return LoadWithProvider(p.path, "file")
}

// Watch is not implemented for file provider - returns nil
func (p *FileProvider) Watch(ctx context.Context) (<-chan *Config, error) {
	// File watching could be implemented in the future using fsnotify
	return nil, nil
}

// Name returns the provider name
func (p *FileProvider) Name() string {
	return "file"
}
