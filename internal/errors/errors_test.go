//nolint:revive // Package name intentionally shadows stdlib errors for convenience.
package errors

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      *Error
		expected string
	}{
		{
			name: "without cause",
			err: &Error{
				Category: CategoryDependency,
				Code:     CodeCyclicDependency,
				Message:  "circular dependency detected",
			},
			expected: "circular dependency detected",
		},
		{
			name: "with cause",
			err: &Error{
				Category: CategoryConfig,
				Code:     CodeConfigParse,
				Message:  "failed to parse config",
				Cause:    errors.New("invalid syntax"),
			},
			expected: "failed to parse config: invalid syntax",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func TestError_Unwrap(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying error")
	err := &Error{
		Category: CategoryInstall,
		Code:     CodeInstallFailed,
		Message:  "install failed",
		Cause:    cause,
	}

	assert.Equal(t, cause, err.Unwrap())
}

func TestError_WithMethods(t *testing.T) {
	t.Parallel()

	err := New(CategoryConfig, "test error")

	_ = err.WithHint("try this").
		WithExample("example: foo").
		WithDetail("key", "value")

	assert.Equal(t, "try this", err.Hint)
	assert.Equal(t, "example: foo", err.Example)
	assert.Equal(t, "value", err.Details["key"])
}

func TestDependencyError(t *testing.T) {
	t.Parallel()

	t.Run("cycle error", func(t *testing.T) {
		t.Parallel()

		cycle := []string{"A", "B", "C", "A"}
		err := NewCycleError(cycle)

		assert.True(t, err.IsCycle())
		assert.Equal(t, CodeCyclicDependency, err.Base.Code)
		assert.Equal(t, cycle, err.Cycle)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("missing dependency error", func(t *testing.T) {
		t.Parallel()

		err := NewMissingDependencyError("Tool/gopls", []string{"Runtime/go"})

		assert.False(t, err.IsCycle())
		assert.Equal(t, CodeMissingDependency, err.Base.Code)
		assert.Equal(t, "Tool/gopls", err.Resource)
		assert.Equal(t, []string{"Runtime/go"}, err.Missing)
	})

	t.Run("unwrap", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("original error")
		err := &DependencyError{
			Base: Error{
				Category: CategoryDependency,
				Code:     CodeCyclicDependency,
				Message:  "test",
				Cause:    cause,
			},
		}

		assert.Equal(t, cause, err.Unwrap())
	})
}

func TestConfigError(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("syntax error")
		err := NewConfigError("failed to load config", cause)

		assert.Equal(t, CodeConfigParse, err.Base.Code)
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("with location", func(t *testing.T) {
		t.Parallel()

		err := NewConfigErrorAt("config.cue", 10, 5, "invalid field", nil)

		assert.Equal(t, "config.cue", err.File)
		assert.Equal(t, 10, err.Line)
		assert.Equal(t, 5, err.Column)
	})

	t.Run("with methods", func(t *testing.T) {
		t.Parallel()

		err := NewConfigError("error", nil).
			WithFile("test.cue").
			WithLocation(15, 3).
			WithContext("  foo: bar")

		assert.Equal(t, "test.cue", err.File)
		assert.Equal(t, 15, err.Line)
		assert.Equal(t, 3, err.Column)
		assert.Equal(t, "  foo: bar", err.Context)
	})
}

func TestValidationError(t *testing.T) {
	t.Parallel()

	err := NewValidationError("Tool/rg", "version", "string", "number")

	assert.Equal(t, CodeValidationFailed, err.Base.Code)
	assert.Equal(t, "Tool/rg", err.Resource)
	assert.Equal(t, "version", err.Field)
	assert.Equal(t, "string", err.Expected)
	assert.Equal(t, "number", err.Got)
}

func TestInstallError(t *testing.T) {
	t.Parallel()

	cause := errors.New("download failed")
	err := NewInstallError("Tool/gh", "install", cause).
		WithVersion("2.86.0").
		WithURL("https://example.com/gh.tar.gz")

	assert.Equal(t, CodeInstallFailed, err.Base.Code)
	assert.Equal(t, "Tool/gh", err.Resource)
	assert.Equal(t, "install", err.Action)
	assert.Equal(t, "2.86.0", err.Version)
	assert.Equal(t, "https://example.com/gh.tar.gz", err.URL)
	assert.Equal(t, cause, err.Unwrap())
}

func TestChecksumError(t *testing.T) {
	t.Parallel()

	err := NewChecksumError("Tool/rg", "https://example.com/rg.tar.gz", "sha256:abc", "sha256:def")

	assert.Equal(t, CodeChecksumMismatch, err.Base.Code)
	assert.Equal(t, "Tool/rg", err.Resource)
	assert.Equal(t, "sha256:abc", err.Expected)
	assert.Equal(t, "sha256:def", err.Got)
	assert.NotEmpty(t, err.Base.Hint)
}

func TestNetworkError(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("connection refused")
		err := NewNetworkError("https://example.com", cause)

		assert.Equal(t, CodeNetworkFailed, err.Base.Code)
		assert.Equal(t, "https://example.com", err.URL)
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("HTTP error", func(t *testing.T) {
		t.Parallel()

		err := NewHTTPError("https://example.com/file.tar.gz", 404)

		assert.Equal(t, CodeHTTPError, err.Base.Code)
		assert.Equal(t, 404, err.StatusCode)
		assert.Contains(t, err.Error(), "404")
	})
}

func TestStateError(t *testing.T) {
	t.Parallel()

	t.Run("basic", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("file not found")
		err := NewStateError("failed to load state", cause)

		assert.Equal(t, CodeStateError, err.Base.Code)
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("lock error", func(t *testing.T) {
		t.Parallel()

		err := NewLockError("/tmp/state.lock", 12345)

		assert.Equal(t, CodeStateLocked, err.Base.Code)
		assert.Equal(t, "/tmp/state.lock", err.LockFile)
		assert.Equal(t, 12345, err.LockPID)
		assert.Contains(t, err.Base.Hint, "/tmp/state.lock")
	})
}

func TestRegistryError(t *testing.T) {
	t.Parallel()

	cause := errors.New("404 not found")
	err := NewRegistryError("aqua", "package not found", cause).
		WithPackage("cli/cli").
		WithVersion("2.86.0")

	assert.Equal(t, CodeRegistryError, err.Base.Code)
	assert.Equal(t, "aqua", err.Registry)
	assert.Equal(t, "cli/cli", err.Package)
	assert.Equal(t, "2.86.0", err.Version)
	assert.Equal(t, cause, err.Unwrap())
}

func TestErrorsIs(t *testing.T) {
	t.Parallel()

	t.Run("same code matches", func(t *testing.T) {
		t.Parallel()

		err1 := NewCycleError([]string{"A", "B", "A"})
		err2 := NewCycleError([]string{"X", "Y", "X"})

		assert.ErrorIs(t, err1, err2)
	})

	t.Run("different code does not match", func(t *testing.T) {
		t.Parallel()

		cycleErr := NewCycleError([]string{"A", "B", "A"})
		missingErr := NewMissingDependencyError("Tool/x", []string{"Runtime/y"})

		assert.NotErrorIs(t, cycleErr, missingErr)
	})

	t.Run("different types do not match", func(t *testing.T) {
		t.Parallel()

		depErr := NewCycleError([]string{"A", "B", "A"})
		configErr := NewConfigError("test", nil)

		assert.NotErrorIs(t, depErr, configErr)
	})

	t.Run("base error Is", func(t *testing.T) {
		t.Parallel()

		err1 := &Error{Code: CodeInstallFailed, Message: "install failed"}
		err2 := &Error{Code: CodeInstallFailed, Message: "different message"}

		assert.ErrorIs(t, err1, err2)
	})

	t.Run("network error codes", func(t *testing.T) {
		t.Parallel()

		err1 := NewHTTPError("https://a.com", 404)
		err2 := NewHTTPError("https://b.com", 500)

		// Same code (CodeHTTPError)
		assert.ErrorIs(t, err1, err2)
	})

	t.Run("network vs install does not match", func(t *testing.T) {
		t.Parallel()

		netErr := NewNetworkError("https://example.com", nil)
		installErr := NewInstallError("Tool/x", "install", nil)

		assert.NotErrorIs(t, netErr, installErr)
	})
}

func TestErrorsAs(t *testing.T) {
	t.Parallel()

	// Test that errors.As works correctly with our error types
	t.Run("DependencyError", func(t *testing.T) {
		t.Parallel()

		var err error = NewCycleError([]string{"A", "B", "A"})

		var depErr *DependencyError
		require.ErrorAs(t, err, &depErr)
		assert.True(t, depErr.IsCycle())
	})

	t.Run("ConfigError", func(t *testing.T) {
		t.Parallel()

		var err error = NewConfigError("test", nil)

		var configErr *ConfigError
		require.ErrorAs(t, err, &configErr)
		assert.Equal(t, CodeConfigParse, configErr.Base.Code)
	})

	t.Run("wrapped error", func(t *testing.T) {
		t.Parallel()

		original := NewInstallError("Tool/gh", "install", nil)
		wrapped := Wrap(CategoryInstall, "operation failed", original)

		var installErr *InstallError
		require.ErrorAs(t, wrapped, &installErr)
		assert.Equal(t, "Tool/gh", installErr.Resource)
	})
}
