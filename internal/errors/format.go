//nolint:revive // Package name intentionally shadows stdlib errors for convenience.
package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"
)

// Formatter formats errors for CLI output.
type Formatter struct {
	NoColor bool
	Writer  io.Writer

	// Colors
	errorColor    *color.Color
	codeColor     *color.Color
	resourceColor *color.Color
	hintColor     *color.Color
	exampleColor  *color.Color
	expectedColor *color.Color
	gotColor      *color.Color
	dimColor      *color.Color
	arrowColor    *color.Color
}

// NewFormatter creates a new Formatter.
func NewFormatter(w io.Writer, noColor bool) *Formatter {
	if noColor {
		color.NoColor = true
	}

	return &Formatter{
		NoColor:       noColor,
		Writer:        w,
		errorColor:    color.New(color.FgRed, color.Bold),
		codeColor:     color.New(color.FgRed),
		resourceColor: color.New(color.FgCyan),
		hintColor:     color.New(color.FgGreen),
		exampleColor:  color.New(color.FgBlue),
		expectedColor: color.New(color.FgYellow),
		gotColor:      color.New(color.FgRed),
		dimColor:      color.New(color.FgHiBlack),
		arrowColor:    color.New(color.FgYellow),
	}
}

// formatErrorHeader writes the error header with code.
// Format: "Error [E101]: message" or "Error: message" if no code.
func (f *Formatter) formatErrorHeader(sb *strings.Builder, code Code, message string) {
	sb.WriteString(f.errorColor.Sprint("Error"))
	if code != "" {
		sb.WriteString(" ")
		sb.WriteString(f.codeColor.Sprintf("[%s]", code))
	}
	sb.WriteString(f.errorColor.Sprint(": "))
	sb.WriteString(message)
	sb.WriteString("\n")
}

// Format formats an error for CLI display.
func (f *Formatter) Format(err error) string {
	if err == nil {
		return ""
	}

	var sb strings.Builder

	// Try to match specific error types
	var depErr *DependencyError
	var configErr *ConfigError
	var valErr *ValidationError
	var installErr *InstallError
	var checksumErr *ChecksumError
	var networkErr *NetworkError
	var stateErr *StateError
	var registryErr *RegistryError
	var baseErr *Error

	switch {
	case errors.As(err, &depErr):
		f.formatDependencyError(&sb, depErr)
	case errors.As(err, &configErr):
		f.formatConfigError(&sb, configErr)
	case errors.As(err, &valErr):
		f.formatValidationError(&sb, valErr)
	case errors.As(err, &checksumErr):
		f.formatChecksumError(&sb, checksumErr)
	case errors.As(err, &installErr):
		f.formatInstallError(&sb, installErr)
	case errors.As(err, &networkErr):
		f.formatNetworkError(&sb, networkErr)
	case errors.As(err, &stateErr):
		f.formatStateError(&sb, stateErr)
	case errors.As(err, &registryErr):
		f.formatRegistryError(&sb, registryErr)
	case errors.As(err, &baseErr):
		f.formatBaseError(&sb, baseErr)
	default:
		// Fallback for non-tomei errors
		sb.WriteString(f.errorColor.Sprint("Error: "))
		sb.WriteString(err.Error())
		sb.WriteString("\n")
	}

	return sb.String()
}

// FormatJSON formats an error as JSON.
func (f *Formatter) FormatJSON(err error) ([]byte, error) {
	if err == nil {
		return nil, nil
	}

	// Try to match specific error types
	var depErr *DependencyError
	var configErr *ConfigError
	var valErr *ValidationError
	var installErr *InstallError
	var checksumErr *ChecksumError
	var networkErr *NetworkError
	var stateErr *StateError
	var registryErr *RegistryError
	var baseErr *Error

	switch {
	case errors.As(err, &depErr):
		return json.MarshalIndent(depErr, "", "  ")
	case errors.As(err, &configErr):
		return json.MarshalIndent(configErr, "", "  ")
	case errors.As(err, &valErr):
		return json.MarshalIndent(valErr, "", "  ")
	case errors.As(err, &checksumErr):
		return json.MarshalIndent(checksumErr, "", "  ")
	case errors.As(err, &installErr):
		return json.MarshalIndent(installErr, "", "  ")
	case errors.As(err, &networkErr):
		return json.MarshalIndent(networkErr, "", "  ")
	case errors.As(err, &stateErr):
		return json.MarshalIndent(stateErr, "", "  ")
	case errors.As(err, &registryErr):
		return json.MarshalIndent(registryErr, "", "  ")
	case errors.As(err, &baseErr):
		return json.MarshalIndent(baseErr, "", "  ")
	default:
		// Fallback for non-tomei errors
		return json.MarshalIndent(map[string]string{"error": err.Error()}, "", "  ")
	}
}

