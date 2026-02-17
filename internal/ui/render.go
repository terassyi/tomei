package ui

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/terassyi/tomei/internal/installer/engine"
)

const (
	progressBarWidth = 20
	progressFull     = '█'
	progressEmpty    = '░'
)

// View implements tea.Model.
// Renders ALL layers: completed layers (snapshots) + current layer + pending layer headers.
// The last frame rendered before tea.Quit persists in the terminal scrollback.
func (m *ApplyModel) View() string {
	if m.totalLayers == 0 && len(m.allLayerNodes) == 0 {
		return ""
	}

	var b strings.Builder

	// Determine the first layer not yet covered by a snapshot.
	// Completed snapshots cover layers 0..len(completedLayers)-1.
	// The current live layer is at m.currentLayer and should be rendered
	// only when it has NOT been snapshotted yet.
	snapshotCount := len(m.completedLayers)
	currentLayerSnapshotted := snapshotCount > m.currentLayer

	// 1. Completed layers (from snapshots)
	for i, snapshot := range m.completedLayers {
		nodes := m.allLayerNodes[i]
		b.WriteString(renderLayerHeader(snapshot.phase, i, m.totalLayers, nodes, formatElapsed(snapshot.elapsed), m.width))
		b.WriteByte('\n')
		renderTaskList(&b, snapshot.tasks, snapshot.taskOrder, snapshot.completedOrder, m.width)
	}

	// 2. Current layer (live, rendered only when not yet snapshotted)
	if !currentLayerSnapshotted {
		elapsed := m.layerElapsed
		nodes := m.allLayerNodes[m.currentLayer]
		b.WriteString(renderLayerHeader(m.currentPhase, m.currentLayer, m.totalLayers, nodes, formatElapsed(elapsed), m.width))
		b.WriteByte('\n')
		renderTaskList(&b, m.tasks, m.taskOrder, m.completedOrder, m.width)
	}

	// 3. Pending DAG layer headers (layers not yet started)
	// Only render pending headers for DAG layers (phase layers are dynamic).
	// Start from whichever is higher: the layer after the live one,
	// or after all snapshots (to avoid duplicating a snapshotted layer).
	pendingStart := max(m.currentLayer+1, snapshotCount)
	for i := pendingStart; i < m.totalLayers; i++ {
		b.WriteString(renderLayerHeader(engine.PhaseDAG, i, m.totalLayers, m.allLayerNodes[i], "-", m.width))
		b.WriteByte('\n')
	}

	// 4. Log panel (slog messages)
	renderLogPanel(&b, m.slogLines, m.width)

	// 5. Elapsed footer
	fmt.Fprintf(&b, "\nElapsed: %s", formatElapsed(m.totalElapsed))

	return b.String()
}

// renderTaskList renders all tasks in a layer to the builder.
// Completed/failed tasks are rendered first (in completion order),
// followed by running tasks (in start order).
func renderTaskList(b *strings.Builder, tasks map[string]*taskState, taskOrder []string, completedOrder []string, width int) {
	// 1. Completed/failed tasks in completion order
	for _, key := range completedOrder {
		task := tasks[key]
		if task == nil {
			continue
		}
		renderTask(b, task, width)
	}

	// 2. Running tasks in start order (skip already-rendered completed tasks)
	completedSet := make(map[string]struct{}, len(completedOrder))
	for _, key := range completedOrder {
		completedSet[key] = struct{}{}
	}
	for _, key := range taskOrder {
		if _, done := completedSet[key]; done {
			continue
		}
		task := tasks[key]
		if task == nil {
			continue
		}
		renderTask(b, task, width)
	}
}

// renderTask renders a single task to the builder.
func renderTask(b *strings.Builder, task *taskState, width int) {
	taskElapsed := task.elapsed
	if task.status == taskRunning {
		taskElapsed = time.Since(task.startTime)
	}

	switch task.status {
	case taskDone:
		b.WriteString(renderCompletedLine(task, taskElapsed, width))
		b.WriteByte('\n')
	case taskFailed:
		b.WriteString(renderFailedLine(task, taskElapsed, width))
		b.WriteByte('\n')
	case taskRunning:
		if task.hasProgress {
			b.WriteString(renderProgressLine(task, taskElapsed, width))
			b.WriteByte('\n')
		} else {
			lines := renderDelegationLines(task, taskElapsed, width)
			for _, line := range lines {
				b.WriteString(line)
				b.WriteByte('\n')
			}
		}
	}
}

