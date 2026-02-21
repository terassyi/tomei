package env

import (
	"fmt"
	"strings"
)

// Shell variable references and commands used in generated output.
const (
	shellHome   = "$HOME"
	shellPath   = "$PATH"
	fishAddPath = "fish_add_path"
)

// ShellType represents a shell syntax type.
type ShellType string

const (
	// ShellPosix represents POSIX-compatible shells (bash, zsh, sh).
	ShellPosix ShellType = "posix"
	// ShellFish represents the fish shell.
	ShellFish ShellType = "fish"
)

// ParseShellType parses a string into a ShellType.
func ParseShellType(s string) (ShellType, error) {
	switch s {
	case "posix", "bash", "sh", "zsh", "":
		return ShellPosix, nil
	case "fish":
		return ShellFish, nil
	default:
		return "", fmt.Errorf("unsupported shell type: %q (supported: posix, fish)", s)
	}
}

// Formatter provides shell-specific formatting for environment variable statements.
type Formatter interface {
	// ExportVar formats a single environment variable export statement.
	ExportVar(key, value string) string
	// ExportPath formats a PATH export statement with the given directories prepended.
	ExportPath(dirs []string) string
	// Ext returns the file extension for this shell type (e.g., ".sh", ".fish").
	// The format matches filepath.Ext() convention (dot-prefixed).
	Ext() string
}

// NewFormatter returns a Formatter for the given ShellType.
func NewFormatter(st ShellType) Formatter {
	switch st {
	case ShellFish:
		return fishFormatter{}
	default:
		return posixFormatter{}
	}
}

var (
	_ Formatter = (*posixFormatter)(nil)
	_ Formatter = (*fishFormatter)(nil)
)

type posixFormatter struct{}

func (posixFormatter) ExportVar(key, value string) string {
	return fmt.Sprintf("export %s=%q", key, value)
}

func (posixFormatter) ExportPath(dirs []string) string {
	return fmt.Sprintf("export PATH=%q", strings.Join(dirs, ":")+":"+shellPath)
}

func (posixFormatter) Ext() string { return ".sh" }

type fishFormatter struct{}

func (fishFormatter) ExportVar(key, value string) string {
	return fmt.Sprintf("set -gx %s %q", key, value)
}

func (fishFormatter) ExportPath(dirs []string) string {
	quoted := make([]string, len(dirs))
	for i, d := range dirs {
		quoted[i] = fmt.Sprintf("%q", d)
	}
	return fmt.Sprintf("%s %s", fishAddPath, strings.Join(quoted, " "))
}

func (fishFormatter) Ext() string { return ".fish" }
