package state

import "fmt"

// ValidationError represents a single validation issue.
type ValidationError struct {
	Field   string // e.g., "version", "tools.gh.version"
	Message string
}

func (e ValidationError) String() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ValidationResult holds the result of state validation.
type ValidationResult struct {
	Errors   []ValidationError // fatal issues that should prevent loading
	Warnings []ValidationError // non-fatal issues logged as warnings
}

// IsValid returns true if there are no fatal validation errors.
func (r *ValidationResult) IsValid() bool {
	return len(r.Errors) == 0
}

// HasWarnings returns true if there are any validation warnings.
func (r *ValidationResult) HasWarnings() bool {
	return len(r.Warnings) > 0
}

func (r *ValidationResult) warn(field, message string) {
	r.Warnings = append(r.Warnings, ValidationError{Field: field, Message: message})
}

// validateVersion checks the state file format version.
func (r *ValidationResult) validateVersion(version string) {
	if version == "" {
		r.warn("version", "version is empty")
	} else if version != Version {
		r.warn("version", fmt.Sprintf("unknown version %q (expected %q)", version, Version))
	}
}

// ValidateUserState validates a UserState for integrity.
func ValidateUserState(st *UserState) *ValidationResult {
	result := &ValidationResult{}

	result.validateVersion(st.Version)

	for name, tool := range st.Tools {
		if tool.Version == "" {
			result.warn(fmt.Sprintf("tools.%s.version", name), "version is empty")
		}
	}

	for name, rt := range st.Runtimes {
		if rt.Version == "" {
			result.warn(fmt.Sprintf("runtimes.%s.version", name), "version is empty")
		}
		if rt.InstallPath == "" {
			result.warn(fmt.Sprintf("runtimes.%s.installPath", name), "installPath is empty")
		}
	}

	return result
}

// ValidateSystemState validates a SystemState for integrity.
func ValidateSystemState(st *SystemState) *ValidationResult {
	result := &ValidationResult{}
	result.validateVersion(st.Version)
	return result
}