// renderLayerHeader renders a layer header line.
// e.g. "Layer 1/2: Runtime/go                                                    4.4s"
func renderLayerHeader(phase engine.Phase, layer, total int, nodes []string, elapsed string, width int) string {
	var prefix string
	var style lipgloss.Style

	switch phase {
	case engine.PhaseTaint:
		prefix = fmt.Sprintf("Reinstall: %s", fitNodeNames(nodes, width-20))
		style = taintHeaderStyle
	case engine.PhaseRemove:
		prefix = fmt.Sprintf("Remove: %s", fitNodeNames(nodes, width-20))
		style = removeHeaderStyle
	default:
		prefix = fmt.Sprintf("Layer %d/%d: %s", layer+1, total, fitNodeNames(nodes, width-30))
		style = layerHeaderStyle
	}

	return style.Render(rightAlign(prefix, elapsed, width))
}

// renderCompletedLine renders a completed task line.
// e.g. " ✓ Runtime/go 1.25.6  installed to ~/.local/share/tomei/runtimes/go     4.4s"
func renderCompletedLine(t *taskState, layerElapsed time.Duration, width int) string {
	taskElapsed := formatElapsed(layerElapsed)
	label := taskLabel(t)

	var detail string
	if t.installPath != "" {
		detail = "installed to " + shortenPath(t.installPath)
	} else {
		detail = string(t.action)
	}

	prefix := fmt.Sprintf(" %s %s  %s", doneMark, label, detail)
	return rightAlign(prefix, taskElapsed, width)
}

// renderFailedLine renders a failed task line.
// e.g. " ✗ Tool/bat 0.25.0  failed: connection refused                           0.3s"
func renderFailedLine(t *taskState, layerElapsed time.Duration, width int) string {
	taskElapsed := formatElapsed(layerElapsed)
	label := taskLabel(t)

	errMsg := "unknown error"
	if t.err != nil {
		errMsg = t.err.Error()
		if len(errMsg) > 50 {
			errMsg = errMsg[:47] + "..."
		}
	}

	prefix := fmt.Sprintf(" %s %s  failed: %s", failMark, label, errMsg)
	return rightAlign(prefix, taskElapsed, width)
}

// renderProgressLine renders a running task with a progress bar.
// e.g. " => Runtime/go 1.25.6  ████████░░░░░░░░░░░░░░  12.3 MiB / 95.0 MiB    0.3s"
func renderProgressLine(t *taskState, layerElapsed time.Duration, width int) string {
	taskElapsed := formatElapsed(layerElapsed)
	label := taskLabel(t)
	bar := renderProgressBar(t.downloaded, t.total)
	sizes := fmt.Sprintf("%s / %s", formatSize(t.downloaded), formatSize(t.total))

	prefix := fmt.Sprintf(" %s %s  %s  %s", runningMark, label, bar, sizes)
	return rightAlign(prefix, taskElapsed, width)
}

// renderDelegationLines renders a running task with a spinner + log lines.
// Returns multiple lines: 1 header line + up to maxLogLines log lines.
func renderDelegationLines(t *taskState, layerElapsed time.Duration, width int) []string {
	taskElapsed := formatElapsed(layerElapsed)
	label := taskLabel(t)

	// Determine spinner frame based on elapsed time
	frame := spinnerFrame(t.startTime)

	header := rightAlign(fmt.Sprintf(" %s %s  %s", runningMark, label, frame), taskElapsed, width)
	lines := []string{header}

	for _, logLine := range t.logLines {
		lines = append(lines, delegationLogStyle.Render(logIndent+" "+logLine))
	}

	return lines
}

// renderProgressBar renders a Unicode progress bar.
func renderProgressBar(downloaded, total int64) string {
	if total <= 0 {
		return strings.Repeat(string(progressEmpty), progressBarWidth)
	}

	ratio := float64(downloaded) / float64(total)
	if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(progressBarWidth))
	empty := progressBarWidth - filled

	return strings.Repeat(string(progressFull), filled) + strings.Repeat(string(progressEmpty), empty)
}