func (f *Formatter) formatDependencyError(sb *strings.Builder, err *DependencyError) {
	f.formatErrorHeader(sb, err.Base.Code, err.Base.Message)
	sb.WriteString("\n")

	if err.IsCycle() {
		// Format cycle path
		for i, node := range err.Cycle {
			sb.WriteString("  ")
			if i == len(err.Cycle)-1 {
				sb.WriteString(f.gotColor.Sprintf("%s", node))
				sb.WriteString(f.arrowColor.Sprint("  ← cycle"))
			} else {
				sb.WriteString(f.resourceColor.Sprintf("%s", node))
			}
			sb.WriteString("\n")

			if i < len(err.Cycle)-1 {
				sb.WriteString("      ")
				sb.WriteString(f.arrowColor.Sprint("↓"))
				sb.WriteString(" depends on\n")
			}
		}
	} else if len(err.Missing) > 0 {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Resource: "))
		sb.WriteString(f.resourceColor.Sprint(err.Resource))
		sb.WriteString("\n")

		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Missing:  "))
		sb.WriteString(f.gotColor.Sprint(strings.Join(err.Missing, ", ")))
		sb.WriteString("\n")
	}

	f.formatHintAndExample(sb, &err.Base)
}

func (f *Formatter) formatConfigError(sb *strings.Builder, err *ConfigError) {
	f.formatErrorHeader(sb, err.Base.Code, err.Base.Message)
	sb.WriteString("\n")

	if err.File != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("File: "))
		sb.WriteString(f.resourceColor.Sprint(err.File))
		sb.WriteString("\n")
	}

	if err.Line > 0 {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Line: "))
		fmt.Fprintf(sb, "%d", err.Line)
		if err.Column > 0 {
			fmt.Fprintf(sb, ":%d", err.Column)
		}
		sb.WriteString("\n")
	}

	if err.Context != "" {
		sb.WriteString("\n")
		// Indent each line of context
		for line := range strings.SplitSeq(err.Context, "\n") {
			sb.WriteString("    ")
			sb.WriteString(f.dimColor.Sprint(line))
			sb.WriteString("\n")
		}
	}

	if err.Base.Cause != nil {
		sb.WriteString("\n  ")
		sb.WriteString(f.dimColor.Sprint("Cause: "))
		sb.WriteString(err.Base.Cause.Error())
		sb.WriteString("\n")
	}

	f.formatHintAndExample(sb, &err.Base)
}

func (f *Formatter) formatValidationError(sb *strings.Builder, err *ValidationError) {
	f.formatErrorHeader(sb, err.Base.Code, err.Base.Message)
	sb.WriteString("\n")

	if err.Resource != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Resource: "))
		sb.WriteString(f.resourceColor.Sprint(err.Resource))
		sb.WriteString("\n")
	}

	if err.Field != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Field:    "))
		sb.WriteString(err.Field)
		sb.WriteString("\n")
	}

	if err.Expected != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Expected: "))
		sb.WriteString(f.expectedColor.Sprint(err.Expected))
		sb.WriteString("\n")
	}

	if err.Got != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Got:      "))
		sb.WriteString(f.gotColor.Sprint(err.Got))
		sb.WriteString("\n")
	}

	f.formatHintAndExample(sb, &err.Base)
}

func (f *Formatter) formatInstallError(sb *strings.Builder, err *InstallError) {
	f.formatErrorHeader(sb, err.Base.Code, err.Base.Message)
	sb.WriteString("\n")

	if err.Resource != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Resource: "))
		sb.WriteString(f.resourceColor.Sprint(err.Resource))
		sb.WriteString("\n")
	}

	if err.Version != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Version:  "))
		sb.WriteString(err.Version)
		sb.WriteString("\n")
	}

	if err.URL != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("URL:      "))
		sb.WriteString(err.URL)
		sb.WriteString("\n")
	}

	if err.Base.Cause != nil {
		sb.WriteString("\n  ")
		sb.WriteString(f.dimColor.Sprint("Cause: "))
		sb.WriteString(err.Base.Cause.Error())
		sb.WriteString("\n")
	}

	f.formatHintAndExample(sb, &err.Base)
}

