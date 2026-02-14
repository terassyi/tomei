package ui

import (
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/stretchr/testify/assert"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/resource"
)

// enableColorForTest forces lipgloss to emit ANSI escape sequences during tests
// (by default lipgloss detects no TTY and strips colors).
// It returns a cleanup function that restores the original profile.
func enableColorForTest(t *testing.T) {
	t.Helper()
	orig := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(orig) })
}

// containsANSI returns true if the string contains ANSI escape sequences.
func containsANSI(s string) bool {
	return strings.Contains(s, "\x1b[")
}

func TestFormatElapsed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{name: "zero", d: 0, want: "0.0s"},
		{name: "sub-second", d: 300 * time.Millisecond, want: "0.3s"},
		{name: "seconds", d: 4400 * time.Millisecond, want: "4.4s"},
		{name: "large", d: 31600 * time.Millisecond, want: "31.6s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, formatElapsed(tt.d))
		})
	}
}

func TestFormatSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		bytes int64
		want  string
	}{
		{name: "zero", bytes: 0, want: "0 B"},
		{name: "bytes", bytes: 512, want: "512 B"},
		{name: "KiB", bytes: 500 * 1024, want: "500.0 KiB"},
		{name: "MiB", bytes: 12300000, want: "11.7 MiB"},
		{name: "GiB", bytes: 2 * 1024 * 1024 * 1024, want: "2.0 GiB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, formatSize(tt.bytes))
		})
	}
}

func TestFitNodeNames(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		names    []string
		maxWidth int
		want     string
	}{
		{
			name:     "empty",
			names:    []string{},
			maxWidth: 80,
			want:     "",
		},
		{
			name:     "single",
			names:    []string{"Runtime/go"},
			maxWidth: 80,
			want:     "Runtime/go",
		},
		{
			name:     "multiple fits",
			names:    []string{"Tool/bat", "Tool/rg", "Tool/gopls"},
			maxWidth: 80,
			want:     "Tool/bat, Tool/rg, Tool/gopls",
		},
		{
			name:     "truncated",
			names:    []string{"Tool/bat", "Tool/rg", "Tool/gopls", "Tool/fd", "Tool/jq"},
			maxWidth: 40,
			want:     "Tool/bat, Tool/rg, ... +3 more",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := fitNodeNames(tt.names, tt.maxWidth)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestRenderProgressBar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		downloaded int64
		total      int64
		wantFull   int
		wantEmpty  int
	}{
		{name: "zero", downloaded: 0, total: 0, wantFull: 0, wantEmpty: progressBarWidth},
		{name: "half", downloaded: 50, total: 100, wantFull: 10, wantEmpty: 10},
		{name: "complete", downloaded: 100, total: 100, wantFull: 20, wantEmpty: 0},
		{name: "quarter", downloaded: 25, total: 100, wantFull: 5, wantEmpty: 15},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			bar := renderProgressBar(tt.downloaded, tt.total)
			full := strings.Count(bar, string(progressFull))
			empty := strings.Count(bar, string(progressEmpty))
			assert.Equal(t, tt.wantFull, full, "filled blocks")
			assert.Equal(t, tt.wantEmpty, empty, "empty blocks")
			assert.Equal(t, progressBarWidth, full+empty, "total width")
		})
	}
}

func TestRenderLayerHeader(t *testing.T) {
	t.Parallel()
	header := renderLayerHeader(0, 2, []string{"Runtime/go"}, "4.4s", 80)
	assert.Contains(t, header, "Layer 1/2: Runtime/go")
	assert.Contains(t, header, "4.4s")
}

func TestRenderLayerHeader_Styled(t *testing.T) {
	t.Parallel()
	enableColorForTest(t)

	header := renderLayerHeader(0, 2, []string{"Runtime/go"}, "4.4s", 80)
	assert.Contains(t, header, "Layer 1/2: Runtime/go")
	assert.Contains(t, header, "4.4s")
	assert.True(t, containsANSI(header), "layer header should contain ANSI escape sequences for cyan styling")
}

