//go:build integration

package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/engine"
	tomeilog "github.com/terassyi/tomei/internal/log"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// handleLogEvent dispatches an engine event to the LogStore.
// This replicates the logic in cmd/tomei/apply.go (main package).
func handleLogEvent(logStore *tomeilog.Store, event engine.Event) {
	switch event.Type {
	case engine.EventStart:
		logStore.RecordStart(event.Kind, event.Name, event.Version, string(event.Action), event.Method)
	case engine.EventOutput:
		logStore.RecordOutput(event.Kind, event.Name, event.Output)
	case engine.EventError:
		logStore.RecordError(event.Kind, event.Name, event.Error)
	case engine.EventComplete:
		logStore.RecordComplete(event.Kind, event.Name)
	}
}

// TestEngine_Apply_LogStore_FailedToolCapturesOutput verifies the full path:
// engine event → LogStore → Flush → disk log file (only for failed resources).
func TestEngine_Apply_LogStore_FailedToolCapturesOutput(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	logsDir := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// Define two tools: alpha (succeeds) and bravo (fails)
	cueContent := `package tomei

alpha: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "alpha"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: { url: "https://example.com/alpha" }
	}
}

bravo: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bravo"
	spec: {
		installerRef: "download"
		version: "2.0.0"
		source: { url: "https://example.com/bravo" }
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueContent), 0644))
	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := &mockToolInstaller{
		installed: make(map[string]*resource.ToolState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			outputCb := download.CallbackFromContext[download.OutputCallback](ctx)

			if name == "bravo" {
				if outputCb != nil {
					outputCb("compiling bravo...")
					outputCb("error: link failed")
				}
				return nil, fmt.Errorf("bravo install failed")
			}

			// alpha succeeds
			if outputCb != nil {
				outputCb("downloading alpha...")
			}
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				BinPath:      filepath.Join("/mock/bin", name),
			}, nil
		},
	}

	logStore, err := tomeilog.NewStore(logsDir)
	require.NoError(t, err)
	defer logStore.Close()

	eng := engine.NewEngine(mockTool, newMockRuntimeInstaller(), newMockInstallerRepositoryInstaller(), store)
	eng.SetEventHandler(func(event engine.Event) {
		handleLogEvent(logStore, event)
	})

	err = eng.Apply(context.Background(), resources)
	require.Error(t, err)

	// Only bravo should be in failed resources
	failed := logStore.FailedResources()
	require.Len(t, failed, 1)
	assert.Equal(t, resource.KindTool, failed[0].Kind)
	assert.Equal(t, "bravo", failed[0].Name)
	assert.Equal(t, "2.0.0", failed[0].Version)
	assert.Contains(t, failed[0].Output, "compiling bravo...\n")
	assert.Contains(t, failed[0].Output, "error: link failed\n")

	// Flush to disk
	require.NoError(t, logStore.Flush())

	// bravo log file should exist
	bravoLog := filepath.Join(logStore.SessionDir(), "Tool_bravo.log")
	bravoContent, err := os.ReadFile(bravoLog)
	require.NoError(t, err)
	assert.Contains(t, string(bravoContent), "# Resource: Tool/bravo")
	assert.Contains(t, string(bravoContent), "bravo install failed")
	assert.Contains(t, string(bravoContent), "compiling bravo...")
	assert.Contains(t, string(bravoContent), "error: link failed")

	// alpha log file should NOT exist (success = discarded)
	alphaLog := filepath.Join(logStore.SessionDir(), "Tool_alpha.log")
	_, err = os.Stat(alphaLog)
	assert.True(t, os.IsNotExist(err))
}

// TestEngine_Apply_LogStore_RuntimeOutputCallback verifies that runtime delegation
// OutputCallback events flow through to the LogStore.
func TestEngine_Apply_LogStore_RuntimeOutputCallback(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	logsDir := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.5"
		source: { url: "https://go.dev/dl/go1.25.5.linux-amd64.tar.gz" }
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "runtime.cue"), []byte(cueContent), 0644))
	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockRuntime := &mockRuntimeInstaller{
		installed: make(map[string]*resource.RuntimeState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
			outputCb := download.CallbackFromContext[download.OutputCallback](ctx)
			if outputCb != nil {
				outputCb("downloading go1.25.5...")
				outputCb("extracting archive...")
				outputCb("error: checksum mismatch")
			}
			return nil, fmt.Errorf("go install failed: checksum mismatch")
		},
	}

	logStore, err := tomeilog.NewStore(logsDir)
	require.NoError(t, err)
	defer logStore.Close()

	eng := engine.NewEngine(newMockToolInstaller(), mockRuntime, newMockInstallerRepositoryInstaller(), store)
	eng.SetEventHandler(func(event engine.Event) {
		handleLogEvent(logStore, event)
	})

	err = eng.Apply(context.Background(), resources)
	require.Error(t, err)

	failed := logStore.FailedResources()
	require.Len(t, failed, 1)
	assert.Equal(t, resource.KindRuntime, failed[0].Kind)
	assert.Equal(t, "go", failed[0].Name)
	assert.Contains(t, failed[0].Output, "downloading go1.25.5...\n")
	assert.Contains(t, failed[0].Output, "error: checksum mismatch\n")

	// Flush and verify log file
	require.NoError(t, logStore.Flush())

	logFile := filepath.Join(logStore.SessionDir(), "Runtime_go.log")
	content, err := os.ReadFile(logFile)
	require.NoError(t, err)
	assert.Contains(t, string(content), "# Resource: Runtime/go")
	assert.Contains(t, string(content), "downloading go1.25.5...")
}

// TestEngine_Apply_LogStore_AllSuccessNoLogs verifies that when all resources
// succeed, no log files are written to disk.
func TestEngine_Apply_LogStore_AllSuccessNoLogs(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	logsDir := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

alpha: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "alpha"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: { url: "https://example.com/alpha" }
	}
}

bravo: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bravo"
	spec: {
		installerRef: "download"
		version: "2.0.0"
		source: { url: "https://example.com/bravo" }
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueContent), 0644))
	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := &mockToolInstaller{
		installed: make(map[string]*resource.ToolState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			outputCb := download.CallbackFromContext[download.OutputCallback](ctx)
			if outputCb != nil {
				outputCb("installing " + name + "...")
			}
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				BinPath:      filepath.Join("/mock/bin", name),
			}, nil
		},
	}

	logStore, err := tomeilog.NewStore(logsDir)
	require.NoError(t, err)

	eng := engine.NewEngine(mockTool, newMockRuntimeInstaller(), newMockInstallerRepositoryInstaller(), store)
	eng.SetEventHandler(func(event engine.Event) {
		handleLogEvent(logStore, event)
	})

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// No failed resources
	assert.Empty(t, logStore.FailedResources())

	// Flush should be a no-op
	require.NoError(t, logStore.Flush())

	// Close should clean up session directory
	logStore.Close()
	_, err = os.Stat(logStore.SessionDir())
	assert.True(t, os.IsNotExist(err))
}

// TestEngine_Apply_LogStore_ParallelFailureIsolation verifies that parallel execution
// correctly isolates failed resource logs from successful ones.
func TestEngine_Apply_LogStore_ParallelFailureIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	logsDir := filepath.Join(tmpDir, "logs")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// Three independent tools
	cueContent := `package tomei

toolA: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool-a"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: { url: "https://example.com/tool-a" }
	}
}

