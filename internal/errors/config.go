//nolint:revive // Package name intentionally shadows stdlib errors for convenience.
package errors

// ConfigError represents a configuration loading or parsing error.
type ConfigError struct {
	Base Error `json:"error"`

	// File is the path to the configuration file.
	File string `json:"file,omitempty"`

	// Line is the line number where the error occurred.
	Line int `json:"line,omitempty"`

	// Column is the column number where the error occurred.
	Column int `json:"column,omitempty"`

	// Context contains surrounding lines of code for display.
	Context string `json:"context,omitempty"`
}

// NewConfigError creates a ConfigError.
func NewConfigError(message string, cause error) *ConfigError {
	return &ConfigError{
		Base: Error{
			Category: CategoryConfig,
			Code:     CodeConfigParse,
			Message:  message,
			Cause:    cause,
		},
	}
}

// NewConfigErrorAt creates a ConfigError with file location information.
func NewConfigErrorAt(file string, line, column int, message string, cause error) *ConfigError {
	return &ConfigError{
		Base: Error{
			Category: CategoryConfig,
			Code:     CodeConfigParse,
			Message:  message,
			Cause:    cause,
		},
		File:   file,
		Line:   line,
		Column: column,
	}
}

// WithContext sets the surrounding code context.
func (e *ConfigError) WithContext(context string) *ConfigError {
	e.Context = context
	return e
}

// WithFile sets the file path.
func (e *ConfigError) WithFile(file string) *ConfigError {
	e.File = file
	return e
}

// WithLocation sets the line and column.
func (e *ConfigError) WithLocation(line, column int) *ConfigError {
	e.Line = line
	e.Column = column
	return e
}

// Error implements the error interface.
func (e *ConfigError) Error() string {
	return e.Base.Error()
}

// Unwrap returns the underlying error.
func (e *ConfigError) Unwrap() error {
	return e.Base.Cause
}

// Is reports whether the target error matches this error by code.
func (e *ConfigError) Is(target error) bool {
	t, ok := target.(*ConfigError)
	if !ok {
		return false
	}
	return e.Base.Code == t.Base.Code
}
