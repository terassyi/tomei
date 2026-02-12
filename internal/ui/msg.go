package ui

import (
	"log/slog"
	"time"

	"github.com/terassyi/tomei/internal/installer/engine"
)

// engineEventMsg wraps an engine.Event as a Bubble Tea message.
type engineEventMsg struct {
	event engine.Event
}

// applyDoneMsg signals that engine.Apply has completed.
type applyDoneMsg struct {
	err error
}

// tickMsg triggers periodic UI updates (elapsed time, spinner).
type tickMsg time.Time

// slogMsg delivers a structured log record to the TUI model.
type slogMsg struct {
	level   slog.Level
	message string
}
