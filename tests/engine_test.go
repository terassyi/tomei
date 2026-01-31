//go:build integration

package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/toto/internal/installer/engine"
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
)

// mockToolInstaller is a mock implementation of engine.ToolInstaller.
type mockToolInstaller struct {
	installed map[string]*resource.ToolState
	removed   map[string]bool
}

func newMockToolInstaller() *mockToolInstaller {
	return &mockToolInstaller{
		installed: make(map[string]*resource.ToolState),
		removed:   make(map[string]bool),
	}
}

func (m *mockToolInstaller) Install(_ context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
	st := &resource.ToolState{
		InstallerRef: res.ToolSpec.InstallerRef,
		Version:      res.ToolSpec.Version,
		BinPath:      filepath.Join("/mock/bin", name),
	}
	m.installed[name] = st
	return st, nil
}

func (m *mockToolInstaller) Remove(_ context.Context, _ *resource.ToolState, name string) error {
	m.removed[name] = true
	delete(m.installed, name)
	return nil
}

// mockRuntimeInstaller is a mock implementation of engine.RuntimeInstaller.
type mockRuntimeInstaller struct {
	installed map[string]*resource.RuntimeState
	removed   map[string]bool
}

func newMockRuntimeInstaller() *mockRuntimeInstaller {
	return &mockRuntimeInstaller{
		installed: make(map[string]*resource.RuntimeState),
		removed:   make(map[string]bool),
	}
}

func (m *mockRuntimeInstaller) Install(_ context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
	st := &resource.RuntimeState{
		InstallerRef: res.RuntimeSpec.InstallerRef,
		Version:      res.RuntimeSpec.Version,
		InstallPath:  filepath.Join("/mock/runtimes", name, res.RuntimeSpec.Version),
		Binaries:     res.RuntimeSpec.Binaries,
		ToolBinPath:  res.RuntimeSpec.ToolBinPath,
		Env:          res.RuntimeSpec.Env,
	}
	m.installed[name] = st
	return st, nil
}

func (m *mockRuntimeInstaller) Remove(_ context.Context, _ *resource.RuntimeState, name string) error {
	m.removed[name] = true
	delete(m.installed, name)
	return nil
}

// TestEngine_PlanAll_Tool tests that Engine correctly plans tool actions from CUE config.
func TestEngine_PlanAll_Tool(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// Create CUE config with a tool
	cueContent := `package toto

ripgrep: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "download"
		version: "14.0.0"
		source: {
			url: "https://example.com/ripgrep.tar.gz"
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tool.cue"), []byte(cueContent), 0644))

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, store)

	ctx := context.Background()
	runtimeActions, toolActions, err := eng.PlanAll(ctx, configDir)
	require.NoError(t, err)

	assert.Empty(t, runtimeActions)
	assert.Len(t, toolActions, 1)
	assert.Equal(t, "ripgrep", toolActions[0].Name)
	assert.Equal(t, resource.ActionInstall, toolActions[0].Type)
}

// TestEngine_PlanAll_Runtime tests that Engine correctly plans runtime actions from CUE config.
func TestEngine_PlanAll_Runtime(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package toto

goRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		installerRef: "download"
		version: "1.25.5"
		source: {
			url: "https://go.dev/dl/go1.25.5.linux-amd64.tar.gz"
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "runtime.cue"), []byte(cueContent), 0644))

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, store)

	ctx := context.Background()
	runtimeActions, toolActions, err := eng.PlanAll(ctx, configDir)
	require.NoError(t, err)

	assert.Len(t, runtimeActions, 1)
	assert.Empty(t, toolActions)
	assert.Equal(t, "go", runtimeActions[0].Name)
	assert.Equal(t, resource.ActionInstall, runtimeActions[0].Type)
}

// TestEngine_Apply_Tool tests that Engine correctly applies tool installation.
func TestEngine_Apply_Tool(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package toto

jq: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "download"
		version: "1.7.1"
		source: {
			url: "https://example.com/jq"
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tool.cue"), []byte(cueContent), 0644))

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, store)

	ctx := context.Background()

	// Apply
	err = eng.Apply(ctx, configDir)
	require.NoError(t, err)

	// Verify mock was called
	assert.Contains(t, mockTool.installed, "jq")
	assert.Equal(t, "1.7.1", mockTool.installed["jq"].Version)

	// Verify state was updated
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Contains(t, st.Tools, "jq")
	assert.Equal(t, "1.7.1", st.Tools["jq"].Version)
}

// TestEngine_Apply_Runtime tests that Engine correctly applies runtime installation.
func TestEngine_Apply_Runtime(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package toto

goRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		installerRef: "download"
		version: "1.25.5"
		source: {
			url: "https://go.dev/dl/go1.25.5.linux-amd64.tar.gz"
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "/mock/runtimes/go/1.25.5"
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "runtime.cue"), []byte(cueContent), 0644))

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, store)

	ctx := context.Background()

	// Apply
	err = eng.Apply(ctx, configDir)
	require.NoError(t, err)

	// Verify mock was called
	assert.Contains(t, mockRuntime.installed, "go")
	assert.Equal(t, "1.25.5", mockRuntime.installed["go"].Version)

	// Verify state was updated
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Contains(t, st.Runtimes, "go")
	assert.Equal(t, "1.25.5", st.Runtimes["go"].Version)
}

// TestEngine_Apply_Idempotent tests that applying the same config twice does nothing on second run.
func TestEngine_Apply_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package toto

fd: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "download"
		version: "9.0.0"
		source: {
			url: "https://example.com/fd"
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tool.cue"), []byte(cueContent), 0644))

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, store)

	ctx := context.Background()

	// First apply
	err = eng.Apply(ctx, configDir)
	require.NoError(t, err)
	assert.Len(t, mockTool.installed, 1)

	// Second apply - should be no-op
	runtimeActions, toolActions, err := eng.PlanAll(ctx, configDir)
	require.NoError(t, err)
	assert.Empty(t, runtimeActions)
	assert.Empty(t, toolActions)
}

// TestEngine_Apply_Upgrade tests that changing version triggers an upgrade action.
func TestEngine_Apply_Upgrade(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// Initial config
	cueContent := `package toto

bat: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "download"
		version: "0.23.0"
		source: {
			url: "https://example.com/bat"
		}
	}
}
`
	cueFile := filepath.Join(configDir, "tool.cue")
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, store)

	ctx := context.Background()

	// First apply
	err = eng.Apply(ctx, configDir)
	require.NoError(t, err)

	// Update config with new version
	cueContentV2 := `package toto

