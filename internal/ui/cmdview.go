package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/terassyi/toto/internal/resource"
)

// ANSI escape sequences for terminal control.
const (
	ansiCursorUp      = "\033[%dA"
	ansiClearLine     = "\033[2K"
	ansiCursorToStart = "\033[G"
)

// Default configuration for command view.
const defaultMaxLogLines = 4

// cmdTask represents a single command execution task.
type cmdTask struct {
	kind      resource.Kind
	name      string
	version   string
	method    string // installation method (e.g., "go install", "brew install")
	startTime time.Time
	logs      []string // circular buffer of recent log lines
	done      bool
	err       error
}

// elapsed returns the duration since task start, rounded to 100ms.
func (t *cmdTask) elapsed() time.Duration {
	return time.Since(t.startTime).Round(100 * time.Millisecond)
}

// statusText returns the current status as a string.
func (t *cmdTask) statusText() string {
	if !t.done {
		return fmt.Sprintf("%.1fs", t.elapsed().Seconds())
	}
	if t.err != nil {
		return "failed"
	}
	return "done"
}

// CommandView manages BuildKit-style output for command execution tasks.
// It displays a fixed-height scrolling view of recent log lines per task.
type CommandView struct {
	mu            sync.Mutex
	w             io.Writer
	isTTY         bool
	tasks         map[string]*cmdTask
	taskOrder     []string
	maxLogLines   int
	headerPrinted bool
	linesWritten  int
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

	v.tasks[key] = &cmdTask{
		kind:      kind,
		name:      name,
		version:   version,
		method:    method,
		startTime: time.Now(),
		logs:      make([]string, 0, v.maxLogLines),
	}
	v.taskOrder = append(v.taskOrder, key)

	if v.isTTY {
		v.redrawTTY()
	}
}

// AddOutput adds a line of output to a task.
func (v *CommandView) AddOutput(key, line string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	task, ok := v.tasks[key]
	if !ok || task.done {
		return
	}

	// Circular buffer: remove oldest if at capacity
	if len(task.logs) >= v.maxLogLines {
		task.logs = task.logs[1:]
	}
	task.logs = append(task.logs, line)

	if v.isTTY {
		v.redrawTTY()
	}
}

// CompleteTask marks a task as complete.
func (v *CommandView) CompleteTask(key string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if task, ok := v.tasks[key]; ok {
		task.done = true
		task.logs = nil
	}

	if v.isTTY {
		v.redrawTTY()
	}
}

// FailTask marks a task as failed.
func (v *CommandView) FailTask(key string, err error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if task, ok := v.tasks[key]; ok {
		task.done = true
		task.err = err
	}

	if v.isTTY {
		v.redrawTTY()
	}
}

// PrintTaskStart prints task start for non-TTY output.
func (v *CommandView) PrintTaskStart(key string) {
	v.mu.Lock()
	task := v.tasks[key]
	headerPrinted := v.headerPrinted
	v.headerPrinted = true
	v.mu.Unlock()

	if task == nil {
		return
	}

	if !headerPrinted {
		fmt.Fprintln(v.w)
		fmt.Fprintln(v.w, "Commands:")
	}

	style := NewStyle()
	fmt.Fprintf(v.w, " => %s/%s %s (%s)\n",
		task.kind, style.Path.Sprint(task.name), task.version, task.method)
}

// PrintOutput prints a single output line for non-TTY output.
func (v *CommandView) PrintOutput(line string) {
	fmt.Fprintf(v.w, "    %s\n", line)
}

// PrintTaskComplete prints task completion for non-TTY output.
func (v *CommandView) PrintTaskComplete(key string) {
	v.mu.Lock()
	task := v.tasks[key]
	v.mu.Unlock()

	if task == nil {
		return
	}

	style := NewStyle()
	elapsed := task.elapsed()

	if task.err != nil {
		fmt.Fprintf(v.w, " => %s/%s failed (%.1fs): %v\n",
			task.kind, task.name, elapsed.Seconds(), task.err)
	} else {
		fmt.Fprintf(v.w, " => %s/%s %s done (%.1fs)\n",
			task.kind, style.Path.Sprint(task.name), task.version, elapsed.Seconds())
	}
}

// redrawTTY redraws the view for TTY output with cursor control.
// Must be called with v.mu held.
func (v *CommandView) redrawTTY() {
	// Move cursor up to overwrite previous output
	if v.linesWritten > 0 {
		fmt.Fprintf(v.w, ansiCursorUp, v.linesWritten)
	}

	lines := v.buildOutputLines()

	// Write lines with clear
	for i, line := range lines {
		if i > 0 || v.linesWritten > 0 {
			fmt.Fprint(v.w, ansiClearLine+ansiCursorToStart)
		}
		fmt.Fprintln(v.w, line)
	}

	// Clear remaining old lines
	for i := len(lines); i < v.linesWritten; i++ {
		fmt.Fprint(v.w, ansiClearLine+ansiCursorToStart+"\n")
	}

	v.linesWritten = len(lines)
}

// buildOutputLines constructs the lines to display.
// Must be called with v.mu held.
func (v *CommandView) buildOutputLines() []string {
	var lines []string

	// Header
	if !v.headerPrinted || v.hasActiveTasks() {
		lines = append(lines, "", "Commands:")
		v.headerPrinted = true
	}

	style := NewStyle()

	for _, key := range v.taskOrder {
		task := v.tasks[key]
		if task == nil {
			continue
		}

		// Task header line
		lines = append(lines, fmt.Sprintf(" => %s/%s %s (%s) %s",
			task.kind, style.Path.Sprint(task.name), task.version, task.method, task.statusText()))

		// Log lines for active tasks
		if !task.done {
			for _, log := range task.logs {
				lines = append(lines, "    "+truncateLine(log, 70))
			}
		}

		// Error message for failed tasks
		if task.err != nil {
			lines = append(lines, fmt.Sprintf("    %s %v", style.FailMark, task.err))
		}
	}

	return lines
}

// hasActiveTasks returns true if there are non-completed tasks.
func (v *CommandView) hasActiveTasks() bool {
	for _, task := range v.tasks {
		if !task.done {
			return true
		}
	}
	return false
}

// truncateLine truncates a line to maxLen characters with ellipsis.
func truncateLine(line string, maxLen int) string {
	line = strings.TrimSpace(line)
	if len(line) <= maxLen {
		return line
	}
	return line[:maxLen-3] + "..."
}