func TestRenderCompletedLine(t *testing.T) {
	t.Parallel()
	task := &taskState{
		kind:        resource.KindTool,
		name:        "bat",
		version:     "0.25.0",
		method:      "download",
		action:      resource.ActionInstall,
		installPath: "/home/user/.local/bin/bat",
	}
	line := renderCompletedLine(task, 900*time.Millisecond, 80)
	assert.Contains(t, line, "bat")
	assert.Contains(t, line, "0.25.0")
	assert.Contains(t, line, "installed to")
	assert.Contains(t, line, "0.9s")
}

func TestRenderFailedLine(t *testing.T) {
	t.Parallel()
	task := &taskState{
		kind:    resource.KindTool,
		name:    "bat",
		version: "0.25.0",
		err:     fmt.Errorf("connection refused"),
	}
	line := renderFailedLine(task, 300*time.Millisecond, 80)
	assert.Contains(t, line, "bat")
	assert.Contains(t, line, "failed: connection refused")
	assert.Contains(t, line, "0.3s")
}

func TestRenderProgressLine(t *testing.T) {
	t.Parallel()
	task := &taskState{
		kind:        resource.KindRuntime,
		name:        "go",
		version:     "1.25.6",
		method:      "download",
		hasProgress: true,
		downloaded:  12_000_000,
		total:       95_000_000,
	}
	line := renderProgressLine(task, 300*time.Millisecond, 80)
	assert.Contains(t, line, "=>")
	assert.Contains(t, line, "Runtime/go")
	assert.Contains(t, line, "1.25.6")
	assert.Contains(t, line, "MiB")
}

func TestRenderDelegationLines(t *testing.T) {
	t.Parallel()
	task := &taskState{
		kind:      resource.KindTool,
		name:      "gopls",
		version:   "0.21.0",
		method:    "go install",
		startTime: time.Now(),
		logLines: []string{
			"go: downloading golang.org/x/tools/gopls v0.21.0",
			"go: downloading golang.org/x/tools v0.31.0",
		},
	}
	lines := renderDelegationLines(task, 500*time.Millisecond, 80)
	assert.Len(t, lines, 3, "1 header + 2 log lines")
	assert.Contains(t, lines[0], "=>")
	assert.Contains(t, lines[0], "Tool/gopls")
	assert.Contains(t, lines[0], "go install")
	assert.Contains(t, lines[1], "go: downloading golang.org/x/tools/gopls")
	assert.Contains(t, lines[2], "go: downloading golang.org/x/tools v0.31.0")
}

func TestRenderDelegationLines_Styled(t *testing.T) {
	t.Parallel()
	enableColorForTest(t)

	task := &taskState{
		kind:      resource.KindTool,
		name:      "gopls",
		version:   "0.21.0",
		method:    "go install",
		startTime: time.Now(),
		logLines: []string{
			"go: downloading golang.org/x/tools/gopls v0.21.0",
			"go: downloading golang.org/x/tools v0.31.0",
		},
	}
	lines := renderDelegationLines(task, 500*time.Millisecond, 80)
	assert.True(t, containsANSI(lines[1]), "delegation log line 1 should contain ANSI escape sequences for gray styling")
	assert.True(t, containsANSI(lines[2]), "delegation log line 2 should contain ANSI escape sequences for gray styling")
}

func TestTaskLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		task *taskState
		want string
	}{
		{
			name: "download method omitted",
			task: &taskState{kind: resource.KindTool, name: "bat", version: "0.25.0", method: "download"},
			want: "Tool/bat 0.25.0",
		},
		{
			name: "delegation method shown",
			task: &taskState{kind: resource.KindTool, name: "gopls", version: "0.21.0", method: "go install"},
			want: "Tool/gopls 0.21.0 (go install)",
		},
		{
			name: "aqua install shown",
			task: &taskState{kind: resource.KindTool, name: "rg", version: "15.1.0", method: "aqua install"},
			want: "Tool/rg 15.1.0 (aqua install)",
		},
		{
			name: "no version",
			task: &taskState{kind: resource.KindRuntime, name: "go"},
			want: "Runtime/go",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, taskLabel(tt.task))
		})
	}
}

