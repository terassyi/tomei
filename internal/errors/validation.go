//nolint:revive // Package name intentionally shadows stdlib errors for convenience.
package errors

import "fmt"

// ValidationError represents a resource validation error.
type ValidationError struct {
	Base Error `json:"error"`

	// Resource is the resource that failed validation.
	Resource string `json:"resource,omitempty"`

	// Field is the field that failed validation.
	Field string `json:"field,omitempty"`

	// Expected describes what was expected.
	Expected string `json:"expected,omitempty"`

	// Got describes what was received.
	Got string `json:"got,omitempty"`
}

// NewValidationError creates a ValidationError.
func NewValidationError(resource, field, expected, got string) *ValidationError {
	return &ValidationError{
		Base: Error{
			Category: CategoryValidation,
			Code:     CodeValidationFailed,
			Message:  fmt.Sprintf("validation failed for %s", resource),
		},
		Resource: resource,
		Field:    field,
		Expected: expected,
		Got:      got,
	}
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return e.Base.Error()
}

// Unwrap returns the underlying error.
func (e *ValidationError) Unwrap() error {
	return e.Base.Cause
}

// Is reports whether the target error matches this error by code.
func (e *ValidationError) Is(target error) bool {
	t, ok := target.(*ValidationError)
	if !ok {
		return false
	}
	return e.Base.Code == t.Base.Code
}
