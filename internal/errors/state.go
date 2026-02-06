//nolint:revive // Package name intentionally shadows stdlib errors for convenience.
package errors

import "fmt"

// StateError represents a state management error.
type StateError struct {
	Base Error `json:"error"`

	// LockPID is the PID of the process holding the lock (if applicable).
	LockPID int `json:"lockPid,omitempty"`

	// LockFile is the path to the lock file.
	LockFile string `json:"lockFile,omitempty"`
}

// NewStateError creates a StateError.
func NewStateError(message string, cause error) *StateError {
	return &StateError{
		Base: Error{
			Category: CategoryState,
			Code:     CodeStateError,
			Message:  message,
			Cause:    cause,
		},
	}
}

// NewLockError creates a StateError for lock conflicts.
func NewLockError(lockFile string, lockPID int) *StateError {
	hint := fmt.Sprintf("Wait for the other process to finish, or\nrun 'rm %s' if it's stale.", lockFile)
	return &StateError{
		Base: Error{
			Category: CategoryState,
			Code:     CodeStateLocked,
			Message:  "state locked",
			Hint:     hint,
		},
		LockPID:  lockPID,
		LockFile: lockFile,
	}
}

// RegistryError represents a registry-related error.
type RegistryError struct {
	Base Error `json:"error"`

	// Registry is the registry name (e.g., "aqua").
	Registry string `json:"registry,omitempty"`

	// Package is the package name (if applicable).
	Package string `json:"package,omitempty"`

	// Version is the version (if applicable).
	Version string `json:"version,omitempty"`
}

// NewRegistryError creates a RegistryError.
func NewRegistryError(registry, message string, cause error) *RegistryError {
	return &RegistryError{
		Base: Error{
			Category: CategoryRegistry,
			Code:     CodeRegistryError,
			Message:  message,
			Cause:    cause,
		},
		Registry: registry,
	}
}

// WithPackage sets the package name.
func (e *RegistryError) WithPackage(pkg string) *RegistryError {
	e.Package = pkg
	return e
}

// WithVersion sets the version.
func (e *RegistryError) WithVersion(version string) *RegistryError {
	e.Version = version
	return e
}

// Error implements the error interface for StateError.
func (e *StateError) Error() string {
	return e.Base.Error()
}

// Unwrap returns the underlying error for StateError.
func (e *StateError) Unwrap() error {
	return e.Base.Cause
}

// Is reports whether the target error matches this error by code.
func (e *StateError) Is(target error) bool {
	t, ok := target.(*StateError)
	if !ok {
		return false
	}
	return e.Base.Code == t.Base.Code
}

// Error implements the error interface for RegistryError.
func (e *RegistryError) Error() string {
	return e.Base.Error()
}

// Unwrap returns the underlying error for RegistryError.
func (e *RegistryError) Unwrap() error {
	return e.Base.Cause
}

// Is reports whether the target error matches this error by code.
func (e *RegistryError) Is(target error) bool {
	t, ok := target.(*RegistryError)
	if !ok {
		return false
	}
	return e.Base.Code == t.Base.Code
}
