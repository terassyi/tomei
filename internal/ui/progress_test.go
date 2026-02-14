package ui

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/vbauerster/mpb/v8"
)

// newNonTTYProgressManager creates a ProgressManager that behaves as non-TTY for testing.
func newNonTTYProgressManager(w *bytes.Buffer) *ProgressManager {
	return &ProgressManager{
		w:       w,
		isTTY:   false,
		bars:    make(map[string]*mpb.Bar),
		cmdView: NewCommandView(w),
	}
}

func TestProgressManager_HandleEvent_DownloadStart_NonTTY(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pm := newNonTTYProgressManager(&buf)

	pm.HandleEvent(engine.Event{
		Type:    engine.EventStart,
		Kind:    resource.KindTool,
		Name:    "rg",
		Version: "14.1.1",
		Action:  resource.ActionInstall,
		Method:  "download",
	}, &ApplyResults{})

	output := buf.String()
	assert.Contains(t, output, "Downloads:")
	assert.Contains(t, output, "rg")
	assert.Contains(t, output, "14.1.1")
}

func TestProgressManager_HandleEvent_DownloadHeaderOnce_NonTTY(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pm := newNonTTYProgressManager(&buf)
	results := &ApplyResults{}

	// Send 3 download start events
	for _, name := range []string{"rg", "fd", "bat"} {
		pm.HandleEvent(engine.Event{
			Type:    engine.EventStart,
			Kind:    resource.KindTool,
			Name:    name,
			Version: "1.0.0",
			Action:  resource.ActionInstall,
			Method:  "download",
		}, results)
	}

	output := buf.String()
	assert.Equal(t, 1, strings.Count(output, "Downloads:"), "Downloads: header should appear exactly once")
}

func TestProgressManager_HandleEvent_CommandLifecycle_NonTTY(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pm := newNonTTYProgressManager(&buf)
	results := &ApplyResults{}

	// Start
	pm.HandleEvent(engine.Event{
		Type:    engine.EventStart,
		Kind:    resource.KindTool,
		Name:    "gopls",
		Version: "0.17.0",
		Action:  resource.ActionInstall,
		Method:  "go install",
	}, results)

	// Output
	pm.HandleEvent(engine.Event{
		Type:   engine.EventOutput,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Output: "go: downloading golang.org/x/tools v0.28.0",
		Method: "go install",
	}, results)

	// Complete
	pm.HandleEvent(engine.Event{
		Type:    engine.EventComplete,
		Kind:    resource.KindTool,
		Name:    "gopls",
		Version: "0.17.0",
		Action:  resource.ActionInstall,
		Method:  "go install",
	}, results)

	output := buf.String()
	assert.Contains(t, output, "Commands:")
	assert.Contains(t, output, "gopls")
	assert.Contains(t, output, "go install")
	assert.Contains(t, output, "go: downloading golang.org/x/tools v0.28.0")
	assert.Contains(t, output, "done")
	assert.Equal(t, 1, results.Installed)
}

func TestProgressManager_HandleEvent_CommandError_NonTTY(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pm := newNonTTYProgressManager(&buf)
	results := &ApplyResults{}

	pm.HandleEvent(engine.Event{
		Type:   engine.EventStart,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Method: "go install",
	}, results)

	pm.HandleEvent(engine.Event{
		Type:   engine.EventError,
		Kind:   resource.KindTool,
		Name:   "gopls",
		Method: "go install",
		Error:  fmt.Errorf("build failed"),
	}, results)

	output := buf.String()
	assert.Contains(t, output, "failed")
	assert.Equal(t, 1, results.Failed)
}

func TestProgressManager_HandleEvent_DownloadError_NonTTY(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pm := newNonTTYProgressManager(&buf)
	results := &ApplyResults{}

	pm.HandleEvent(engine.Event{
		Type:   engine.EventStart,
		Kind:   resource.KindTool,
		Name:   "rg",
		Method: "download",
	}, results)

	pm.HandleEvent(engine.Event{
		Type:   engine.EventError,
		Kind:   resource.KindTool,
		Name:   "rg",
		Method: "download",
		Error:  fmt.Errorf("404 not found"),
	}, results)

	output := buf.String()
	assert.Contains(t, output, "failed")
	assert.Contains(t, output, "404 not found")
	assert.Equal(t, 1, results.Failed)
}

func TestProgressManager_UpdateResults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		action    resource.ActionType
		wantField string
	}{
		{"install", resource.ActionInstall, "Installed"},
		{"reinstall", resource.ActionReinstall, "Installed"},
		{"upgrade", resource.ActionUpgrade, "Upgraded"},
		{"remove", resource.ActionRemove, "Removed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			results := &ApplyResults{}
			updateResults(tt.action, results)
			switch tt.wantField {
			case "Installed":
				assert.Equal(t, 1, results.Installed)
			case "Upgraded":
				assert.Equal(t, 1, results.Upgraded)
			case "Removed":
				assert.Equal(t, 1, results.Removed)
			}
		})
	}
}