func (f *Formatter) formatChecksumError(sb *strings.Builder, err *ChecksumError) {
	f.formatErrorHeader(sb, err.Base.Code, err.Base.Message)
	sb.WriteString("\n")

	if err.Resource != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Resource: "))
		sb.WriteString(f.resourceColor.Sprint(err.Resource))
		sb.WriteString("\n")
	}

	if err.URL != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("URL:      "))
		sb.WriteString(err.URL)
		sb.WriteString("\n")
	}

	sb.WriteString("\n")
	sb.WriteString("  ")
	sb.WriteString(f.dimColor.Sprint("Expected: "))
	sb.WriteString(f.expectedColor.Sprint(err.Expected))
	sb.WriteString("\n")

	sb.WriteString("  ")
	sb.WriteString(f.dimColor.Sprint("Got:      "))
	sb.WriteString(f.gotColor.Sprint(err.Got))
	sb.WriteString("\n")

	f.formatHintAndExample(sb, &err.Base)
}

func (f *Formatter) formatNetworkError(sb *strings.Builder, err *NetworkError) {
	f.formatErrorHeader(sb, err.Base.Code, err.Base.Message)
	sb.WriteString("\n")

	if err.URL != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("URL:    "))
		sb.WriteString(err.URL)
		sb.WriteString("\n")
	}

	if err.StatusCode > 0 {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Status: "))
		sb.WriteString(f.gotColor.Sprintf("%d", err.StatusCode))
		sb.WriteString("\n")
	}

	if err.Base.Cause != nil {
		sb.WriteString("\n  ")
		sb.WriteString(f.dimColor.Sprint("Cause: "))
		sb.WriteString(err.Base.Cause.Error())
		sb.WriteString("\n")
	}

	f.formatHintAndExample(sb, &err.Base)
}

func (f *Formatter) formatStateError(sb *strings.Builder, err *StateError) {
	f.formatErrorHeader(sb, err.Base.Code, err.Base.Message)
	sb.WriteString("\n")

	if err.LockPID > 0 {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Another tomei process is running (PID: "))
		sb.WriteString(f.gotColor.Sprintf("%d", err.LockPID))
		sb.WriteString(f.dimColor.Sprint(")"))
		sb.WriteString("\n")
	}

	if err.LockFile != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Lock file: "))
		sb.WriteString(f.resourceColor.Sprint(err.LockFile))
		sb.WriteString("\n")
	}

	if err.Base.Cause != nil {
		sb.WriteString("\n  ")
		sb.WriteString(f.dimColor.Sprint("Cause: "))
		sb.WriteString(err.Base.Cause.Error())
		sb.WriteString("\n")
	}

	f.formatHintAndExample(sb, &err.Base)
}

func (f *Formatter) formatRegistryError(sb *strings.Builder, err *RegistryError) {
	f.formatErrorHeader(sb, err.Base.Code, err.Base.Message)
	sb.WriteString("\n")

	if err.Registry != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Registry: "))
		sb.WriteString(f.resourceColor.Sprint(err.Registry))
		sb.WriteString("\n")
	}

	if err.Package != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Package:  "))
		sb.WriteString(err.Package)
		sb.WriteString("\n")
	}

	if err.Version != "" {
		sb.WriteString("  ")
		sb.WriteString(f.dimColor.Sprint("Version:  "))
		sb.WriteString(err.Version)
		sb.WriteString("\n")
	}

	if err.Base.Cause != nil {
		sb.WriteString("\n  ")
		sb.WriteString(f.dimColor.Sprint("Cause: "))
		sb.WriteString(err.Base.Cause.Error())
		sb.WriteString("\n")
	}

	f.formatHintAndExample(sb, &err.Base)
}

func (f *Formatter) formatBaseError(sb *strings.Builder, err *Error) {
	f.formatErrorHeader(sb, err.Code, err.Message)

	if err.Cause != nil {
		sb.WriteString("\n  ")
		sb.WriteString(f.dimColor.Sprint("Cause: "))
		sb.WriteString(err.Cause.Error())
		sb.WriteString("\n")
	}

	f.formatHintAndExample(sb, err)
}

func (f *Formatter) formatHintAndExample(sb *strings.Builder, err *Error) {
	if err.Hint != "" {
		sb.WriteString("\n")
		sb.WriteString(f.hintColor.Sprint("Hint: "))
		// Handle multi-line hints
		lines := strings.Split(err.Hint, "\n")
		sb.WriteString(lines[0])
		sb.WriteString("\n")
		for _, line := range lines[1:] {
			sb.WriteString("      ")
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	if err.Example != "" {
		sb.WriteString("\n")
		sb.WriteString(f.exampleColor.Sprint("Example:"))
		sb.WriteString("\n")
		for line := range strings.SplitSeq(err.Example, "\n") {
			sb.WriteString("  ")
			sb.WriteString(f.dimColor.Sprint(line))
			sb.WriteString("\n")
		}
	}
}