// spinnerFrame returns the current spinner character based on elapsed time.
func spinnerFrame(startTime time.Time) string {
	elapsed := time.Since(startTime)
	idx := int(elapsed.Milliseconds()/80) % len(spinnerChars)
	return spinnerChars[idx]
}

// taskLabel returns the display label for a task, e.g. "Runtime/go 1.25.6" or "Tool/bat 0.25.0 (aqua install)".
func taskLabel(t *taskState) string {
	label := fmt.Sprintf("%s/%s", t.kind, t.name)
	if t.version != "" {
		label += " " + t.version
	}
	if t.method != "" && t.method != "download" {
		label += " (" + t.method + ")"
	}
	return label
}

// formatElapsed formats a duration as "X.Xs".
func formatElapsed(d time.Duration) string {
	secs := d.Seconds()
	return fmt.Sprintf("%.1fs", secs)
}

// formatSize formats bytes as human-readable size.
func formatSize(bytes int64) string {
	const (
		kib = 1024
		mib = 1024 * kib
		gib = 1024 * mib
	)

	switch {
	case bytes >= gib:
		return fmt.Sprintf("%.1f GiB", float64(bytes)/float64(gib))
	case bytes >= mib:
		return fmt.Sprintf("%.1f MiB", float64(bytes)/float64(mib))
	case bytes >= kib:
		return fmt.Sprintf("%.1f KiB", float64(bytes)/float64(kib))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// fitNodeNames joins node names, truncating if too long.
func fitNodeNames(names []string, maxWidth int) string {
	if len(names) == 0 {
		return ""
	}

	joined := strings.Join(names, ", ")
	if len(joined) <= maxWidth {
		return joined
	}

	// Progressively include names until we exceed maxWidth
	result := names[0]
	for i := 1; i < len(names); i++ {
		candidate := result + ", " + names[i]
		suffix := fmt.Sprintf(", ... +%d more", len(names)-i)
		if len(candidate)+len(suffix) > maxWidth {
			return result + suffix
		}
		result = candidate
	}
	return result
}

// shortenPath replaces the user's home directory with ~.
func shortenPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// rightAlign places suffix at the right edge of a line of given width.
// Uses width-1 to prevent terminals from wrapping at the exact column boundary.
func rightAlign(prefix, suffix string, width int) string {
	// Strip ANSI escape sequences for width calculation
	prefixLen := lipglossWidth(prefix)
	suffixLen := len(suffix)

	gap := max(width-1-prefixLen-suffixLen, 1)
	return prefix + strings.Repeat(" ", gap) + suffix
}

// renderLogPanel renders the slog log panel if there are log lines.
func renderLogPanel(b *strings.Builder, lines []slogLine, width int) {
	if len(lines) == 0 {
		return
	}

	// Separator line
	sep := "── Logs " + strings.Repeat("─", max(width-8, 0))
	b.WriteByte('\n')
	b.WriteString(logSeparatorStyle.Render(sep))
	b.WriteByte('\n')

	for _, line := range lines {
		label := slogLevelLabel(line.level)
		text := fmt.Sprintf(" %s %s", label, line.message)
		styled := slogLineStyle(line.level, text)
		b.WriteString(styled)
		b.WriteByte('\n')
	}
}

// slogLevelLabel returns a styled short label for the log level.
func slogLevelLabel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return errorLogStyle.Render("ERROR")
	case level >= slog.LevelWarn:
		return warnLogStyle.Render("WARN")
	case level >= slog.LevelInfo:
		return "INFO"
	default:
		return debugLogStyle.Render("DEBUG")
	}
}

// slogLineStyle applies color to the entire log line based on level.
func slogLineStyle(level slog.Level, text string) string {
	switch {
	case level >= slog.LevelError:
		return errorLogStyle.Render(text)
	case level >= slog.LevelWarn:
		return warnLogStyle.Render(text)
	case level >= slog.LevelInfo:
		return text
	default:
		return debugLogStyle.Render(text)
	}
}

// lipglossWidth returns the visible width of a string, stripping ANSI escape sequences.
func lipglossWidth(s string) int {
	width := 0
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEscape = false
			}
			continue
		}
		width++
	}
	return width
}
