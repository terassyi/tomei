package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/resource"
)

// Update implements tea.Model.
func (m *ApplyModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil

	case tickMsg:
		now := time.Now()
		if !m.applyStart.IsZero() {
			m.totalElapsed = now.Sub(m.applyStart)
		}
		if !m.layerStart.IsZero() {
			m.layerElapsed = now.Sub(m.layerStart)
		}
		return m, tick()

	case engineEventMsg:
		return m.handleEngineEvent(msg.event)

	case slogMsg:
		return m.handleSlogMsg(msg)

	case applyDoneMsg:
		return m.handleApplyDone(msg)
	}

	return m, nil
}

// handleEngineEvent processes engine events and updates model state.
func (m *ApplyModel) handleEngineEvent(event engine.Event) (tea.Model, tea.Cmd) {
	switch event.Type {
	case engine.EventLayerStart:
		return m.handleLayerStart(event)
	case engine.EventStart:
		return m.handleStart(event)
	case engine.EventProgress:
		return m.handleProgress(event)
	case engine.EventOutput:
		return m.handleOutput(event)
	case engine.EventComplete:
		return m.handleComplete(event)
	case engine.EventError:
		return m.handleError(event)
	}
	return m, nil
}

// handleLayerStart processes an EventLayerStart event.
func (m *ApplyModel) handleLayerStart(event engine.Event) (tea.Model, tea.Cmd) {
	now := time.Now()

	// First EventLayerStart: initialize all-layer info and start timing
	if m.applyStart.IsZero() {
		m.applyStart = now
		m.allLayerNodes = event.AllLayerNodes
		m.totalLayers = event.TotalLayers
	}

	// Snapshot previous layer (if not the first layer)
	if event.Layer > 0 {
		m.snapshotCurrentLayer()
	}

	// Reset for new layer
	m.currentLayer = event.Layer
	m.layerStart = now
	m.layerElapsed = 0
	m.tasks = make(map[string]*taskState)
	m.taskOrder = nil
	m.completedOrder = nil

	return m, nil
}

// handleStart processes an EventStart event.
func (m *ApplyModel) handleStart(event engine.Event) (tea.Model, tea.Cmd) {
	key := taskKey(event.Kind, event.Name)
	if _, exists := m.tasks[key]; exists {
		return m, nil
	}

	m.tasks[key] = &taskState{
		key:       key,
		kind:      event.Kind,
		name:      event.Name,
		version:   event.Version,
		method:    event.Method,
		action:    event.Action,
		status:    taskRunning,
		startTime: time.Now(),
	}
	m.taskOrder = append(m.taskOrder, key)

	return m, nil
}

// handleProgress processes an EventProgress event.
func (m *ApplyModel) handleProgress(event engine.Event) (tea.Model, tea.Cmd) {
	key := taskKey(event.Kind, event.Name)
	task, exists := m.tasks[key]
	if !exists {
		return m, nil
	}

	task.downloaded = event.Downloaded
	task.total = event.Total
	task.hasProgress = true

	return m, nil
}

// handleOutput processes an EventOutput event.
func (m *ApplyModel) handleOutput(event engine.Event) (tea.Model, tea.Cmd) {
	key := taskKey(event.Kind, event.Name)
	task, exists := m.tasks[key]
	if !exists {
		return m, nil
	}

	task.logLines = append(task.logLines, event.Output)
	if len(task.logLines) > maxLogLines {
		task.logLines = task.logLines[len(task.logLines)-maxLogLines:]
	}

	return m, nil
}

// handleComplete processes an EventComplete event.
func (m *ApplyModel) handleComplete(event engine.Event) (tea.Model, tea.Cmd) {
	key := taskKey(event.Kind, event.Name)
	task, exists := m.tasks[key]
	if !exists {
		return m, nil
	}

	task.status = taskDone
	task.elapsed = time.Since(task.startTime)
	task.installPath = event.InstallPath
	updateResults(event.Action, m.results)
	m.completedOrder = append(m.completedOrder, key)

	return m, nil
}

// handleError processes an EventError event.
func (m *ApplyModel) handleError(event engine.Event) (tea.Model, tea.Cmd) {
	key := taskKey(event.Kind, event.Name)
	task, exists := m.tasks[key]
	if !exists {
		return m, nil
	}

	task.status = taskFailed
	task.elapsed = time.Since(task.startTime)
	task.err = event.Error
	m.results.Failed++
	m.completedOrder = append(m.completedOrder, key)

	return m, nil
}

// handleSlogMsg appends a slog record to the log panel, keeping at most maxSlogLines.
func (m *ApplyModel) handleSlogMsg(msg slogMsg) (tea.Model, tea.Cmd) {
	m.slogLines = append(m.slogLines, slogLine(msg))
	if len(m.slogLines) > maxSlogLines {
		m.slogLines = m.slogLines[len(m.slogLines)-maxSlogLines:]
	}
	return m, nil
}

// handleApplyDone processes an applyDoneMsg.
func (m *ApplyModel) handleApplyDone(msg applyDoneMsg) (tea.Model, tea.Cmd) {
	// Snapshot the final layer
	m.snapshotCurrentLayer()

	m.done = true
	m.err = msg.err

	return m, tea.Quit
}

// snapshotCurrentLayer saves the current layer's state for later rendering by View().
// It deep-copies the tasks map so the snapshot is immutable and not affected by
// subsequent modifications to the live tasks map.
func (m *ApplyModel) snapshotCurrentLayer() {
	if m.totalLayers == 0 {
		return
	}

	// Deep copy tasks to prevent the snapshot from sharing mutable state
	// with the live layer. Each taskState is copied by value (except slices,
	// which are copied to new backing arrays).
	copiedTasks := make(map[string]*taskState, len(m.tasks))
	for k, v := range m.tasks {
		copied := *v
		if len(v.logLines) > 0 {
			copied.logLines = make([]string, len(v.logLines))
			copy(copied.logLines, v.logLines)
		}
		copiedTasks[k] = &copied
	}

	copiedOrder := make([]string, len(m.taskOrder))
	copy(copiedOrder, m.taskOrder)

	copiedCompletedOrder := make([]string, len(m.completedOrder))
	copy(copiedCompletedOrder, m.completedOrder)

	snapshot := &layerState{
		elapsed:        m.layerElapsed,
		tasks:          copiedTasks,
		taskOrder:      copiedOrder,
		completedOrder: copiedCompletedOrder,
	}
	m.completedLayers = append(m.completedLayers, snapshot)
}

// taskKey returns the display key for a task, e.g. "Tool/bat".
func taskKey(kind resource.Kind, name string) string {
	return fmt.Sprintf("%s/%s", kind, name)
}
