package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jtdowney/tsbridge/internal/errors"
)

// validateFilePath validates that a file path is safe to use and doesn't contain
// directory traversal attempts or other security issues
func validateFilePath(path string) error {
	if path == "" {
		return errors.NewValidationError("empty file path")
	}

	// Path must be absolute
	if !filepath.IsAbs(path) {
		return errors.NewValidationError("file path must be absolute")
	}

	// Check for null bytes
	if strings.Contains(path, "\x00") {
		return errors.NewValidationError("invalid file path: contains null bytes")
	}

	// Check for directory traversal attempts BEFORE cleaning
	// This is important because filepath.Clean would resolve .. components
	if strings.Contains(path, "..") {
		return errors.NewValidationError("invalid file path: contains directory traversal")
	}

	// Additional safety: ensure no path components are . or ..
	parts := strings.SplitSeq(path, string(filepath.Separator))
	for part := range parts {
		if part == ".." || part == "." {
			return errors.NewValidationError("invalid file path: contains directory traversal")
		}
	}

	return nil
}

// ResolveSecret resolves a secret value from either an environment variable or file.
// Priority order:
// 1. Direct value (if not empty)
// 2. File content (if filePath specified)
// 3. Environment variable (if envVar specified)
// Returns empty string if no sources are configured or have values.
func ResolveSecret(value, envVar, filePath string) (string, error) {
	// Priority 1: Direct value
	if value != "" {
		return value, nil
	}

	// Priority 2: File
	if filePath != "" {
		// Validate the file path for security
		if err := validateFilePath(filePath); err != nil {
			return "", err
		}

		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("reading secret file: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}

	// Priority 3: Environment variable
	if envVar != "" {
		value := os.Getenv(envVar)
		if value == "" {
			return "", fmt.Errorf("environment variable %s is not set or empty", envVar)
		}
		return value, nil
	}

	// No sources configured
	return "", nil
}

// ResolveSecretWithFallback resolves a secret value with an additional fallback environment variable.
// Priority order:
// 1. Direct value (if not empty)
// 2. File content (if filePath specified)
// 3. Environment variable (if envVar specified)
// 4. Fallback environment variable (if fallbackEnv specified)
// Returns empty string if no sources are configured or have values.
// Returns error if a configured source (file or env var) exists but cannot be accessed.
func ResolveSecretWithFallback(value, envVar, filePath, fallbackEnv string) (string, error) {
	// Try the primary sources first
	result, err := ResolveSecret(value, envVar, filePath)
	if err != nil {
		// If we have a fallback, try it before returning the error
		if fallbackEnv != "" {
			if fallbackValue := os.Getenv(fallbackEnv); fallbackValue != "" {
				return fallbackValue, nil
			}
		}
		// No fallback or fallback is empty, return the original error
		return "", err
	}
	if result != "" {
		return result, nil
	}

	// Try fallback environment variable
	if fallbackEnv != "" {
		return os.Getenv(fallbackEnv), nil
	}

	return "", nil
}
