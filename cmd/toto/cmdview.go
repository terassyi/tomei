package main

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/terassyi/toto/internal/resource"
)

const (
	// ANSI escape sequences
	cursorUp      = "\033[%dA"
	clearLine     = "\033[2K"
	cursorToStart = "\033[G"

	// Default max log lines to display per task
	defaultMaxLogLines = 4
)

// cmdTask represents a single command execution task.
type cmdTask struct {
	kind      resource.Kind
	name      string
	version   string
	method    string // "go install", "brew install", etc.
	startTime time.Time
	logs      []string // circular buffer of recent log lines
	done      bool
	err       error
}

// CommandView manages the Commands section display with BuildKit-style output.
type CommandView struct {
	mu            sync.Mutex
	w             io.Writer
	isTTY         bool
	tasks         map[string]*cmdTask
	taskOrder     []string // order of task keys for display
	maxLogLines   int
	headerPrinted bool
	linesWritten  int // number of lines currently displayed (for TTY redraw)
}

// NewCommandView creates a new CommandView.
func NewCommandView(w io.Writer, isTTY bool) *CommandView {
	return &CommandView{
		w:           w,
		isTTY:       isTTY,
		tasks:       make(map[string]*cmdTask),
		maxLogLines: defaultMaxLogLines,
	}
}

// StartTask begins tracking a new command task.
func (v *CommandView) StartTask(key string, kind resource.Kind, name, version, method string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	task := &cmdTask{
		kind:      kind,
		name:      name,
		version:   version,
		method:    method,
		startTime: time.Now(),
		logs:      make([]string, 0, v.maxLogLines),
	}

	v.tasks[key] = task
	v.taskOrder = append(v.taskOrder, key)

	v.redraw()
}

// AddOutput adds a line of output to a task.
func (v *CommandView) AddOutput(key, line string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	task, exists := v.tasks[key]
	if !exists || task.done {
		return
	}

	// Add to circular buffer
	if len(task.logs) >= v.maxLogLines {
		task.logs = task.logs[1:]
	}
	task.logs = append(task.logs, line)

	v.redraw()
}

// CompleteTask marks a task as complete.
func (v *CommandView) CompleteTask(key string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	task, exists := v.tasks[key]
	if !exists {
		return
	}

	task.done = true
	task.logs = nil // clear logs on completion

	v.redraw()
}

// FailTask marks a task as failed.
func (v *CommandView) FailTask(key string, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	task, exists := v.tasks[key]
	if !exists {
		return
	}

	task.done = true
	task.err = err

	v.redraw()
}

// redraw redraws the entire command view (must be called with lock held).
func (v *CommandView) redraw() {
	if v.isTTY {
		v.redrawTTY()
	} else {
		v.redrawNonTTY()
	}
}

// redrawTTY redraws for TTY environment with cursor control.
func (v *CommandView) redrawTTY() {
	// Move cursor up to clear previous output
	if v.linesWritten > 0 {
		fmt.Fprintf(v.w, cursorUp, v.linesWritten)
	}

	var lines []string

	// Header
	if !v.headerPrinted || v.hasActiveTasks() {
		lines = append(lines, "")
		lines = append(lines, "Commands:")
		v.headerPrinted = true
	}

	style := newOutputStyle()

	// Render each task
	for _, key := range v.taskOrder {
		task := v.tasks[key]
		if task == nil {
			continue
		}

		// Task header line
		elapsed := time.Since(task.startTime).Round(100 * time.Millisecond)
		status := fmt.Sprintf("%.1fs", elapsed.Seconds())
		if task.done {
			if task.err != nil {
				status = "failed"
			} else {
				status = "done"
			}
		}

		taskLine := fmt.Sprintf(" => %s/%s %s (%s) %s",
			task.kind,
			style.path.Sprint(task.name),
			task.version,
			task.method,
			status)
		lines = append(lines, taskLine)

		// Log lines (only for active tasks)
		if !task.done {
			for _, log := range task.logs {
				// Indent log lines
				logLine := fmt.Sprintf("    %s", truncateLine(log, 70))
				lines = append(lines, logLine)
			}
		}

		// Error message for failed tasks
		if task.err != nil {
			errLine := fmt.Sprintf("    %s %v", style.failMark, task.err)
			lines = append(lines, errLine)
		}
	}

	// Clear and write lines
	for i, line := range lines {
		if i > 0 || v.linesWritten > 0 {
			fmt.Fprint(v.w, clearLine+cursorToStart)
		}
		fmt.Fprintln(v.w, line)
	}

	// Clear any remaining old lines
	for i := len(lines); i < v.linesWritten; i++ {
		fmt.Fprint(v.w, clearLine+cursorToStart+"\n")
	}

	v.linesWritten = len(lines)
}

// redrawNonTTY outputs for non-TTY environment (simple streaming).
func (v *CommandView) redrawNonTTY() {
	// In non-TTY mode, only output new content
	// This is called on each update, so we need to track what's been printed

	if !v.headerPrinted {
		fmt.Fprintln(v.w)
		fmt.Fprintln(v.w, "Commands:")
		v.headerPrinted = true
	}

	style := newOutputStyle()

	// Find the most recently updated task
	for _, key := range v.taskOrder {
		task := v.tasks[key]
		if task == nil {
			continue
		}

		// For non-TTY, we print task start, logs, and completion separately
		// This is handled by the caller checking the state
	}

	// For non-TTY, actual output is done in specific methods
	_ = style // avoid unused
}

// PrintTaskStart prints task start for non-TTY.
func (v *CommandView) PrintTaskStart(key string) {
	if v.isTTY {
		return
	}

	v.mu.Lock()
	task := v.tasks[key]
	v.mu.Unlock()

	if task == nil {
		return
	}

	if !v.headerPrinted {
		fmt.Fprintln(v.w)
		fmt.Fprintln(v.w, "Commands:")
		v.headerPrinted = true
	}

	style := newOutputStyle()
	fmt.Fprintf(v.w, " => %s/%s %s (%s)\n",
		task.kind,
		style.path.Sprint(task.name),
		task.version,
		task.method)
}

// PrintOutput prints a single output line for non-TTY.
func (v *CommandView) PrintOutput(line string) {
	if v.isTTY {
		return
	}
	fmt.Fprintf(v.w, "    %s\n", line)
}

// PrintTaskComplete prints task completion for non-TTY.
func (v *CommandView) PrintTaskComplete(key string) {
	if v.isTTY {
		return
	}

	v.mu.Lock()
	task := v.tasks[key]
	v.mu.Unlock()

	if task == nil {
		return
	}

	elapsed := time.Since(task.startTime).Round(100 * time.Millisecond)
	style := newOutputStyle()

	if task.err != nil {
		fmt.Fprintf(v.w, " => %s/%s failed (%.1fs): %v\n",
			task.kind,
			task.name,
			elapsed.Seconds(),
			task.err)
	} else {
		fmt.Fprintf(v.w, " => %s/%s %s done (%.1fs)\n",
			task.kind,
			style.path.Sprint(task.name),
			task.version,
			elapsed.Seconds())
	}
}

// hasActiveTasks returns true if there are any non-done tasks.
func (v *CommandView) hasActiveTasks() bool {
	for _, task := range v.tasks {
		if !task.done {
			return true
		}
	}
	return false
}

// truncateLine truncates a line to maxLen characters.
func truncateLine(line string, maxLen int) string {
	line = strings.TrimSpace(line)
	if len(line) <= maxLen {
		return line
	}
	return line[:maxLen-3] + "..."
}
