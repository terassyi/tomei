package main

import (
	"github.com/fatih/color"
	"github.com/terassyi/toto/internal/resource"
)

// outputStyle holds common output styling for CLI commands.
type outputStyle struct {
	successMark   string
	failMark      string
	warnMark      string
	upgradeMark   string
	removeMark    string
	reinstallMark string
	header        *color.Color
	path          *color.Color
	success       *color.Color
	step          *color.Color
}

// newOutputStyle creates a new outputStyle with standard colors.
func newOutputStyle() *outputStyle {
	return &outputStyle{
		successMark:   color.New(color.FgGreen).Sprint("✓"),
		failMark:      color.New(color.FgRed).Sprint("✗"),
		warnMark:      color.New(color.FgYellow).Sprint("⚠"),
		upgradeMark:   color.New(color.FgCyan).Sprint("↑"),
		removeMark:    color.New(color.FgYellow).Sprint("-"),
		reinstallMark: color.New(color.FgCyan).Sprint("⟳"),
		header:        color.New(color.FgCyan, color.Bold),
		path:          color.New(color.FgCyan),
		success:       color.New(color.FgGreen, color.Bold),
		step:          color.New(color.FgYellow),
	}
}

// actionIcon returns the icon for an action type.
func (s *outputStyle) actionIcon(action resource.ActionType) string {
	switch action {
	case resource.ActionInstall:
		return s.successMark
	case resource.ActionUpgrade:
		return s.upgradeMark
	case resource.ActionRemove:
		return s.removeMark
	case resource.ActionReinstall:
		return s.reinstallMark
	default:
		return " "
	}
}