func TestView_ShowsCurrentLayerAndPendingHeaders(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Initialize with 2 layers
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 2,
		LayerNodes:    []string{"Runtime/go"},
		AllLayerNodes: [][]string{{"Runtime/go"}, {"Tool/bat", "Tool/rg"}},
	}})

	view := m.View()
	assert.Contains(t, view, "Layer 1/2: Runtime/go")
	assert.Contains(t, view, "Layer 2/2: Tool/bat, Tool/rg")
	assert.Contains(t, view, "-", "pending layer should show dash")
	assert.Contains(t, view, "Elapsed:")
}

func TestView_OnlyRendersStartedTasks(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Initialize layer with 2 nodes
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes:    []string{"Tool/bat", "Tool/rg"},
		AllLayerNodes: [][]string{{"Tool/bat", "Tool/rg"}},
	}})

	// Start only bat
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "bat", Version: "0.25.0",
	}})

	view := m.View()
	// Layer header contains both names (correct)
	assert.Contains(t, view, "Layer 1/1: Tool/bat, Tool/rg")
	// Only bat has a task line (with "=>")
	lines := strings.Split(view, "\n")
	taskLines := 0
	for _, line := range lines {
		if strings.Contains(line, "=>") {
			taskLines++
			assert.Contains(t, line, "Tool/bat", "running task line should be for bat")
		}
	}
	assert.Equal(t, 1, taskLines, "only one task line should be rendered")
}

func TestView_RendersFinalStateWhenDone(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes:    []string{"Tool/bat"},
		AllLayerNodes: [][]string{{"Tool/bat"}},
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "bat", Version: "0.25.0",
		Method: "download", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindTool, Name: "bat",
		Action: resource.ActionInstall, InstallPath: "/home/user/.local/bin/bat",
	}})
	m.Update(applyDoneMsg{err: nil})

	view := m.View()
	// Final frame should still contain the completed layer info
	assert.Contains(t, view, "Layer 1/1: Tool/bat")
	assert.Contains(t, view, doneMark)
	assert.Contains(t, view, "Elapsed:")
}

func TestView_EmptyBeforeFirstEvent(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	assert.Empty(t, m.View(), "View should be empty before any events")
}

func TestView_DelegationLogLinesWhileRunning(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Initialize with 1 layer containing a delegation task
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes:    []string{"Tool/gopls"},
		AllLayerNodes: [][]string{{"Tool/gopls"}},
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type:    engine.EventStart,
		Kind:    resource.KindTool,
		Name:    "gopls",
		Version: "0.21.0",
		Method:  "go install",
		Action:  resource.ActionInstall,
	}})

	// Send output lines
	m.Update(engineEventMsg{event: engine.Event{
		Type:   engine.EventOutput,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Output: "go: downloading golang.org/x/tools/gopls v0.21.0",
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type:   engine.EventOutput,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Output: "go: downloading golang.org/x/tools v0.31.0",
	}})

	view := m.View()
	assert.Contains(t, view, "Layer 1/1: Tool/gopls")
	assert.Contains(t, view, "Tool/gopls 0.21.0 (go install)")
	assert.Contains(t, view, "go: downloading golang.org/x/tools/gopls v0.21.0",
		"delegation log line 1 should appear in view")
	assert.Contains(t, view, "go: downloading golang.org/x/tools v0.31.0",
		"delegation log line 2 should appear in view")
}