bat: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "download"
		version: "0.24.0"
		source: {
			url: "https://example.com/bat"
		}
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContentV2), 0644))

	// Plan should show upgrade
	runtimeActions, toolActions, err := eng.PlanAll(ctx, configDir)
	require.NoError(t, err)
	assert.Empty(t, runtimeActions)
	assert.Len(t, toolActions, 1)
	assert.Equal(t, "bat", toolActions[0].Name)
	assert.Equal(t, resource.ActionUpgrade, toolActions[0].Type)

	// Apply upgrade
	err = eng.Apply(ctx, configDir)
	require.NoError(t, err)

	// Verify state was updated
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Equal(t, "0.24.0", st.Tools["bat"].Version)
}

// TestEngine_Apply_Remove tests that removing a resource from config triggers removal.
func TestEngine_Apply_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// Initial config with tool
	cueContent := `package toto

fzf: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fzf"
	spec: {
		installerRef: "download"
		version: "0.44.0"
		source: {
			url: "https://example.com/fzf"
		}
	}
}
`
	cueFile := filepath.Join(configDir, "tool.cue")
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, store)

	ctx := context.Background()

	// First apply
	err = eng.Apply(ctx, configDir)
	require.NoError(t, err)
	assert.Contains(t, mockTool.installed, "fzf")

	// Remove tool from config (keep another tool so config isn't empty)
	cueContentWithOther := `package toto

other: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "other"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/other"
		}
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContentWithOther), 0644))

	// Plan should show remove for fzf and install for other
	runtimeActions, toolActions, err := eng.PlanAll(ctx, configDir)
	require.NoError(t, err)
	assert.Empty(t, runtimeActions)
	assert.Len(t, toolActions, 2) // remove fzf + install other

	// Find the fzf action
	var fzfAction *engine.ToolAction
	for i := range toolActions {
		if toolActions[i].Name == "fzf" {
			fzfAction = &toolActions[i]
			break
		}
	}
	require.NotNil(t, fzfAction)
	assert.Equal(t, resource.ActionRemove, fzfAction.Type)

	// Apply removal
	err = eng.Apply(ctx, configDir)
	require.NoError(t, err)

	// Verify mock Remove was called
	assert.True(t, mockTool.removed["fzf"])

	// Verify state no longer has the tool
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.NotContains(t, st.Tools, "fzf")
}

// TestEngine_Apply_RuntimeAndTool tests installing both runtime and tool together.
func TestEngine_Apply_RuntimeAndTool(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package toto

goRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		installerRef: "download"
		version: "1.25.5"
		source: {
			url: "https://go.dev/dl/go1.25.5.linux-amd64.tar.gz"
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
	}
}

jq: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "download"
		version: "1.7.1"
		source: {
			url: "https://example.com/jq"
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "resources.cue"), []byte(cueContent), 0644))

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, store)

	ctx := context.Background()

	// Plan
	runtimeActions, toolActions, err := eng.PlanAll(ctx, configDir)
	require.NoError(t, err)
	assert.Len(t, runtimeActions, 1)
	assert.Len(t, toolActions, 1)

	// Apply
	err = eng.Apply(ctx, configDir)
	require.NoError(t, err)

	// Verify both installed
	assert.Contains(t, mockRuntime.installed, "go")
	assert.Contains(t, mockTool.installed, "jq")

	// Verify state
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Contains(t, st.Runtimes, "go")
	assert.Contains(t, st.Tools, "jq")
}

// TestEngine_Apply_MultipleTools tests installing multiple tools at once.
func TestEngine_Apply_MultipleTools(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package toto

ripgrep: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "download"
		version: "14.0.0"
		source: { url: "https://example.com/rg" }
	}
}

fd: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "download"
		version: "9.0.0"
		source: { url: "https://example.com/fd" }
	}
}

bat: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "download"
		version: "0.24.0"
		source: { url: "https://example.com/bat" }
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueContent), 0644))

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, store)

	ctx := context.Background()

	// Apply
	err = eng.Apply(ctx, configDir)
	require.NoError(t, err)

	// Verify all installed
	assert.Len(t, mockTool.installed, 3)
	assert.Contains(t, mockTool.installed, "ripgrep")
	assert.Contains(t, mockTool.installed, "fd")
	assert.Contains(t, mockTool.installed, "bat")

	// Verify state
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Len(t, st.Tools, 3)
}
