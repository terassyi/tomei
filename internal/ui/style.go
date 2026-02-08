package ui

import (
	"github.com/fatih/color"
	"github.com/terassyi/tomei/internal/resource"
)

// Style holds common output styling for CLI commands.
type Style struct {
	SuccessMark   string
	FailMark      string
	WarnMark      string
	UpgradeMark   string
	RemoveMark    string
	ReinstallMark string
	Header        *color.Color
	Path          *color.Color
	Success       *color.Color
	Step          *color.Color
}

// NewStyle creates a new Style with standard colors.
func NewStyle() *Style {
	return &Style{
		SuccessMark:   color.New(color.FgGreen).Sprint("✓"),
		FailMark:      color.New(color.FgRed).Sprint("✗"),
		WarnMark:      color.New(color.FgYellow).Sprint("⚠"),
		UpgradeMark:   color.New(color.FgCyan).Sprint("↑"),
		RemoveMark:    color.New(color.FgYellow).Sprint("-"),
		ReinstallMark: color.New(color.FgCyan).Sprint("⟳"),
		Header:        color.New(color.FgCyan, color.Bold),
		Path:          color.New(color.FgCyan),
		Success:       color.New(color.FgGreen, color.Bold),
		Step:          color.New(color.FgYellow),
	}
}

// ActionIcon returns the icon for an action type.
func (s *Style) ActionIcon(action resource.ActionType) string {
	switch action {
	case resource.ActionInstall:
		return s.SuccessMark
	case resource.ActionUpgrade:
		return s.UpgradeMark
	case resource.ActionRemove:
		return s.RemoveMark
	case resource.ActionReinstall:
		return s.ReinstallMark
	default:
		return " "
	}
}
