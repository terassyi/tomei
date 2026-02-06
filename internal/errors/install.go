//nolint:revive // Package name intentionally shadows stdlib errors for convenience.
package errors

import "fmt"

// InstallError represents an installation-related error.
type InstallError struct {
	Base Error `json:"error"`

	// Resource is the resource being installed.
	Resource string `json:"resource,omitempty"`

	// Version is the version being installed.
	Version string `json:"version,omitempty"`

	// URL is the download URL (if applicable).
	URL string `json:"url,omitempty"`

	// Action is the operation being performed (install, upgrade, remove).
	Action string `json:"action,omitempty"`
}

// NewInstallError creates an InstallError.
func NewInstallError(resource, action string, cause error) *InstallError {
	return &InstallError{
		Base: Error{
			Category: CategoryInstall,
			Code:     CodeInstallFailed,
			Message:  fmt.Sprintf("%s failed", action),
			Cause:    cause,
		},
		Resource: resource,
		Action:   action,
	}
}

// WithVersion sets the version.
func (e *InstallError) WithVersion(version string) *InstallError {
	e.Version = version
	return e
}

// WithURL sets the URL.
func (e *InstallError) WithURL(url string) *InstallError {
	e.URL = url
	return e
}

// ChecksumError represents a checksum verification failure.
type ChecksumError struct {
	Base Error `json:"error"`

	// Resource is the resource being verified.
	Resource string `json:"resource,omitempty"`

	// URL is the download URL.
	URL string `json:"url,omitempty"`

	// Expected is the expected checksum.
	Expected string `json:"expected,omitempty"`

	// Got is the actual checksum.
	Got string `json:"got,omitempty"`
}

// NewChecksumError creates a ChecksumError.
func NewChecksumError(resource, url, expected, got string) *ChecksumError {
	return &ChecksumError{
		Base: Error{
			Category: CategoryInstall,
			Code:     CodeChecksumMismatch,
			Message:  "checksum verification failed",
			Hint:     "The file may have been corrupted during download.\nRun 'toto apply --force' to skip verification, or\nupdate the checksum in your manifest.",
		},
		Resource: resource,
		URL:      url,
		Expected: expected,
		Got:      got,
	}
}

// Error implements the error interface for InstallError.
func (e *InstallError) Error() string {
	return e.Base.Error()
}

// Unwrap returns the underlying error for InstallError.
func (e *InstallError) Unwrap() error {
	return e.Base.Cause
}

// Is reports whether the target error matches this error by code.
func (e *InstallError) Is(target error) bool {
	t, ok := target.(*InstallError)
	if !ok {
		return false
	}
	return e.Base.Code == t.Base.Code
}

// Error implements the error interface for ChecksumError.
func (e *ChecksumError) Error() string {
	return e.Base.Error()
}

// Unwrap returns the underlying error for ChecksumError.
func (e *ChecksumError) Unwrap() error {
	return e.Base.Cause
}

// Is reports whether the target error matches this error by code.
func (e *ChecksumError) Is(target error) bool {
	t, ok := target.(*ChecksumError)
	if !ok {
		return false
	}
	return e.Base.Code == t.Base.Code
}