toolB: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool-b"
	spec: {
		installerRef: "download"
		version: "2.0.0"
		source: { url: "https://example.com/tool-b" }
	}
}

toolC: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool-c"
	spec: {
		installerRef: "download"
		version: "3.0.0"
		source: { url: "https://example.com/tool-c" }
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueContent), 0644))
	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := &mockToolInstaller{
		installed: make(map[string]*resource.ToolState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			outputCb := download.CallbackFromContext[download.OutputCallback](ctx)

			if name == "tool-b" {
				if outputCb != nil {
					outputCb("tool-b: compiling...")
					outputCb("tool-b: FATAL error")
				}
				return nil, fmt.Errorf("tool-b failed")
			}

			if outputCb != nil {
				outputCb(name + ": installing...")
			}
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				BinPath:      filepath.Join("/mock/bin", name),
			}, nil
		},
	}

	logStore, err := tomeilog.NewStore(logsDir)
	require.NoError(t, err)
	defer logStore.Close()

	eng := engine.NewEngine(mockTool, newMockRuntimeInstaller(), newMockInstallerRepositoryInstaller(), store)
	eng.SetParallelism(3)

	var mu sync.Mutex
	eng.SetEventHandler(func(event engine.Event) {
		mu.Lock()
		handleLogEvent(logStore, event)
		mu.Unlock()
	})

	err = eng.Apply(context.Background(), resources)
	require.Error(t, err)

	// tool-b must be in failed resources
	failed := logStore.FailedResources()
	var failedNames []string
	for _, f := range failed {
		failedNames = append(failedNames, f.Name)
	}
	assert.Contains(t, failedNames, "tool-b")
	assert.NotContains(t, failedNames, "tool-a")

	// Verify tool-b output is isolated
	for _, f := range failed {
		if f.Name == "tool-b" {
			assert.Contains(t, f.Output, "tool-b: compiling...\n")
			assert.Contains(t, f.Output, "tool-b: FATAL error\n")
			assert.NotContains(t, f.Output, "tool-a:")
		}
	}

	// Flush and verify only tool-b log exists
	require.NoError(t, logStore.Flush())

	toolBLog := filepath.Join(logStore.SessionDir(), "Tool_tool-b.log")
	_, err = os.Stat(toolBLog)
	assert.NoError(t, err)

	toolALog := filepath.Join(logStore.SessionDir(), "Tool_tool-a.log")
	_, err = os.Stat(toolALog)
	assert.True(t, os.IsNotExist(err))
}