func TestView_DelegationLogLinesClearedAfterCompletion(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes:    []string{"Tool/gopls"},
		AllLayerNodes: [][]string{{"Tool/gopls"}},
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type:    engine.EventStart,
		Kind:    resource.KindTool,
		Name:    "gopls",
		Version: "0.21.0",
		Method:  "go install",
		Action:  resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type:   engine.EventOutput,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Output: "go: downloading golang.org/x/tools/gopls v0.21.0",
	}})

	// Complete the task
	m.Update(engineEventMsg{event: engine.Event{
		Type:   engine.EventComplete,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Action: resource.ActionInstall,
	}})

	view := m.View()
	assert.Contains(t, view, doneMark, "completed task should show done mark")
	assert.NotContains(t, view, "go: downloading",
		"delegation log lines should be cleared after completion")
}

func TestView_DelegationLogLinesClearedInFinalSnapshot(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes:    []string{"Tool/gopls"},
		AllLayerNodes: [][]string{{"Tool/gopls"}},
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type:    engine.EventStart,
		Kind:    resource.KindTool,
		Name:    "gopls",
		Version: "0.21.0",
		Method:  "go install",
		Action:  resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type:   engine.EventOutput,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Output: "go: downloading golang.org/x/tools/gopls v0.21.0",
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type:   engine.EventComplete,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Action: resource.ActionInstall,
	}})

	// Apply done triggers snapshot
	m.Update(applyDoneMsg{err: nil})

	view := m.View()
	assert.Contains(t, view, doneMark, "completed delegation task should show done mark in final view")
	assert.NotContains(t, view, "go: downloading",
		"delegation log lines should be cleared in final snapshot")
}

func TestView_NoLayerHeaderDuplication_TwoLayers(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	allNodes := [][]string{{"Runtime/go"}, {"Tool/bat", "Tool/rg"}}

	// Layer 0 start
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 2,
		LayerNodes: []string{"Runtime/go"}, AllLayerNodes: allNodes,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindRuntime, Name: "go",
		Version: "1.25.0", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindRuntime, Name: "go",
		Action: resource.ActionInstall, InstallPath: "/runtimes/go",
	}})

	// Layer 1 start (should snapshot layer 0)
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 1, TotalLayers: 2,
		LayerNodes: []string{"Tool/bat", "Tool/rg"}, AllLayerNodes: allNodes,
	}})

	view := m.View()
	// Count occurrences of each layer header
	layer1Count := strings.Count(view, "Layer 1/2")
	layer2Count := strings.Count(view, "Layer 2/2")
	assert.Equal(t, 1, layer1Count, "Layer 1/2 header should appear exactly once")
	assert.Equal(t, 1, layer2Count, "Layer 2/2 header should appear exactly once")
}

func TestView_NoLayerHeaderDuplication_AfterDone(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	allNodes := [][]string{{"Runtime/go"}, {"Tool/bat"}}

	// Layer 0
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 2,
		LayerNodes: []string{"Runtime/go"}, AllLayerNodes: allNodes,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindRuntime, Name: "go",
		Version: "1.25.0", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindRuntime, Name: "go",
		Action: resource.ActionInstall,
	}})

	// Layer 1
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 1, TotalLayers: 2,
		LayerNodes: []string{"Tool/bat"}, AllLayerNodes: allNodes,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "bat",
		Version: "0.25.0", Method: "download", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindTool, Name: "bat",
		Action: resource.ActionInstall, InstallPath: "/bin/bat",
	}})

	// Apply done
	m.Update(applyDoneMsg{err: nil})

	view := m.View()
	layer1Count := strings.Count(view, "Layer 1/2")
	layer2Count := strings.Count(view, "Layer 2/2")
	assert.Equal(t, 1, layer1Count, "Layer 1/2 header should appear exactly once in final view")
	assert.Equal(t, 1, layer2Count, "Layer 2/2 header should appear exactly once in final view")
}

