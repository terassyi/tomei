//nolint:revive // Package name intentionally shadows stdlib errors for convenience.
package errors

import "fmt"

// NetworkError represents a network-related error.
type NetworkError struct {
	Base Error `json:"error"`

	// URL is the URL that failed.
	URL string `json:"url,omitempty"`

	// StatusCode is the HTTP status code (if applicable).
	StatusCode int `json:"statusCode,omitempty"`
}

// NewNetworkError creates a NetworkError.
func NewNetworkError(url string, cause error) *NetworkError {
	return &NetworkError{
		Base: Error{
			Category: CategoryNetwork,
			Code:     CodeNetworkFailed,
			Message:  "network request failed",
			Cause:    cause,
		},
		URL: url,
	}
}

// NewHTTPError creates a NetworkError for HTTP errors.
func NewHTTPError(url string, statusCode int) *NetworkError {
	return &NetworkError{
		Base: Error{
			Category: CategoryNetwork,
			Code:     CodeHTTPError,
			Message:  fmt.Sprintf("HTTP %d", statusCode),
		},
		URL:        url,
		StatusCode: statusCode,
	}
}

// Error implements the error interface.
func (e *NetworkError) Error() string {
	return e.Base.Error()
}

// Unwrap returns the underlying error.
func (e *NetworkError) Unwrap() error {
	return e.Base.Cause
}

// Is reports whether the target error matches this error by code.
func (e *NetworkError) Is(target error) bool {
	t, ok := target.(*NetworkError)
	if !ok {
		return false
	}
	return e.Base.Code == t.Base.Code
}
