package ui

import (
	"errors"
	"fmt"
	"log/slog"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/resource"
)

func TestUpdate_EventLayerStart_InitializesModel(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	event := engine.Event{
		Type:        engine.EventLayerStart,
		Layer:       0,
		TotalLayers: 2,
		LayerNodes:  []string{"Runtime/go"},
		AllLayerNodes: [][]string{
			{"Runtime/go"},
			{"Tool/bat", "Tool/rg"},
		},
	}

	updated, _ := m.Update(engineEventMsg{event: event})
	model := updated.(*ApplyModel)

	assert.Equal(t, 2, model.totalLayers)
	assert.Equal(t, 0, model.currentLayer)
	assert.Len(t, model.allLayerNodes, 2)
	assert.Equal(t, []string{"Runtime/go"}, model.allLayerNodes[0])
	assert.Equal(t, []string{"Tool/bat", "Tool/rg"}, model.allLayerNodes[1])
	assert.False(t, model.applyStart.IsZero(), "applyStart should be set")
}

func TestUpdate_EventLayerStart_SnapshotsOnSecondLayer(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// First layer
	event0 := engine.Event{
		Type:        engine.EventLayerStart,
		Layer:       0,
		TotalLayers: 2,
		LayerNodes:  []string{"Runtime/go"},
		AllLayerNodes: [][]string{
			{"Runtime/go"},
			{"Tool/bat"},
		},
	}
	updated, _ := m.Update(engineEventMsg{event: event0})
	m = updated.(*ApplyModel)

	// Add a completed task to current layer
	startEvent := engine.Event{
		Type:    engine.EventStart,
		Kind:    resource.KindRuntime,
		Name:    "go",
		Version: "1.25.0",
		Action:  resource.ActionInstall,
	}
	updated, _ = m.Update(engineEventMsg{event: startEvent})
	m = updated.(*ApplyModel)

	completeEvent := engine.Event{
		Type:        engine.EventComplete,
		Kind:        resource.KindRuntime,
		Name:        "go",
		Action:      resource.ActionInstall,
		InstallPath: "/runtimes/go",
	}
	updated, _ = m.Update(engineEventMsg{event: completeEvent})
	m = updated.(*ApplyModel)

	// Second layer start should snapshot the first layer
	event1 := engine.Event{
		Type:        engine.EventLayerStart,
		Layer:       1,
		TotalLayers: 2,
		LayerNodes:  []string{"Tool/bat"},
		AllLayerNodes: [][]string{
			{"Runtime/go"},
			{"Tool/bat"},
		},
	}
	updated, _ = m.Update(engineEventMsg{event: event1})
	model := updated.(*ApplyModel)

	assert.Equal(t, 1, model.currentLayer)
	require.Len(t, model.completedLayers, 1, "first layer should be snapshotted")
	assert.Contains(t, model.completedLayers[0].tasks, "Runtime/go")
	assert.Equal(t, taskDone, model.completedLayers[0].tasks["Runtime/go"].status)
	assert.Empty(t, model.tasks, "current layer tasks should be reset")
}

func TestUpdate_EventStart_CreatesTask(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Initialize layer first
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes: []string{"Tool/bat"}, AllLayerNodes: [][]string{{"Tool/bat"}},
	}})

	event := engine.Event{
		Type:    engine.EventStart,
		Kind:    resource.KindTool,
		Name:    "bat",
		Version: "0.25.0",
		Method:  "download",
		Action:  resource.ActionInstall,
	}
	updated, _ := m.Update(engineEventMsg{event: event})
	model := updated.(*ApplyModel)

	require.Contains(t, model.tasks, "Tool/bat")
	task := model.tasks["Tool/bat"]
	assert.Equal(t, "bat", task.name)
	assert.Equal(t, "0.25.0", task.version)
	assert.Equal(t, "download", task.method)
	assert.Equal(t, taskRunning, task.status)
	assert.False(t, task.hasProgress)
	assert.Equal(t, []string{"Tool/bat"}, model.taskOrder)
}

func TestUpdate_EventProgress_SetsHasProgress(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Setup: layer + task
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes: []string{"Tool/bat"}, AllLayerNodes: [][]string{{"Tool/bat"}},
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "bat", Version: "0.25.0",
	}})

	// Progress event
	event := engine.Event{
		Type:       engine.EventProgress,
		Kind:       resource.KindTool,
		Name:       "bat",
		Downloaded: 500000,
		Total:      3000000,
	}
	updated, _ := m.Update(engineEventMsg{event: event})
	model := updated.(*ApplyModel)

	task := model.tasks["Tool/bat"]
	assert.True(t, task.hasProgress, "hasProgress should be true after EventProgress")
	assert.Equal(t, int64(500000), task.downloaded)
	assert.Equal(t, int64(3000000), task.total)
}

func TestUpdate_EventProgress_IgnoredWithoutStart(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes: []string{"Tool/bat"}, AllLayerNodes: [][]string{{"Tool/bat"}},
	}})

	// Progress without Start should be ignored
	event := engine.Event{
		Type:       engine.EventProgress,
		Kind:       resource.KindTool,
		Name:       "bat",
		Downloaded: 500000,
		Total:      3000000,
	}
	updated, _ := m.Update(engineEventMsg{event: event})
	model := updated.(*ApplyModel)

	assert.Empty(t, model.tasks, "task should not be created by Progress event")
}