func TestView_NoLayerHeaderDuplication_ThreeLayers(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	allNodes := [][]string{{"Runtime/go"}, {"Tool/gopls"}, {"Tool/bat", "Tool/rg"}}

	// Layer 0
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 3,
		LayerNodes: []string{"Runtime/go"}, AllLayerNodes: allNodes,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindRuntime, Name: "go",
		Version: "1.25.0", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindRuntime, Name: "go",
		Action: resource.ActionInstall,
	}})

	// Layer 1
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 1, TotalLayers: 3,
		LayerNodes: []string{"Tool/gopls"}, AllLayerNodes: allNodes,
	}})

	// Check intermediate state: layer 0 completed, layer 1 current, layer 2 pending
	view := m.View()
	assert.Equal(t, 1, strings.Count(view, "Layer 1/3"), "Layer 1/3 should appear once")
	assert.Equal(t, 1, strings.Count(view, "Layer 2/3"), "Layer 2/3 should appear once")
	assert.Equal(t, 1, strings.Count(view, "Layer 3/3"), "Layer 3/3 should appear once")

	// Layer 2
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "gopls",
		Version: "0.21.0", Method: "go install", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindTool, Name: "gopls",
		Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 2, TotalLayers: 3,
		LayerNodes: []string{"Tool/bat", "Tool/rg"}, AllLayerNodes: allNodes,
	}})

	view = m.View()
	assert.Equal(t, 1, strings.Count(view, "Layer 1/3"), "Layer 1/3 should appear once after layer 2 start")
	assert.Equal(t, 1, strings.Count(view, "Layer 2/3"), "Layer 2/3 should appear once after layer 2 start")
	assert.Equal(t, 1, strings.Count(view, "Layer 3/3"), "Layer 3/3 should appear once after layer 2 start")

	// Apply done
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "bat",
		Version: "0.25.0", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindTool, Name: "bat",
		Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "rg",
		Version: "15.1.0", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindTool, Name: "rg",
		Action: resource.ActionInstall,
	}})
	m.Update(applyDoneMsg{err: nil})

	view = m.View()
	assert.Equal(t, 1, strings.Count(view, "Layer 1/3"), "Layer 1/3 should appear once in final view")
	assert.Equal(t, 1, strings.Count(view, "Layer 2/3"), "Layer 2/3 should appear once in final view")
	assert.Equal(t, 1, strings.Count(view, "Layer 3/3"), "Layer 3/3 should appear once in final view")
}

func TestLayerHeaderStyle(t *testing.T) {
	t.Parallel()
	enableColorForTest(t)

	input := "Layer 1/2: Runtime/go"
	styled := layerHeaderStyle.Render(input)
	assert.Contains(t, styled, input, "styled text should contain the original content")
	assert.Contains(t, styled, "\x1b[", "styled text should contain ANSI escape sequences")
	assert.Greater(t, len(styled), len(input), "styled text should be longer than plain text due to ANSI codes")
}

func TestDelegationLogStyle(t *testing.T) {
	t.Parallel()
	enableColorForTest(t)

	input := "go: downloading golang.org/x/tools v0.31.0"
	styled := delegationLogStyle.Render(input)
	assert.Contains(t, styled, input, "styled text should contain the original content")
	assert.Contains(t, styled, "\x1b[", "styled text should contain ANSI escape sequences")
	assert.Greater(t, len(styled), len(input), "styled text should be longer than plain text due to ANSI codes")
}

func TestRenderTaskList_CompletedDelegationNoLogs(t *testing.T) {
	t.Parallel()
	tasks := map[string]*taskState{
		"Tool/gopls": {
			key:     "Tool/gopls",
			kind:    resource.KindTool,
			name:    "gopls",
			version: "0.21.0",
			method:  "go install",
			action:  resource.ActionInstall,
			status:  taskDone,
			elapsed: 3 * time.Second,
			logLines: []string{
				"go: downloading golang.org/x/tools/gopls v0.21.0",
				"go: downloading golang.org/x/tools v0.31.0",
			},
		},
	}
	taskOrder := []string{"Tool/gopls"}
	completedOrder := []string{"Tool/gopls"}

	var b strings.Builder
	renderTaskList(&b, tasks, taskOrder, completedOrder, 80)
	output := b.String()

	// Completed delegation tasks should show the completion line but not log lines
	assert.Contains(t, output, "gopls")
	assert.NotContains(t, output, "go: downloading",
		"completed delegation tasks should not show log lines")
}

