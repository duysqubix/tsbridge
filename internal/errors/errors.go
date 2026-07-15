// Package errors provides standardized error types and handling for tsbridge.
// It implements error classification, wrapping, and utility functions for
// consistent error handling across the codebase.
package errors

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrorType represents the category of error
type ErrorType string

const (
	// ErrTypeValidation is for input validation errors
	ErrTypeValidation ErrorType = "validation"

	// ErrTypeNetwork is for network-related errors (connection, timeout, etc)
	ErrTypeNetwork ErrorType = "network"

	// ErrTypeConfig is for configuration errors
	ErrTypeConfig ErrorType = "config"

	// ErrTypeResource is for resource availability errors (ports, files, etc)
	ErrTypeResource ErrorType = "resource"

	// errTypeInternal is for internal/unexpected errors
	errTypeInternal ErrorType = "internal"
)

// Error is the standard error type with classification
type Error struct {
	Type           ErrorType
	Message        string
	Err            error
	HTTPStatusCode int // Optional HTTP status code for network errors
}

// Error implements the error interface
func (e *Error) Error() string {
	typeStr := string(e.Type)
	if e.Type == ErrTypeConfig {
		typeStr = "configuration"
	}

	if e.Err != nil {
		return fmt.Sprintf("%s error: %s: %v", typeStr, e.Message, e.Err)
	}
	return fmt.Sprintf("%s error: %s", typeStr, e.Message)
}

// Unwrap allows errors.Is and errors.As to work
func (e *Error) Unwrap() error {
	return e.Err
}

// NewValidationError creates a new validation error
func NewValidationError(message string) error {
	return &Error{Type: ErrTypeValidation, Message: message}
}

// NewNetworkErrorWithStatus creates a new network error with HTTP status code
func NewNetworkErrorWithStatus(message string, statusCode int) error {
	return &Error{Type: ErrTypeNetwork, Message: message, HTTPStatusCode: statusCode}
}

// NewConfigError creates a new configuration error
func NewConfigError(message string) error {
	return &Error{Type: ErrTypeConfig, Message: message}
}

// NewResourceError creates a new resource error
func NewResourceError(message string) error {
	return &Error{Type: ErrTypeResource, Message: message}
}

// WrapValidation wraps an error as a validation error
func WrapValidation(err error, message string) error {
	return &Error{Type: ErrTypeValidation, Message: message, Err: err}
}

// WrapNetwork wraps an error as a network error
func WrapNetwork(err error, message string) error {
	return &Error{Type: ErrTypeNetwork, Message: message, Err: err}
}

// WrapConfig wraps an error as a configuration error
func WrapConfig(err error, message string) error {
	return &Error{Type: ErrTypeConfig, Message: message, Err: err}
}

// WrapResource wraps an error as a resource error
func WrapResource(err error, message string) error {
	return &Error{Type: ErrTypeResource, Message: message, Err: err}
}

// WrapInternal wraps an error as an internal error
func WrapInternal(err error, message string) error {
	return &Error{Type: errTypeInternal, Message: message, Err: err}
}

// IsNetwork checks if an error is a network error.
func IsNetwork(err error) bool {
	var typedErr *Error
	return errors.As(err, &typedErr) && typedErr.Type == ErrTypeNetwork
}

// ServiceStartupError represents the result of attempting to start multiple services
type ServiceStartupError struct {
	Total      int              // Total number of services attempted
	Successful int              // Number of services that started successfully
	Failed     int              // Number of services that failed to start
	Failures   map[string]error // Map of service name to error for failed services
}

// Error implements the error interface
func (e *ServiceStartupError) Error() string {
	var msg strings.Builder
	if e.Failed == e.Total {
		fmt.Fprintf(&msg, "all %d services failed to start:", e.Total)
	} else {
		fmt.Fprintf(&msg, "%d of %d services failed to start:", e.Failed, e.Total)
	}
	for service, err := range e.Failures {
		fmt.Fprintf(&msg, "\n  - %s: %v", service, err)
	}
	return msg.String()
}

// AllFailed returns true if all services failed to start
func (e *ServiceStartupError) AllFailed() bool {
	return e.Failed == e.Total && e.Total > 0
}

// NewServiceStartupError creates a new service startup error if there were any failures
func NewServiceStartupError(total, successful, failed int, failures map[string]error) error {
	if failed == 0 || len(failures) == 0 {
		return nil
	}

	return &Error{
		Type:    errTypeInternal,
		Message: "service startup",
		Err: &ServiceStartupError{
			Total:      total,
			Successful: successful,
			Failed:     failed,
			Failures:   failures,
		},
	}
}

// AsServiceStartupError checks if an error is a ServiceStartupError and returns it
func AsServiceStartupError(err error) (*ServiceStartupError, bool) {
	var startupErr *ServiceStartupError
	ok := errors.As(err, &startupErr)
	return startupErr, ok
}

// providerError represents an error from a configuration provider.
// It includes the provider name for context in error messages.
type providerError struct {
	Provider string
	Message  string
	Cause    error
}

// Error implements the error interface
func (e *providerError) Error() string {
	if e.Cause != nil {
		return e.Provider + " provider: " + e.Message + ": " + e.Cause.Error()
	}
	return e.Provider + " provider: " + e.Message
}

// Unwrap returns the underlying error
func (e *providerError) Unwrap() error {
	return e.Cause
}

// NewProviderError creates a new provider error without a cause
func NewProviderError(provider string, errType ErrorType, message string) error {
	return &Error{
		Type: errType,
		Err: &providerError{
			Provider: provider,
			Message:  message,
		},
	}
}

// WrapProviderError wraps an error with provider context
func WrapProviderError(err error, provider string, errType ErrorType, operation string) error {
	if err == nil {
		return nil
	}
	return &Error{
		Type: errType,
		Err: &providerError{
			Provider: provider,
			Message:  operation,
			Cause:    err,
		},
	}
}

// timeoutError represents a timeout during an operation
type timeoutError struct {
	Operation string
	Timeout   time.Duration
	Cause     error
}

// Error implements the error interface
func (e *timeoutError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s timed out after %v: %v", e.Operation, e.Timeout, e.Cause)
	}
	return fmt.Sprintf("%s timed out after %v", e.Operation, e.Timeout)
}

// Unwrap returns the underlying error
func (e *timeoutError) Unwrap() error {
	return e.Cause
}

// NewTimeoutError creates a new timeout error
func NewTimeoutError(operation string, timeout time.Duration) error {
	return &Error{
		Type:    ErrTypeResource,
		Message: "operation timeout",
		Err: &timeoutError{
			Operation: operation,
			Timeout:   timeout,
		},
	}
}

// IsTimeout checks if an error is a timeout error
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}
	var timeoutErr *timeoutError
	return errors.As(err, &timeoutErr)
}
