// Package errors provides structured error types for tomei.
// These errors carry rich context information that can be formatted
// for human-readable CLI output or machine-readable JSON.
//
//nolint:revive // Package name intentionally shadows stdlib errors for convenience.
package errors

// Category represents the classification of an error.
type Category string

const (
	CategoryConfig     Category = "config"
	CategoryValidation Category = "validation"
	CategoryDependency Category = "dependency"
	CategoryInstall    Category = "install"
	CategoryState      Category = "state"
	CategoryRegistry   Category = "registry"
	CategoryNetwork    Category = "network"
)

// Code represents a machine-readable error code.
type Code string

const (
	// Dependency errors (E1xx)
	CodeCyclicDependency  Code = "E101"
	CodeMissingDependency Code = "E102"

	// Config errors (E2xx)
	CodeConfigParse      Code = "E201"
	CodeValidationFailed Code = "E202"

	// Install errors (E3xx)
	CodeInstallFailed    Code = "E301"
	CodeChecksumMismatch Code = "E302"

	// Network errors (E4xx)
	CodeNetworkFailed Code = "E401"
	CodeHTTPError     Code = "E402"

	// State errors (E5xx)
	CodeStateError  Code = "E501"
	CodeStateLocked Code = "E502"

	// Registry errors (E6xx)
	CodeRegistryError Code = "E601"
)

// Error is the base error type for tomei.
// It provides structured information that can be formatted for CLI output.
type Error struct {
	// Category classifies the error type.
	Category Category `json:"category"`

	// Code is a machine-readable error code.
	Code Code `json:"code,omitempty"`

	// Message is a short description of the error.
	Message string `json:"message"`

	// Details contains additional context information.
	Details map[string]any `json:"details,omitempty"`

	// Hint provides actionable advice for the user.
	Hint string `json:"hint,omitempty"`

	// Example shows a CUE code example (when applicable).
	Example string `json:"example,omitempty"`

	// Cause is the underlying error.
	Cause error `json:"-"`
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return e.Message + ": " + e.Cause.Error()
	}
	return e.Message
}

// Unwrap returns the underlying error.
func (e *Error) Unwrap() error {
	return e.Cause
}

// Is reports whether the target error matches this error.
// It matches if the target is an *Error with the same Code (if both have codes).
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	// If both have codes, compare by code
	if e.Code != "" && t.Code != "" {
		return e.Code == t.Code
	}
	// Otherwise compare by category and message
	return e.Category == t.Category && e.Message == t.Message
}

// WithHint sets the hint and returns the error for chaining.
func (e *Error) WithHint(hint string) *Error {
	e.Hint = hint
	return e
}

// WithExample sets the example and returns the error for chaining.
func (e *Error) WithExample(example string) *Error {
	e.Example = example
	return e
}

// WithDetail adds a detail and returns the error for chaining.
func (e *Error) WithDetail(key string, value any) *Error {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details[key] = value
	return e
}

// New creates a new Error with the given category and message.
func New(category Category, message string) *Error {
	return &Error{
		Category: category,
		Message:  message,
	}
}

// Wrap creates a new Error wrapping an existing error.
func Wrap(category Category, message string, cause error) *Error {
	return &Error{
		Category: category,
		Message:  message,
		Cause:    cause,
	}
}