func TestUpdate_EventOutput_AddsLogLines(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Setup
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes: []string{"Tool/gopls"}, AllLayerNodes: [][]string{{"Tool/gopls"}},
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "gopls", Method: "go install",
	}})

	// Add log lines
	for i := range 7 {
		m.Update(engineEventMsg{event: engine.Event{
			Type:   engine.EventOutput,
			Kind:   resource.KindTool,
			Name:   "gopls",
			Output: fmt.Sprintf("line %d", i),
		}})
	}

	task := m.tasks["Tool/gopls"]
	require.NotNil(t, task)
	assert.Len(t, task.logLines, maxLogLines, "should keep only last %d lines", maxLogLines)
	assert.Equal(t, "line 2", task.logLines[0], "oldest visible line should be line 2")
	assert.Equal(t, "line 6", task.logLines[4], "newest line should be line 6")
}

func TestUpdate_EventComplete_UpdatesResults(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Setup
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes: []string{"Tool/bat"}, AllLayerNodes: [][]string{{"Tool/bat"}},
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "bat",
		Action: resource.ActionInstall,
	}})

	// Complete
	event := engine.Event{
		Type:        engine.EventComplete,
		Kind:        resource.KindTool,
		Name:        "bat",
		Action:      resource.ActionInstall,
		InstallPath: "/bin/bat",
	}
	m.Update(engineEventMsg{event: event})

	task := m.tasks["Tool/bat"]
	assert.Equal(t, taskDone, task.status)
	assert.Equal(t, "/bin/bat", task.installPath)
	assert.Equal(t, 1, results.Installed)
}

func TestUpdate_EventError_UpdatesResults(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Setup
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes: []string{"Tool/bat"}, AllLayerNodes: [][]string{{"Tool/bat"}},
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "bat",
	}})

	// Error
	event := engine.Event{
		Type:  engine.EventError,
		Kind:  resource.KindTool,
		Name:  "bat",
		Error: errors.New("connection refused"),
	}
	m.Update(engineEventMsg{event: event})

	task := m.tasks["Tool/bat"]
	assert.Equal(t, taskFailed, task.status)
	assert.Equal(t, 1, results.Failed)
}

func TestUpdate_ApplyDone_QuitsProgram(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Setup a layer
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes: []string{"Tool/bat"}, AllLayerNodes: [][]string{{"Tool/bat"}},
	}})

	updated, cmd := m.Update(applyDoneMsg{err: nil})
	model := updated.(*ApplyModel)

	assert.True(t, model.done)
	require.NoError(t, model.err)
	assert.NotNil(t, cmd, "should return quit command")
}

func TestUpdate_ApplyDone_WithError(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes: []string{"Tool/bat"}, AllLayerNodes: [][]string{{"Tool/bat"}},
	}})

	updated, _ := m.Update(applyDoneMsg{err: errors.New("apply failed")})
	model := updated.(*ApplyModel)

	assert.True(t, model.done)
	assert.EqualError(t, model.err, "apply failed")
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	model := updated.(*ApplyModel)

	assert.Equal(t, 120, model.width)
}

func TestUpdate_SnapshotDeepCopy(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Layer 0 with a delegation task
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 2,
		LayerNodes:    []string{"Tool/gopls"},
		AllLayerNodes: [][]string{{"Tool/gopls"}, {"Tool/bat"}},
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "gopls",
		Version: "0.21.0", Method: "go install", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type:   engine.EventOutput,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Output: "go: downloading golang.org/x/tools/gopls v0.21.0",
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindTool, Name: "gopls",
		Action: resource.ActionInstall,
	}})

	// Layer 1 start triggers snapshot of layer 0
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 1, TotalLayers: 2,
		LayerNodes:    []string{"Tool/bat"},
		AllLayerNodes: [][]string{{"Tool/gopls"}, {"Tool/bat"}},
	}})

	// Verify snapshot is independent of live state
	require.Len(t, m.completedLayers, 1)
	snapshotTask := m.completedLayers[0].tasks["Tool/gopls"]
	require.NotNil(t, snapshotTask)
	assert.Len(t, snapshotTask.logLines, 1)
	assert.Equal(t, "go: downloading golang.org/x/tools/gopls v0.21.0", snapshotTask.logLines[0])

	// Verify current layer is empty (no tasks from layer 0 leaked)
	assert.Empty(t, m.tasks)
	assert.Empty(t, m.taskOrder)
}

func TestUpdate_SlogMsg_AppendsToSlogLines(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	updated, _ := m.Update(slogMsg{level: slog.LevelWarn, message: "warning one"})
	model := updated.(*ApplyModel)

	require.Len(t, model.slogLines, 1)
	assert.Equal(t, slog.LevelWarn, model.slogLines[0].level)
	assert.Equal(t, "warning one", model.slogLines[0].message)

	updated, _ = model.Update(slogMsg{level: slog.LevelError, message: "error one"})
	model = updated.(*ApplyModel)

	require.Len(t, model.slogLines, 2)
	assert.Equal(t, slog.LevelError, model.slogLines[1].level)
}

func TestUpdate_SlogMsg_TruncatesAtMaxSlogLines(t *testing.T) {
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Send more than maxSlogLines
	for i := range maxSlogLines + 3 {
		m.Update(slogMsg{level: slog.LevelWarn, message: fmt.Sprintf("msg %d", i)})
	}

	assert.Len(t, m.slogLines, maxSlogLines, "should keep only last %d lines", maxSlogLines)
	assert.Equal(t, "msg 3", m.slogLines[0].message, "oldest visible should be msg 3")
	assert.Equal(t, fmt.Sprintf("msg %d", maxSlogLines+2), m.slogLines[maxSlogLines-1].message)
}
