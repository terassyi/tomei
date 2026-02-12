package ui

import "github.com/charmbracelet/lipgloss"

var (
	doneMarkStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))   // green
	failMarkStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))   // red
	layerHeaderStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("14"))  // light cyan
	delegationLogStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("245")) // gray
	warnLogStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))   // yellow
	errorLogStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))   // red
	debugLogStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("245")) // gray
	logSeparatorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245")) // gray
	runningMark        = "=>"
	doneMark           = doneMarkStyle.Render("✓")
	failMark           = failMarkStyle.Render("✗")
	logIndent          = "   "
)

// spinnerChars are the braille spinner frames.
var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
