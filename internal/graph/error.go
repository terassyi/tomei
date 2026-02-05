package graph

import (
	"fmt"
	"strings"

	"github.com/fatih/color"
)

// CycleError represents a circular dependency error with detailed path information.
type CycleError struct {
	Cycle []NodeID
}

// Error implements the error interface.
func (e *CycleError) Error() string {
	return fmt.Sprintf("circular dependency detected: %v", e.Cycle)
}

// FormatCycle returns a human-readable formatted string showing the cycle path.
func (e *CycleError) FormatCycle(noColor bool) string {
	if len(e.Cycle) == 0 {
		return "circular dependency detected (empty cycle)"
	}

	if noColor {
		color.NoColor = true
	}

	var sb strings.Builder
	red := color.New(color.FgRed)
	yellow := color.New(color.FgYellow)
	cyan := color.New(color.FgCyan)

	sb.WriteString(red.Sprint("Error: circular dependency detected"))
	sb.WriteString("\n\n")

	for i, node := range e.Cycle {
		// Indent
		sb.WriteString("  ")

		// Last node (same as first) shows the cycle point
		if i == len(e.Cycle)-1 {
			sb.WriteString(red.Sprintf("%s", node))
			sb.WriteString(yellow.Sprint("  ← cycle"))
		} else {
			sb.WriteString(cyan.Sprintf("%s", node))
		}
		sb.WriteString("\n")

		// Arrow to next node (except after the last)
		if i < len(e.Cycle)-1 {
			sb.WriteString("      ")
			sb.WriteString(yellow.Sprint("↓"))
			sb.WriteString(" depends on\n")
		}
	}

	return sb.String()
}

// NewCycleError creates a new CycleError from a cycle path.
func NewCycleError(cycle []NodeID) *CycleError {
	return &CycleError{Cycle: cycle}
}