func TestView_DelegationLogLinesStyledWhileRunning(t *testing.T) {
	t.Parallel()
	enableColorForTest(t)

	results := &ApplyResults{}
	m := NewApplyModel(results)

	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes:    []string{"Tool/gopls"},
		AllLayerNodes: [][]string{{"Tool/gopls"}},
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

	view := m.View()

	// Find the delegation log line and verify it has ANSI styling
	for line := range strings.SplitSeq(view, "\n") {
		if strings.Contains(line, "go: downloading") {
			assert.True(t, containsANSI(line),
				"running delegation log line should be styled with gray ANSI codes")
			return
		}
	}
	t.Fatal("delegation log line not found in view")
}

func TestView_DelegationLogLinesClearedAfterCompletionView(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes:    []string{"Tool/gopls"},
		AllLayerNodes: [][]string{{"Tool/gopls"}},
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
		Type:   engine.EventComplete,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Action: resource.ActionInstall,
	}})

	view := m.View()
	assert.Contains(t, view, doneMark, "completed task should show done mark")
	assert.NotContains(t, view, "go: downloading",
		"delegation log lines should not appear after completion")
}

func TestView_SnapshotClearsDelegationLogLines(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	allNodes := [][]string{{"Tool/gopls"}, {"Tool/bat"}}

	// Layer 0: delegation task with output
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 2,
		LayerNodes: []string{"Tool/gopls"}, AllLayerNodes: allNodes,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type:    engine.EventStart,
		Kind:    resource.KindTool,
		Name:    "gopls",
		Version: "0.21.0",
		Method:  "go install",
		Action:  resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type:   engine.EventOutput,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Output: "go: downloading golang.org/x/tools/gopls v0.21.0",
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type:   engine.EventComplete,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Action: resource.ActionInstall,
	}})

	// Layer 1: triggers snapshot of layer 0
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 1, TotalLayers: 2,
		LayerNodes: []string{"Tool/bat"}, AllLayerNodes: allNodes,
	}})

	// Completed delegation tasks should not show log lines in snapshot
	view := m.View()
	assert.Contains(t, view, doneMark, "snapshot should show done mark")
	assert.NotContains(t, view, "go: downloading",
		"delegation log lines should be cleared in layer snapshot")
}

func TestView_CompletedTasksRenderedBeforeRunning(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Layer with 3 tools
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes:    []string{"Tool/bat", "Tool/rg", "Tool/gopls"},
		AllLayerNodes: [][]string{{"Tool/bat", "Tool/rg", "Tool/gopls"}},
	}})

	// Start all three (bat first, then rg, then gopls)
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "bat",
		Version: "0.26.1", Method: "aqua install", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "rg",
		Version: "15.1.0", Method: "aqua install", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "gopls",
		Version: "0.21.0", Method: "go install", Action: resource.ActionInstall,
	}})

	// Complete rg first (out of start order), then bat
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindTool, Name: "rg",
		Action: resource.ActionInstall, InstallPath: "/bin/rg",
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindTool, Name: "bat",
		Action: resource.ActionInstall, InstallPath: "/bin/bat",
	}})

	// gopls is still running
	view := m.View()
	lines := strings.Split(view, "\n")

	// Find positions of task lines
	rgPos, batPos, goplsPos := -1, -1, -1
	for i, line := range lines {
		switch {
		case strings.Contains(line, "Tool/rg"):
			rgPos = i
		case strings.Contains(line, "Tool/bat"):
			if !strings.Contains(line, "Layer") { // skip layer header
				batPos = i
			}
		case strings.Contains(line, "Tool/gopls"):
			if !strings.Contains(line, "Layer") {
				goplsPos = i
			}
		}
	}

	assert.Greater(t, batPos, rgPos, "rg completed first so should appear before bat")
	assert.Greater(t, goplsPos, batPos, "gopls is still running so should appear after completed tasks")
}

