package errors

import (
	"errors"
	"strings"
	"testing"
)

func TestErrorTypes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantType ErrorType
		wantMsg  string
	}{
		{
			name:     "validation error",
			err:      NewValidationError("invalid configuration"),
			wantType: ErrTypeValidation,
			wantMsg:  "validation error: invalid configuration",
		},
		{
			name:     "configuration error",
			err:      NewConfigError("missing required field"),
			wantType: ErrTypeConfig,
			wantMsg:  "configuration error: missing required field",
		},
		{
			name:     "resource error",
			err:      NewResourceError("port already in use"),
			wantType: ErrTypeResource,
			wantMsg:  "resource error: port already in use",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check error message
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}

			// Check error type
			var typed *Error
			if !errors.As(tt.err, &typed) {
				t.Fatal("error should be of type *Error")
			}
			if typed.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", typed.Type, tt.wantType)
			}
		})
	}
}

func TestWrap(t *testing.T) {
	baseErr := errors.New("base error")

	tests := []struct {
		name     string
		err      error
		wantType ErrorType
		wantMsg  string
	}{
		{
			name:     "wrap as validation error",
			err:      WrapValidation(baseErr, "invalid input"),
			wantType: ErrTypeValidation,
			wantMsg:  "validation error: invalid input: base error",
		},
		{
			name:     "wrap as network error",
			err:      WrapNetwork(baseErr, "connection failed"),
			wantType: ErrTypeNetwork,
			wantMsg:  "network error: connection failed: base error",
		},
		{
			name:     "wrap as config error",
			err:      WrapConfig(baseErr, "config parse failed"),
			wantType: ErrTypeConfig,
			wantMsg:  "configuration error: config parse failed: base error",
		},
		{
			name:     "wrap as resource error",
			err:      WrapResource(baseErr, "resource unavailable"),
			wantType: ErrTypeResource,
			wantMsg:  "resource error: resource unavailable: base error",
		},
		{
			name:     "wrap as internal error",
			err:      WrapInternal(baseErr, "unexpected failure"),
			wantType: errTypeInternal,
			wantMsg:  "internal error: unexpected failure: base error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Check error message
			if got := tt.err.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}

			// Check error type
			var typed *Error
			if !errors.As(tt.err, &typed) {
				t.Fatal("error should be of type *Error")
			}
			if typed.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", typed.Type, tt.wantType)
			}

			// Check that original error is preserved
			if !errors.Is(tt.err, baseErr) {
				t.Error("wrapped error should preserve original error")
			}
		})
	}
}

