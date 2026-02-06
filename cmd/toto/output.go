package main

import "github.com/fatih/color"

// outputStyle holds common output styling for CLI commands.
type outputStyle struct {
	successMark string
	failMark    string
	warnMark    string
	header      *color.Color
	path        *color.Color
	success     *color.Color
	step        *color.Color
}

// newOutputStyle creates a new outputStyle with standard colors.
func newOutputStyle() *outputStyle {
	return &outputStyle{
		successMark: color.New(color.FgGreen).Sprint("✓"),
		failMark:    color.New(color.FgRed).Sprint("✗"),
		warnMark:    color.New(color.FgYellow).Sprint("⚠"),
		header:      color.New(color.FgCyan, color.Bold),
		path:        color.New(color.FgCyan),
		success:     color.New(color.FgGreen, color.Bold),
		step:        color.New(color.FgYellow),
	}
}