func TestProgressManager_HandleEvent_MixedDownloadAndCommand_NonTTY(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pm := newNonTTYProgressManager(&buf)
	results := &ApplyResults{}

	// Download tool
	pm.HandleEvent(engine.Event{
		Type:    engine.EventStart,
		Kind:    resource.KindTool,
		Name:    "rg",
		Version: "14.1.1",
		Action:  resource.ActionInstall,
		Method:  "download",
	}, results)

	pm.HandleEvent(engine.Event{
		Type:    engine.EventComplete,
		Kind:    resource.KindTool,
		Name:    "rg",
		Version: "14.1.1",
		Action:  resource.ActionInstall,
		Method:  "download",
	}, results)

	// Delegation tool
	pm.HandleEvent(engine.Event{
		Type:    engine.EventStart,
		Kind:    resource.KindTool,
		Name:    "gopls",
		Version: "0.17.0",
		Action:  resource.ActionInstall,
		Method:  "go install",
	}, results)

	pm.HandleEvent(engine.Event{
		Type:    engine.EventComplete,
		Kind:    resource.KindTool,
		Name:    "gopls",
		Version: "0.17.0",
		Action:  resource.ActionInstall,
		Method:  "go install",
	}, results)

	output := buf.String()
	assert.Contains(t, output, "Downloads:")
	assert.Contains(t, output, "Commands:")
	assert.Equal(t, 2, results.Installed)
}

func TestProgressManager_IsDownloadMethod(t *testing.T) {
	t.Parallel()
	tests := []struct {
		method string
		want   bool
	}{
		{"download", true},
		{"", true},
		{"go install", false},
		{"brew install", false},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isDownloadMethod(tt.method))
		})
	}
}

func TestProgressManager_ResourceKey(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Tool/rg", resourceKey(resource.KindTool, "rg"))
	assert.Equal(t, "Runtime/go", resourceKey(resource.KindRuntime, "go"))
}

func TestPrintApplySummary_NoChanges(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	PrintApplySummary(&buf, &ApplyResults{})
	output := buf.String()
	assert.Contains(t, output, "No changes to apply")
}

func TestPrintApplySummary_WithResults(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	PrintApplySummary(&buf, &ApplyResults{
		Installed: 3,
		Upgraded:  1,
		Failed:    1,
	})
	output := buf.String()
	assert.Contains(t, output, "Installed: 3")
	assert.Contains(t, output, "Upgraded:  1")
	assert.Contains(t, output, "Failed:    1")
	assert.Contains(t, output, "completed with errors")
}

func TestPrintApplySummary_AllSuccess(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	PrintApplySummary(&buf, &ApplyResults{
		Installed: 2,
	})
	output := buf.String()
	assert.Contains(t, output, "Apply complete!")
}

func TestProgressManager_ConcurrentHandleEvent_NonTTY(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pm := newNonTTYProgressManager(&buf)
	results := &ApplyResults{}

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)

	for i := range n {
		go func(idx int) {
			defer wg.Done()
			name := fmt.Sprintf("tool%d", idx)

			pm.HandleEvent(engine.Event{
				Type:    engine.EventStart,
				Kind:    resource.KindTool,
				Name:    name,
				Version: "1.0.0",
				Action:  resource.ActionInstall,
				Method:  "download",
			}, results)

			pm.HandleEvent(engine.Event{
				Type:    engine.EventComplete,
				Kind:    resource.KindTool,
				Name:    name,
				Version: "1.0.0",
				Action:  resource.ActionInstall,
				Method:  "download",
			}, results)
		}(i)
	}

	wg.Wait()

	assert.Equal(t, n, results.Installed)
	// Downloads: header should appear exactly once
	assert.Equal(t, 1, strings.Count(buf.String(), "Downloads:"))
}

func TestProgressManager_ConcurrentMixedEvents_NonTTY(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	pm := newNonTTYProgressManager(&buf)
	results := &ApplyResults{}

	var wg sync.WaitGroup
	wg.Add(2)

	// Download events
	go func() {
		defer wg.Done()
		for i := range 5 {
			name := fmt.Sprintf("dl%d", i)
			pm.HandleEvent(engine.Event{
				Type:    engine.EventStart,
				Kind:    resource.KindTool,
				Name:    name,
				Version: "1.0.0",
				Action:  resource.ActionInstall,
				Method:  "download",
			}, results)
			pm.HandleEvent(engine.Event{
				Type:    engine.EventComplete,
				Kind:    resource.KindTool,
				Name:    name,
				Version: "1.0.0",
				Action:  resource.ActionInstall,
				Method:  "download",
			}, results)
		}
	}()

	// Delegation events
	go func() {
		defer wg.Done()
		for i := range 5 {
			name := fmt.Sprintf("cmd%d", i)
			pm.HandleEvent(engine.Event{
				Type:    engine.EventStart,
				Kind:    resource.KindTool,
				Name:    name,
				Version: "1.0.0",
				Action:  resource.ActionInstall,
				Method:  "go install",
			}, results)
			pm.HandleEvent(engine.Event{
				Type:   engine.EventOutput,
				Kind:   resource.KindTool,
				Name:   name,
				Output: "building...",
				Method: "go install",
			}, results)
			pm.HandleEvent(engine.Event{
				Type:    engine.EventComplete,
				Kind:    resource.KindTool,
				Name:    name,
				Version: "1.0.0",
				Action:  resource.ActionInstall,
				Method:  "go install",
			}, results)
		}
	}()

	wg.Wait()

	assert.Equal(t, 10, results.Installed)
}
