//nolint:revive // Package name intentionally shadows stdlib errors for convenience.
package errors

import (
	"fmt"
	"strings"
)

// DependencyError represents a dependency resolution error.
type DependencyError struct {
	Base Error `json:"error"`

	// Resource is the resource that has the dependency issue.
	Resource string `json:"resource,omitempty"`

	// Missing lists unresolved dependencies.
	Missing []string `json:"missing,omitempty"`

	// Cycle lists the nodes in a circular dependency.
	// The first and last elements are the same, showing the cycle point.
	Cycle []string `json:"cycle,omitempty"`
}

// NewCycleError creates a DependencyError for circular dependencies.
func NewCycleError(cycle []string) *DependencyError {
	return &DependencyError{
		Base: Error{
			Category: CategoryDependency,
			Code:     CodeCyclicDependency,
			Message:  "circular dependency detected",
			Hint:     "Remove one of the dependencies to break the cycle.",
		},
		Cycle: cycle,
	}
}

// NewMissingDependencyError creates a DependencyError for unresolved dependencies.
func NewMissingDependencyError(resource string, missing []string) *DependencyError {
	hint := fmt.Sprintf("Add the missing resource(s) to your manifest: %s", strings.Join(missing, ", "))
	return &DependencyError{
		Base: Error{
			Category: CategoryDependency,
			Code:     CodeMissingDependency,
			Message:  "unresolved dependency",
			Hint:     hint,
		},
		Resource: resource,
		Missing:  missing,
	}
}

// IsCycle returns true if this is a circular dependency error.
func (e *DependencyError) IsCycle() bool {
	return len(e.Cycle) > 0
}

// Error implements the error interface.
func (e *DependencyError) Error() string {
	return e.Base.Error()
}

// Unwrap returns the underlying error.
func (e *DependencyError) Unwrap() error {
	return e.Base.Cause
}

// Is reports whether the target error matches this error by code.
func (e *DependencyError) Is(target error) bool {
	t, ok := target.(*DependencyError)
	if !ok {
		return false
	}
	return e.Base.Code == t.Base.Code
}
