package ui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/terassyi/toto/internal/resource"
)

// cmdTask represents a single command execution task.
type cmdTask struct {
	kind      resource.Kind
	name      string
	version   string
	method    string // installation method (e.g., "go install", "brew install")
	startTime time.Time
	lastLog   string // most recent log line (for TTY spinner display)
	done      bool
	err       error
}

// elapsed returns the duration since task start, rounded to 100ms.
func (t *cmdTask) elapsed() time.Duration {
	return time.Since(t.startTime).Round(100 * time.Millisecond)
}

// CommandView manages task state and non-TTY output for command execution tasks.
type CommandView struct {
	mu            sync.Mutex
	w             io.Writer
	tasks         map[string]*cmdTask
	headerPrinted bool
}

// NewCommandView creates a new CommandView.
func NewCommandView(w io.Writer) *CommandView {
	return &CommandView{
		w:     w,
		tasks: make(map[string]*cmdTask),
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
	}
}

// AddOutput records the latest output line for a task.
func (v *CommandView) AddOutput(key, line string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	task, ok := v.tasks[key]
	if !ok || task.done {
		return
	}

	task.lastLog = line
}

// LastLog returns the most recent log line for a task.
func (v *CommandView) LastLog(key string) string {
	v.mu.Lock()
	defer v.mu.Unlock()

	if task, ok := v.tasks[key]; ok {
		return task.lastLog
	}
	return ""
}

// CompleteTask marks a task as complete.
func (v *CommandView) CompleteTask(key string) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if task, ok := v.tasks[key]; ok {
		task.done = true
		task.lastLog = ""
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

// truncateLine truncates a line to maxLen characters with ellipsis.
func truncateLine(line string, maxLen int) string {
	line = strings.TrimSpace(line)
	if len(line) <= maxLen {
		return line
	}
	return line[:maxLen-3] + "..."
}