func TestRenderLogPanel_Empty(t *testing.T) {
	t.Parallel()
	var b strings.Builder
	renderLogPanel(&b, nil, 80)
	assert.Empty(t, b.String(), "log panel should not render when there are no log lines")
}

func TestRenderLogPanel_WithLines(t *testing.T) {
	t.Parallel()
	lines := []slogLine{
		{level: slog.LevelWarn, message: "backup failed"},
		{level: slog.LevelError, message: "state write error"},
	}
	var b strings.Builder
	renderLogPanel(&b, lines, 80)
	output := b.String()

	assert.Contains(t, output, "Logs")
	assert.Contains(t, output, "WARN")
	assert.Contains(t, output, "backup failed")
	assert.Contains(t, output, "ERROR")
	assert.Contains(t, output, "state write error")
}

func TestRenderLogPanel_Styled(t *testing.T) {
	t.Parallel()
	enableColorForTest(t)

	lines := []slogLine{
		{level: slog.LevelWarn, message: "warn msg"},
		{level: slog.LevelError, message: "error msg"},
	}
	var b strings.Builder
	renderLogPanel(&b, lines, 80)
	output := b.String()

	assert.True(t, containsANSI(output), "log panel should contain ANSI escape sequences for colored output")
}

func TestSlogLevelLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		level slog.Level
		want  string
	}{
		{slog.LevelDebug, "DEBUG"},
		{slog.LevelInfo, "INFO"},
		{slog.LevelWarn, "WARN"},
		{slog.LevelError, "ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			t.Parallel()
			label := slogLevelLabel(tt.level)
			assert.Contains(t, label, tt.want)
		})
	}
}

func TestView_LogPanelShownWithSlogLines(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	// Initialize a layer
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes:    []string{"Tool/bat"},
		AllLayerNodes: [][]string{{"Tool/bat"}},
	}})

	// Add a slog message
	m.Update(slogMsg{level: slog.LevelWarn, message: "registry warning"})

	view := m.View()
	assert.Contains(t, view, "Logs")
	assert.Contains(t, view, "WARN")
	assert.Contains(t, view, "registry warning")
	assert.Contains(t, view, "Elapsed:")
}

func TestView_LogPanelHiddenWithoutSlogLines(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes:    []string{"Tool/bat"},
		AllLayerNodes: [][]string{{"Tool/bat"}},
	}})

	view := m.View()
	assert.NotContains(t, view, "Logs", "log panel should not appear when there are no slog lines")
}

func TestView_FinalViewIncludesLogPanel(t *testing.T) {
	t.Parallel()
	results := &ApplyResults{}
	m := NewApplyModel(results)

	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventLayerStart, Layer: 0, TotalLayers: 1,
		LayerNodes:    []string{"Tool/bat"},
		AllLayerNodes: [][]string{{"Tool/bat"}},
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventStart, Kind: resource.KindTool, Name: "bat",
		Version: "0.25.0", Method: "download", Action: resource.ActionInstall,
	}})
	m.Update(engineEventMsg{event: engine.Event{
		Type: engine.EventComplete, Kind: resource.KindTool, Name: "bat",
		Action: resource.ActionInstall, InstallPath: "/bin/bat",
	}})

	// Add slog messages
	m.Update(slogMsg{level: slog.LevelWarn, message: "backup warning"})
	m.Update(slogMsg{level: slog.LevelError, message: "critical error"})

	// Finish
	m.Update(applyDoneMsg{err: nil})

	view := m.FinalView()
	assert.Contains(t, view, "Logs")
	assert.Contains(t, view, "backup warning")
	assert.Contains(t, view, "critical error")
}