func TestServiceStartupError(t *testing.T) {
	t.Run("all services failed", func(t *testing.T) {
		failures := map[string]error{
			"service1": errors.New("connection refused"),
			"service2": errors.New("port already in use"),
			"service3": errors.New("invalid config"),
		}

		err := &ServiceStartupError{
			Total:      3,
			Successful: 0,
			Failed:     3,
			Failures:   failures,
		}

		// Verify error message
		msg := err.Error()
		if !strings.Contains(msg, "all 3 services failed") {
			t.Errorf("expected error message to indicate all services failed, got: %s", msg)
		}

		// Should include details about each failure
		for service, failure := range failures {
			if !strings.Contains(msg, service) {
				t.Errorf("expected error message to include service name %s, got: %s", service, msg)
			}
			if !strings.Contains(msg, failure.Error()) {
				t.Errorf("expected error message to include failure reason %s, got: %s", failure.Error(), msg)
			}
		}

		// Should be considered a total failure
		if !err.AllFailed() {
			t.Error("expected AllFailed() to return true when all services failed")
		}
	})

	t.Run("partial failure", func(t *testing.T) {
		failures := map[string]error{
			"service2": errors.New("backend unreachable"),
		}

		err := &ServiceStartupError{
			Total:      3,
			Successful: 2,
			Failed:     1,
			Failures:   failures,
		}

		// Verify error message
		msg := err.Error()
		if !strings.Contains(msg, "1 of 3 services failed") {
			t.Errorf("expected error message to indicate partial failure, got: %s", msg)
		}

		// Should include the failed service
		if !strings.Contains(msg, "service2") {
			t.Errorf("expected error message to include failed service name, got: %s", msg)
		}

		// Should NOT be considered a total failure
		if err.AllFailed() {
			t.Error("expected AllFailed() to return false when some services succeeded")
		}
	})

	t.Run("no failures returns nil", func(t *testing.T) {
		// When creating with no failures, should return nil
		err := NewServiceStartupError(5, 5, 0, nil)
		if err != nil {
			t.Errorf("expected nil when no services failed, got: %v", err)
		}
	})

	t.Run("empty failures map", func(t *testing.T) {
		err := &ServiceStartupError{
			Total:      2,
			Successful: 2,
			Failed:     0,
			Failures:   map[string]error{},
		}

		// Even with empty map, should indicate success
		if err.AllFailed() {
			t.Error("expected AllFailed() to return false with empty failures map")
		}
	})

	t.Run("error type checking", func(t *testing.T) {
		failures := map[string]error{
			"service1": errors.New("failed"),
		}

		err := NewServiceStartupError(1, 0, 1, failures)

		// Should be unwrappable to get the actual error
		var startupErr *ServiceStartupError
		if !errors.As(err, &startupErr) {
			t.Error("expected to be able to unwrap to ServiceStartupError")
		}
	})

	t.Run("constructor validation", func(t *testing.T) {
		// Test with valid partial failure
		err := NewServiceStartupError(3, 2, 1, map[string]error{
			"failed": errors.New("error"),
		})
		if err == nil {
			t.Error("expected error for partial failure")
		}

		// Test with all failed
		err = NewServiceStartupError(2, 0, 2, map[string]error{
			"svc1": errors.New("error1"),
			"svc2": errors.New("error2"),
		})
		if err == nil {
			t.Error("expected error when all services failed")
		}
	})

	t.Run("AsServiceStartupError helper", func(t *testing.T) {
		// Test with actual ServiceStartupError
		err := NewServiceStartupError(3, 1, 2, map[string]error{
			"svc1": errors.New("failed1"),
			"svc2": errors.New("failed2"),
		})

		startupErr, ok := AsServiceStartupError(err)
		if !ok {
			t.Error("expected AsServiceStartupError to return true for ServiceStartupError")
			return
		}
		if startupErr == nil {
			t.Error("expected non-nil ServiceStartupError")
			return
		}
		if startupErr.Total != 3 || startupErr.Failed != 2 {
			t.Errorf("unexpected values: total=%d, failed=%d", startupErr.Total, startupErr.Failed)
		}

		// Test with non-ServiceStartupError
		regularErr := errors.New("regular error")
		_, ok = AsServiceStartupError(regularErr)
		if ok {
			t.Error("expected AsServiceStartupError to return false for regular error")
		}

		// Test with nil
		_, ok = AsServiceStartupError(nil)
		if ok {
			t.Error("expected AsServiceStartupError to return false for nil")
		}
	})
}

func TestProviderErrorWrapping(t *testing.T) {
	tests := []struct {
		name         string
		provider     string
		operation    string
		baseErr      error
		wrapFunc     func(error, string, string) error
		wantContains []string
	}{
		{
			name:      "file provider config error",
			provider:  "file",
			operation: "loading config",
			baseErr:   errors.New("file not found"),
			wrapFunc: func(err error, provider, operation string) error {
				return WrapProviderError(err, provider, ErrTypeConfig, operation)
			},
			wantContains: []string{"file provider", "loading config", "file not found"},
		},
		{
			name:      "docker provider resource error",
			provider:  "docker",
			operation: "connecting to Docker",
			baseErr:   errors.New("connection refused"),
			wrapFunc: func(err error, provider, operation string) error {
				return WrapProviderError(err, provider, ErrTypeResource, operation)
			},
			wantContains: []string{"docker provider", "connecting to Docker", "connection refused"},
		},
		{
			name:      "validation error with provider context",
			provider:  "docker",
			operation: "parsing service config",
			baseErr:   errors.New("invalid backend address"),
			wrapFunc: func(err error, provider, operation string) error {
				return WrapProviderError(err, provider, ErrTypeValidation, operation)
			},
			wantContains: []string{"docker provider", "parsing service config", "invalid backend address"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.wrapFunc(tt.baseErr, tt.provider, tt.operation)

			// Check error message contains expected strings
			errMsg := err.Error()
			for _, want := range tt.wantContains {
				if !strings.Contains(errMsg, want) {
					t.Errorf("error message %q does not contain %q", errMsg, want)
				}
			}
		})
	}
}

func TestNewProviderError(t *testing.T) {
	tests := []struct {
		name      string
		provider  string
		errType   ErrorType
		message   string
		wantError string
	}{
		{
			name:      "file provider config error",
			provider:  "file",
			errType:   ErrTypeConfig,
			message:   "invalid TOML syntax",
			wantError: "configuration error: : file provider: invalid TOML syntax",
		},
		{
			name:      "docker provider validation error",
			provider:  "docker",
			errType:   ErrTypeValidation,
			message:   "missing required label",
			wantError: "validation error: : docker provider: missing required label",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := NewProviderError(tt.provider, tt.errType, tt.message)

			if err.Error() != tt.wantError {
				t.Errorf("error = %q, want %q", err.Error(), tt.wantError)
			}

		})
	}
}
