//go:build integration

package tests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/installer/tool"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// mockToolInstaller is a thread-safe mock implementation of engine.ToolInstaller.
type mockToolInstaller struct {
	mu          sync.Mutex
	installed   map[string]*resource.ToolState
	removed     map[string]bool
	installFunc func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error)
}

func newMockToolInstaller() *mockToolInstaller {
	return &mockToolInstaller{
		installed: make(map[string]*resource.ToolState),
		removed:   make(map[string]bool),
	}
}

func (m *mockToolInstaller) Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
	if m.installFunc != nil {
		return m.installFunc(ctx, res, name)
	}
	st := &resource.ToolState{
		InstallerRef: res.ToolSpec.InstallerRef,
		RuntimeRef:   res.ToolSpec.RuntimeRef,
		Package:      res.ToolSpec.Package,
		Version:      res.ToolSpec.Version,
		BinPath:      filepath.Join("/mock/bin", name),
	}
	m.mu.Lock()
	m.installed[name] = st
	m.mu.Unlock()
	return st, nil
}

func (m *mockToolInstaller) Remove(_ context.Context, _ *resource.ToolState, name string) error {
	m.mu.Lock()
	m.removed[name] = true
	delete(m.installed, name)
	m.mu.Unlock()
	return nil
}

func (m *mockToolInstaller) RegisterRuntime(_ string, _ *tool.RuntimeInfo) {}

func (m *mockToolInstaller) RegisterInstaller(_ string, _ *tool.InstallerInfo) {}

func (m *mockToolInstaller) SetToolBinPaths(_ map[string]string) {}

func (m *mockToolInstaller) SetProgressCallback(_ download.ProgressCallback) {}

func (m *mockToolInstaller) SetOutputCallback(_ download.OutputCallback) {}

// mockRuntimeInstaller is a thread-safe mock implementation of engine.RuntimeInstaller.
type mockRuntimeInstaller struct {
	mu          sync.Mutex
	installed   map[string]*resource.RuntimeState
	removed     map[string]bool
	installFunc func(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error)
}

func newMockRuntimeInstaller() *mockRuntimeInstaller {
	return &mockRuntimeInstaller{
		installed: make(map[string]*resource.RuntimeState),
		removed:   make(map[string]bool),
	}
}

func (m *mockRuntimeInstaller) Install(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
	if m.installFunc != nil {
		return m.installFunc(ctx, res, name)
	}
	// Resolve BinDir: use explicit BinDir, or fall back to ToolBinPath
	binDir := res.RuntimeSpec.BinDir
	if binDir == "" {
		binDir = res.RuntimeSpec.ToolBinPath
	}

	st := &resource.RuntimeState{
		Type:        res.RuntimeSpec.Type,
		Version:     res.RuntimeSpec.Version,
		InstallPath: filepath.Join("/mock/runtimes", name, res.RuntimeSpec.Version),
		Binaries:    res.RuntimeSpec.Binaries,
		BinDir:      binDir,
		ToolBinPath: res.RuntimeSpec.ToolBinPath,
		Env:         res.RuntimeSpec.Env,
		Commands:    res.RuntimeSpec.Commands,
	}
	m.mu.Lock()
	m.installed[name] = st
	m.mu.Unlock()
	return st, nil
}

func (m *mockRuntimeInstaller) Remove(_ context.Context, _ *resource.RuntimeState, name string) error {
	m.mu.Lock()
	m.removed[name] = true
	delete(m.installed, name)
	m.mu.Unlock()
	return nil
}

func (m *mockRuntimeInstaller) SetProgressCallback(_ download.ProgressCallback) {}

// mockInstallerRepositoryInstaller is a thread-safe mock implementation of engine.InstallerRepositoryInstaller.
type mockInstallerRepositoryInstaller struct {
	mu          sync.Mutex
	installed   map[string]*resource.InstallerRepositoryState
	removed     map[string]bool
	installFunc func(ctx context.Context, res *resource.InstallerRepository, name string) (*resource.InstallerRepositoryState, error)
}

func newMockInstallerRepositoryInstaller() *mockInstallerRepositoryInstaller {
	return &mockInstallerRepositoryInstaller{
		installed: make(map[string]*resource.InstallerRepositoryState),
		removed:   make(map[string]bool),
	}
}

func (m *mockInstallerRepositoryInstaller) Install(ctx context.Context, res *resource.InstallerRepository, name string) (*resource.InstallerRepositoryState, error) {
	if m.installFunc != nil {
		return m.installFunc(ctx, res, name)
	}
	st := &resource.InstallerRepositoryState{
		InstallerRef: res.InstallerRepositorySpec.InstallerRef,
		SourceType:   res.InstallerRepositorySpec.Source.Type,
		URL:          res.InstallerRepositorySpec.Source.URL,
	}
	m.mu.Lock()
	m.installed[name] = st
	m.mu.Unlock()
	return st, nil
}

func (m *mockInstallerRepositoryInstaller) Remove(_ context.Context, _ *resource.InstallerRepositoryState, name string) error {
	m.mu.Lock()
	m.removed[name] = true
	delete(m.installed, name)
	m.mu.Unlock()
	return nil
}

func (m *mockInstallerRepositoryInstaller) SetToolBinPaths(_ map[string]string) {}

// loadResources is a helper to load resources from a config directory.
func loadResources(t *testing.T, configDir string) []resource.Resource {
	t.Helper()
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)
	return resources
}

// TestEngine_PlanAll_Tool tests that Engine correctly plans tool actions from CUE config.
func TestEngine_PlanAll_Tool(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// Create CUE config with a tool
	cueContent := `package tomei

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()
	runtimeActions, _, toolActions, err := eng.PlanAll(ctx, resources)
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

	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()
	runtimeActions, _, toolActions, err := eng.PlanAll(ctx, resources)
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

	cueContent := `package tomei

jq: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Apply
	err = eng.Apply(ctx, resources)
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

	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Apply
	err = eng.Apply(ctx, resources)
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

	cueContent := `package tomei

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// First apply
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)
	assert.Len(t, mockTool.installed, 1)

	// Second apply - should be no-op
	runtimeActions, _, toolActions, err := eng.PlanAll(ctx, resources)
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
	cueContent := `package tomei

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// First apply
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Update config with new version
	cueContentV2 := `package tomei

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

	// Reload resources with new config
	resourcesV2 := loadResources(t, configDir)

	// Plan should show upgrade
	runtimeActions, _, toolActions, err := eng.PlanAll(ctx, resourcesV2)
	require.NoError(t, err)
	assert.Empty(t, runtimeActions)
	assert.Len(t, toolActions, 1)
	assert.Equal(t, "bat", toolActions[0].Name)
	assert.Equal(t, resource.ActionUpgrade, toolActions[0].Type)

	// Apply upgrade
	err = eng.Apply(ctx, resourcesV2)
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
	cueContent := `package tomei

fzf: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// First apply
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)
	assert.Contains(t, mockTool.installed, "fzf")

	// Remove tool from config (keep another tool so config isn't empty)
	cueContentWithOther := `package tomei

other: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

	// Reload resources with new config
	resourcesV2 := loadResources(t, configDir)

	// Plan should show remove for fzf and install for other
	runtimeActions, _, toolActions, err := eng.PlanAll(ctx, resourcesV2)
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
	err = eng.Apply(ctx, resourcesV2)
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

	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.5"
		source: {
			url: "https://go.dev/dl/go1.25.5.linux-amd64.tar.gz"
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
	}
}

jq: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Plan
	runtimeActions, _, toolActions, err := eng.PlanAll(ctx, resources)
	require.NoError(t, err)
	assert.Len(t, runtimeActions, 1)
	assert.Len(t, toolActions, 1)

	// Apply
	err = eng.Apply(ctx, resources)
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

	cueContent := `package tomei

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "download"
		version: "14.0.0"
		source: { url: "https://example.com/rg" }
	}
}

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "download"
		version: "9.0.0"
		source: { url: "https://example.com/fd" }
	}
}

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Apply
	err = eng.Apply(ctx, resources)
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

// TestEngine_Apply_RuntimeDelegation tests installing a tool via runtime delegation (e.g., go install).
func TestEngine_Apply_RuntimeDelegation(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// CUE config with Runtime that has commands, and Tool with runtimeRef
	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.5"
		source: {
			url: "https://go.dev/dl/go1.25.5.linux-amd64.tar.gz"
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "/mock/runtimes/go/1.25.5"
			GOBIN: "~/go/bin"
		}
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
			remove: "rm -f {{.BinPath}}"
		}
	}
}

gopls: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "gopls"
	spec: {
		runtimeRef: "go"
		package: "golang.org/x/tools/gopls"
		version: "v0.16.0"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "resources.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Plan
	runtimeActions, _, toolActions, err := eng.PlanAll(ctx, resources)
	require.NoError(t, err)
	assert.Len(t, runtimeActions, 1)
	assert.Len(t, toolActions, 1)

	assert.Equal(t, "go", runtimeActions[0].Name)
	assert.Equal(t, "gopls", toolActions[0].Name)

	// Verify tool spec has runtimeRef
	assert.Equal(t, "go", toolActions[0].Resource.ToolSpec.RuntimeRef)
	assert.Equal(t, "golang.org/x/tools/gopls", toolActions[0].Resource.ToolSpec.Package.String())

	// Apply
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Verify runtime and tool are installed
	assert.Contains(t, mockRuntime.installed, "go")
	assert.Contains(t, mockTool.installed, "gopls")

	// Verify state
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Contains(t, st.Runtimes, "go")
	assert.Contains(t, st.Tools, "gopls")

	// Verify runtime has commands
	assert.NotNil(t, st.Runtimes["go"].Commands)
	assert.Equal(t, "go install {{.Package}}@{{.Version}}", st.Runtimes["go"].Commands.Install)
}

// TestEngine_Apply_InstallerDelegation tests installing a tool via installer delegation (e.g., brew install).
func TestEngine_Apply_InstallerDelegation(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// CUE config with delegation pattern Installer and Tool
	cueContent := `package tomei

brewInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "brew"
	spec: {
		type: "delegation"
		commands: {
			install: "brew install {{.Package}}"
			remove: "brew uninstall {{.Package}}"
		}
	}
}

jq: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "brew"
		package: "jq"
		version: "1.7"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "resources.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Plan
	runtimeActions, _, toolActions, err := eng.PlanAll(ctx, resources)
	require.NoError(t, err)
	assert.Empty(t, runtimeActions)
	assert.Len(t, toolActions, 1)

	assert.Equal(t, "jq", toolActions[0].Name)
	assert.Equal(t, "brew", toolActions[0].Resource.ToolSpec.InstallerRef)

	// Apply
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Verify tool is installed
	assert.Contains(t, mockTool.installed, "jq")

	// Verify state
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Contains(t, st.Tools, "jq")
	assert.Equal(t, "brew", st.Tools["jq"].InstallerRef)
}

// TestEngine_Apply_MixedPatterns tests installing tools with different patterns together.
func TestEngine_Apply_MixedPatterns(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// CUE config with mixed patterns:
	// - Runtime with commands (for go install)
	// - Download pattern tool (ripgrep)
	// - Runtime delegation tool (gopls)
	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.5"
		source: {
			url: "https://go.dev/dl/go1.25.5.linux-amd64.tar.gz"
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
		}
	}
}

// Download pattern tool
ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

// Runtime delegation tool
gopls: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "gopls"
	spec: {
		runtimeRef: "go"
		package: "golang.org/x/tools/gopls"
		version: "v0.16.0"
	}
}

// Another runtime delegation tool
staticcheck: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "staticcheck"
	spec: {
		runtimeRef: "go"
		package: "honnef.co/go/tools/cmd/staticcheck"
		version: "v0.5.0"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "resources.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Plan
	runtimeActions, _, toolActions, err := eng.PlanAll(ctx, resources)
	require.NoError(t, err)
	assert.Len(t, runtimeActions, 1)
	assert.Len(t, toolActions, 3)

	// Apply
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Verify all installed
	assert.Contains(t, mockRuntime.installed, "go")
	assert.Len(t, mockTool.installed, 3)
	assert.Contains(t, mockTool.installed, "ripgrep")
	assert.Contains(t, mockTool.installed, "gopls")
	assert.Contains(t, mockTool.installed, "staticcheck")

	// Verify state
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Len(t, st.Runtimes, 1)
	assert.Len(t, st.Tools, 3)

	// Check tool patterns
	assert.Equal(t, "download", st.Tools["ripgrep"].InstallerRef)
	assert.Empty(t, st.Tools["ripgrep"].RuntimeRef)

	assert.Equal(t, "go", st.Tools["gopls"].RuntimeRef)
	assert.Equal(t, "golang.org/x/tools/gopls", st.Tools["gopls"].Package.String())

	assert.Equal(t, "go", st.Tools["staticcheck"].RuntimeRef)
}

// TestEngine_Apply_RuntimeWithBinDir tests that BinDir is correctly set in state.
func TestEngine_Apply_RuntimeWithBinDir(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// CUE config with explicit BinDir
	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.5"
		source: {
			url: "https://go.dev/dl/go1.25.5.linux-amd64.tar.gz"
		}
		binaries: ["go", "gofmt"]
		binDir: "~/go/bin"
		toolBinPath: "~/go/bin"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "runtime.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Apply
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Verify state has BinDir
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Contains(t, st.Runtimes, "go")
	assert.Equal(t, "~/go/bin", st.Runtimes["go"].BinDir)
	assert.Equal(t, "~/go/bin", st.Runtimes["go"].ToolBinPath)
}

// TestEngine_PlanAll_DetectsToolRemoval tests that PlanAll detects a tool
// that exists in state but not in manifests as ActionRemove.
func TestEngine_PlanAll_DetectsToolRemoval(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// Initial config with two tools
	cueContent := `package tomei

fzf: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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
	cueFile := filepath.Join(configDir, "tools.cue")
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Install both tools
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Remove fzf from manifest, keep only bat
	cueContentV2 := `package tomei

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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
	resourcesV2 := loadResources(t, configDir)

	// PlanAll should detect fzf removal
	_, _, toolActions, err := eng.PlanAll(ctx, resourcesV2)
	require.NoError(t, err)

	var fzfAction *engine.ToolAction
	for i := range toolActions {
		if toolActions[i].Name == "fzf" {
			fzfAction = &toolActions[i]
			break
		}
	}
	require.NotNil(t, fzfAction, "expected fzf action in plan")
	assert.Equal(t, resource.ActionRemove, fzfAction.Type)

	// bat should NOT appear in actions (Reconcile omits ActionNone entries)
	for _, action := range toolActions {
		assert.NotEqual(t, "bat", action.Name,
			"bat should not appear in plan — it has no changes")
	}
}

// TestEngine_PlanAll_DetectsRuntimeRemoval tests that PlanAll detects a
// runtime in state but absent from manifests as ActionRemove.
func TestEngine_PlanAll_DetectsRuntimeRemoval(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

myruntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "myruntime"
	spec: {
		type: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/myruntime.tar.gz"
			checksum: { value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" }
		}
		binaries: ["mybin"]
		toolBinPath: "~/bin"
	}
}
`
	cueFile := filepath.Join(configDir, "runtime.cue")
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Install runtime
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// PlanAll with empty resources should detect runtime removal
	var emptyResources []resource.Resource
	runtimeActions, _, _, err := eng.PlanAll(ctx, emptyResources)
	require.NoError(t, err)

	var rtAction *engine.RuntimeAction
	for i := range runtimeActions {
		if runtimeActions[i].Name == "myruntime" {
			rtAction = &runtimeActions[i]
			break
		}
	}
	require.NotNil(t, rtAction, "expected myruntime action in plan")
	assert.Equal(t, resource.ActionRemove, rtAction.Type)
}

// TestEngine_PlanAll_NoChanges tests that PlanAll returns ActionNone for
// all resources when state matches manifests (idempotent).
func TestEngine_PlanAll_NoChanges(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

fzf: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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
	cueFile := filepath.Join(configDir, "tools.cue")
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Install tool
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// PlanAll with same resources should have no actions
	// (Reconcile omits ActionNone entries — only Install/Upgrade/Remove appear)
	_, _, toolActions, err := eng.PlanAll(ctx, resources)
	require.NoError(t, err)
	assert.Empty(t, toolActions, "expected no tool actions for idempotent plan")
}

// TestEngine_PlanAll_DetectsUpgrade tests that PlanAll detects a version
// change as ActionUpgrade.
func TestEngine_PlanAll_DetectsUpgrade(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

fzf: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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
	cueFile := filepath.Join(configDir, "tools.cue")
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Install v0.44.0
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Upgrade to v0.45.0
	cueContentV2 := `package tomei

fzf: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fzf"
	spec: {
		installerRef: "download"
		version: "0.45.0"
		source: {
			url: "https://example.com/fzf"
		}
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContentV2), 0644))
	resourcesV2 := loadResources(t, configDir)

	_, _, toolActions, err := eng.PlanAll(ctx, resourcesV2)
	require.NoError(t, err)

	var fzfAction *engine.ToolAction
	for i := range toolActions {
		if toolActions[i].Name == "fzf" {
			fzfAction = &toolActions[i]
			break
		}
	}
	require.NotNil(t, fzfAction, "expected fzf action in plan")
	assert.Equal(t, resource.ActionUpgrade, fzfAction.Type)
}

// TestEngine_Apply_CreatesStateBackup tests that engine.Apply creates a
// state.json.bak file containing the pre-apply state.
// In production, `tomei init` always creates state.json before apply runs,
// so we simulate init by writing an empty state before the first apply.
func TestEngine_Apply_CreatesStateBackup(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

fzf: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Simulate `tomei init`: write empty state.json before first apply
	require.NoError(t, store.Lock())
	require.NoError(t, store.Save(state.NewUserState()))
	require.NoError(t, store.Unlock())

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// First apply — state.json already exists (from init), so backup is created
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Backup should exist after first apply
	bakPath := state.BackupPath(store.StatePath())
	assert.FileExists(t, bakPath)

	// Backup should reflect pre-apply state (empty — no tools installed yet)
	backup, err := state.LoadBackup[state.UserState](store.StatePath())
	require.NoError(t, err)
	require.NotNil(t, backup)
	assert.Empty(t, backup.Tools,
		"backup should reflect empty pre-apply state (simulating post-init)")
}

// TestEngine_Apply_IdempotentCreatesBackup tests that a second apply
// (idempotent, no changes) still creates/updates the backup file.
func TestEngine_Apply_IdempotentCreatesBackup(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

fzf: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Simulate `tomei init`: write empty state.json before first apply
	require.NoError(t, store.Lock())
	require.NoError(t, store.Save(state.NewUserState()))
	require.NoError(t, store.Unlock())

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// First apply — installs fzf
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Record current state (fzf installed)
	require.NoError(t, store.Lock())
	stateAfterFirst, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()
	require.Contains(t, stateAfterFirst.Tools, "fzf")

	// Second apply — idempotent, no changes
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Backup should reflect state just before second apply (fzf installed)
	backup, err := state.LoadBackup[state.UserState](store.StatePath())
	require.NoError(t, err)
	require.NotNil(t, backup)
	assert.Contains(t, backup.Tools, "fzf",
		"backup after idempotent apply should contain fzf from pre-apply state")
	assert.Equal(t, "0.44.0", backup.Tools["fzf"].Version)
}

// TestEngine_Apply_BackupReflectsPreApplyState tests that the backup always
// contains the state from just before the apply, even after an upgrade.
func TestEngine_Apply_BackupReflectsPreApplyState(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

fzf: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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
	cueFile := filepath.Join(configDir, "tools.cue")
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Simulate `tomei init`: write empty state.json before first apply
	require.NoError(t, store.Lock())
	require.NoError(t, store.Save(state.NewUserState()))
	require.NoError(t, store.Unlock())

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// First apply — v0.44.0
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Upgrade to v0.45.0
	cueContentV2 := `package tomei

fzf: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fzf"
	spec: {
		installerRef: "download"
		version: "0.45.0"
		source: {
			url: "https://example.com/fzf"
		}
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContentV2), 0644))
	resourcesV2 := loadResources(t, configDir)

	// Second apply — upgrade
	err = eng.Apply(ctx, resourcesV2)
	require.NoError(t, err)

	// Backup should have v0.44.0 (pre-upgrade state)
	backup, err := state.LoadBackup[state.UserState](store.StatePath())
	require.NoError(t, err)
	require.NotNil(t, backup)
	require.Contains(t, backup.Tools, "fzf")
	assert.Equal(t, "0.44.0", backup.Tools["fzf"].Version,
		"backup should reflect pre-upgrade version")

	// Current state should have v0.45.0
	require.NoError(t, store.Lock())
	current, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()
	require.Contains(t, current.Tools, "fzf")
	assert.Equal(t, "0.45.0", current.Tools["fzf"].Version,
		"current state should have upgraded version")
}

// TestEngine_Apply_RemoveRuntimeBlockedByDependentTool tests that removing a runtime
// from config is rejected when dependent tools still reference it.
func TestEngine_Apply_RemoveRuntimeBlockedByDependentTool(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// Initial config: runtime + delegated tool
	cueContent := `package tomei

runtime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/go.tar.gz"
			checksum: value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		}
		binaries: ["go"]
		toolBinPath: "~/go/bin"
	}
}

gopls: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "gopls"
	spec: {
		runtimeRef: "go"
		package: "golang.org/x/tools/gopls"
		version: "v0.21.0"
	}
}
`
	cueFile := filepath.Join(configDir, "resources.cue")
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// First apply: install runtime + tool
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Remove runtime only, keep gopls
	cueContentV2 := `package tomei

gopls: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "gopls"
	spec: {
		runtimeRef: "go"
		package: "golang.org/x/tools/gopls"
		version: "v0.21.0"
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContentV2), 0644))
	resourcesV2 := loadResources(t, configDir)

	// Apply should fail — runtime has dependent tool
	err = eng.Apply(ctx, resourcesV2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot remove runtime")
	assert.Contains(t, err.Error(), "gopls")

	// PlanAll should also fail
	_, _, _, err = eng.PlanAll(ctx, resourcesV2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot remove runtime")

	// Now remove both — should succeed
	cueContentV3 := `package tomei

placeholder: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fzf"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/fzf"
		}
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContentV3), 0644))
	resourcesV3 := loadResources(t, configDir)

	err = eng.Apply(ctx, resourcesV3)
	require.NoError(t, err)

	// Verify both removed from state
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.NotContains(t, st.Runtimes, "go")
	assert.NotContains(t, st.Tools, "gopls")
}

// TestEngine_Apply_ToolSet_InstallerRef tests ToolSet expansion with installerRef (download pattern).
func TestEngine_Apply_ToolSet_InstallerRef(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

aquaInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: type: "download"
}

cliTools: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "ToolSet"
	metadata: name: "cli-tools"
	spec: {
		installerRef: "aqua"
		tools: {
			fd:  { version: "9.0.0" }
			bat: { version: "0.24.0" }
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "toolset.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	mockTool.mu.Lock()
	assert.Contains(t, mockTool.installed, "fd")
	assert.Contains(t, mockTool.installed, "bat")
	assert.Equal(t, "9.0.0", mockTool.installed["fd"].Version)
	assert.Equal(t, "0.24.0", mockTool.installed["bat"].Version)
	assert.Equal(t, "aqua", mockTool.installed["fd"].InstallerRef)
	mockTool.mu.Unlock()
}

// TestEngine_Apply_ToolSet_RuntimeRef tests ToolSet expansion with runtimeRef (delegation pattern).
func TestEngine_Apply_ToolSet_RuntimeRef(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.23.5"
		source: {
			url: "https://go.dev/dl/go1.23.5.linux-amd64.tar.gz"
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "~/.local/share/tomei/runtimes/go/1.23.5"
			GOBIN: "~/go/bin"
		}
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
			remove: "rm -f {{.BinPath}}"
		}
	}
}

goInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "go"
	spec: {
		type: "delegation"
		runtimeRef: "go"
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
		}
	}
}

goTools: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "ToolSet"
	metadata: name: "go-tools"
	spec: {
		runtimeRef: "go"
		tools: {
			gopls:     { package: "golang.org/x/tools/gopls", version: "v0.21.0" }
			goimports: { package: "golang.org/x/tools/cmd/goimports", version: "v0.31.0" }
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "toolset.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	mockTool.mu.Lock()
	assert.Contains(t, mockTool.installed, "gopls")
	assert.Contains(t, mockTool.installed, "goimports")
	assert.Equal(t, "v0.21.0", mockTool.installed["gopls"].Version)
	assert.Equal(t, "go", mockTool.installed["gopls"].RuntimeRef)
	mockTool.mu.Unlock()

	mockRuntime.mu.Lock()
	assert.Contains(t, mockRuntime.installed, "go")
	mockRuntime.mu.Unlock()
}

// TestEngine_Apply_ToolSet_MixedWithStandalone tests ToolSet alongside standalone Tools.
func TestEngine_Apply_ToolSet_MixedWithStandalone(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

aquaInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: type: "download"
}

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "aqua"
		version: "14.1.1"
		source: url: "https://example.com/rg.tar.gz"
	}
}

cliTools: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "ToolSet"
	metadata: name: "cli-tools"
	spec: {
		installerRef: "aqua"
		tools: {
			fd:  { version: "9.0.0" }
			bat: { version: "0.24.0" }
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "toolset.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	mockTool.mu.Lock()
	assert.Contains(t, mockTool.installed, "rg")
	assert.Contains(t, mockTool.installed, "fd")
	assert.Contains(t, mockTool.installed, "bat")
	assert.Equal(t, "14.1.1", mockTool.installed["rg"].Version)
	assert.Equal(t, "9.0.0", mockTool.installed["fd"].Version)
	assert.Equal(t, "0.24.0", mockTool.installed["bat"].Version)
	mockTool.mu.Unlock()
}

// TestEngine_Apply_ToolProgressCallback tests that the engine injects per-node progress callbacks
// into context and that those callbacks generate correct engine events with node-specific data.
func TestEngine_Apply_ToolProgressCallback(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "download"
		version: "14.0.0"
		source: { url: "https://example.com/rg.tar.gz" }
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tool.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := &mockToolInstaller{
		installed: make(map[string]*resource.ToolState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			// Extract per-node progress callback from context (same as real installer does)
			progressCb := download.CallbackFromContext[download.ProgressCallback](ctx)
			require.NotNil(t, progressCb, "progress callback should be injected into context by engine")

			// Simulate download progress
			progressCb(512, 1024)
			progressCb(1024, 1024)

			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				BinPath:      filepath.Join("/mock/bin", name),
			}, nil
		},
	}

	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	// Capture events to verify callback generates correct engine events
	var mu sync.Mutex
	var progressEvents []engine.Event
	eng.SetEventHandler(func(event engine.Event) {
		if event.Type == engine.EventProgress {
			mu.Lock()
			progressEvents = append(progressEvents, event)
			mu.Unlock()
		}
	})

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Verify progress events were generated with correct node-specific data
	mu.Lock()
	defer mu.Unlock()

	require.Len(t, progressEvents, 2)

	assert.Equal(t, resource.KindTool, progressEvents[0].Kind)
	assert.Equal(t, "ripgrep", progressEvents[0].Name)
	assert.Equal(t, "14.0.0", progressEvents[0].Version)
	assert.Equal(t, int64(512), progressEvents[0].Downloaded)
	assert.Equal(t, int64(1024), progressEvents[0].Total)
	assert.Equal(t, "download", progressEvents[0].Method)

	assert.Equal(t, int64(1024), progressEvents[1].Downloaded)
	assert.Equal(t, int64(1024), progressEvents[1].Total)
}

// TestEngine_Apply_ToolOutputCallback tests that the engine injects per-node output callbacks
// for delegation-pattern tools via context.
func TestEngine_Apply_ToolOutputCallback(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

jq: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "download"
		version: "1.7.1"
		source: { url: "https://example.com/jq" }
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tool.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := &mockToolInstaller{
		installed: make(map[string]*resource.ToolState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			// Extract per-node output callback from context
			outputCb := download.CallbackFromContext[download.OutputCallback](ctx)
			require.NotNil(t, outputCb, "output callback should be injected into context by engine")

			// Simulate command output
			outputCb("downloading jq...")
			outputCb("installing jq to /usr/local/bin")

			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				BinPath:      filepath.Join("/mock/bin", name),
			}, nil
		},
	}

	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	var mu sync.Mutex
	var outputEvents []engine.Event
	eng.SetEventHandler(func(event engine.Event) {
		if event.Type == engine.EventOutput {
			mu.Lock()
			outputEvents = append(outputEvents, event)
			mu.Unlock()
		}
	})

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, outputEvents, 2)

	assert.Equal(t, resource.KindTool, outputEvents[0].Kind)
	assert.Equal(t, "jq", outputEvents[0].Name)
	assert.Equal(t, "1.7.1", outputEvents[0].Version)
	assert.Equal(t, "downloading jq...", outputEvents[0].Output)

	assert.Equal(t, "installing jq to /usr/local/bin", outputEvents[1].Output)
}

// TestEngine_Apply_RuntimeProgressCallback tests that the engine injects per-node progress
// callbacks into context for runtime installations.
func TestEngine_Apply_RuntimeProgressCallback(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
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
			// Extract per-node progress callback from context
			progressCb := download.CallbackFromContext[download.ProgressCallback](ctx)
			require.NotNil(t, progressCb, "progress callback should be injected into context by engine")

			// Simulate download progress
			progressCb(0, 50_000_000)
			progressCb(25_000_000, 50_000_000)
			progressCb(50_000_000, 50_000_000)

			binDir := res.RuntimeSpec.BinDir
			if binDir == "" {
				binDir = res.RuntimeSpec.ToolBinPath
			}
			return &resource.RuntimeState{
				Type:        res.RuntimeSpec.Type,
				Version:     res.RuntimeSpec.Version,
				InstallPath: filepath.Join("/mock/runtimes", name, res.RuntimeSpec.Version),
				Binaries:    res.RuntimeSpec.Binaries,
				BinDir:      binDir,
				ToolBinPath: res.RuntimeSpec.ToolBinPath,
			}, nil
		},
	}

	mockTool := newMockToolInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	var mu sync.Mutex
	var progressEvents []engine.Event
	eng.SetEventHandler(func(event engine.Event) {
		if event.Type == engine.EventProgress {
			mu.Lock()
			progressEvents = append(progressEvents, event)
			mu.Unlock()
		}
	})

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, progressEvents, 3)

	// All events should have correct runtime-specific metadata
	for _, ev := range progressEvents {
		assert.Equal(t, resource.KindRuntime, ev.Kind)
		assert.Equal(t, "go", ev.Name)
		assert.Equal(t, "1.25.5", ev.Version)
	}

	assert.Equal(t, int64(0), progressEvents[0].Downloaded)
	assert.Equal(t, int64(50_000_000), progressEvents[0].Total)
	assert.Equal(t, int64(25_000_000), progressEvents[1].Downloaded)
	assert.Equal(t, int64(50_000_000), progressEvents[2].Downloaded)
}

// TestEngine_Apply_ParallelCallbackIsolation tests that parallel tool installations each receive
// their own isolated callback that generates events with the correct tool name.
func TestEngine_Apply_ParallelCallbackIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "download"
		version: "14.0.0"
		source: { url: "https://example.com/rg.tar.gz" }
	}
}

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "download"
		version: "9.0.0"
		source: { url: "https://example.com/fd.tar.gz" }
	}
}

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "download"
		version: "0.24.0"
		source: { url: "https://example.com/bat.tar.gz" }
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
			progressCb := download.CallbackFromContext[download.ProgressCallback](ctx)
			require.NotNil(t, progressCb)

			// Each tool reports progress with different values
			progressCb(100, 200) // Reports {100, 200} tagged with this tool's name

			time.Sleep(20 * time.Millisecond) // Allow interleaving

			progressCb(200, 200) // Reports {200, 200} tagged with this tool's name

			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				BinPath:      filepath.Join("/mock/bin", name),
			}, nil
		},
	}

	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	var mu sync.Mutex
	eventsByTool := make(map[string][]engine.Event)
	eng.SetEventHandler(func(event engine.Event) {
		if event.Type == engine.EventProgress {
			mu.Lock()
			eventsByTool[event.Name] = append(eventsByTool[event.Name], event)
			mu.Unlock()
		}
	})

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()

	// Each tool should have exactly 2 progress events
	for _, toolName := range []string{"ripgrep", "fd", "bat"} {
		events, ok := eventsByTool[toolName]
		require.True(t, ok, "should have events for %s", toolName)
		assert.Len(t, events, 2, "should have 2 progress events for %s", toolName)

		// Verify all events are correctly tagged with the right tool name
		for _, ev := range events {
			assert.Equal(t, toolName, ev.Name, "event should be tagged with correct tool name")
			assert.Equal(t, resource.KindTool, ev.Kind)
		}
	}

	// Verify no cross-contamination: total of 6 events (2 per tool * 3 tools)
	totalEvents := 0
	for _, events := range eventsByTool {
		totalEvents += len(events)
	}
	assert.Equal(t, 6, totalEvents)
}

// TestEngine_Apply_ParallelExecution tests that independent tools are installed concurrently.
func TestEngine_Apply_ParallelExecution(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "download"
		version: "14.0.0"
		source: { url: "https://example.com/rg" }
	}
}

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "download"
		version: "9.0.0"
		source: { url: "https://example.com/fd" }
	}
}

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	var mu sync.Mutex
	installed := make(map[string]*resource.ToolState)

	mockTool := &mockToolInstaller{
		installed: make(map[string]*resource.ToolState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			current := concurrentCount.Add(1)
			defer concurrentCount.Add(-1)

			for {
				old := maxConcurrent.Load()
				if current <= old || maxConcurrent.CompareAndSwap(old, current) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)

			st := &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				BinPath:      filepath.Join("/mock/bin", name),
			}
			mu.Lock()
			installed[name] = st
			mu.Unlock()
			return st, nil
		},
	}

	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// All tools should be installed
	assert.Len(t, installed, 3)
	assert.Contains(t, installed, "ripgrep")
	assert.Contains(t, installed, "fd")
	assert.Contains(t, installed, "bat")

	// Verify parallelism occurred
	assert.Greater(t, maxConcurrent.Load(), int32(1), "expected concurrent execution")

	// Verify state
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Len(t, st.Tools, 3)
}

// TestEngine_Apply_ParallelCancelOnError tests that a failure cancels other running tasks.
func TestEngine_Apply_ParallelCancelOnError(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "download"
		version: "14.0.0"
		source: { url: "https://example.com/rg" }
	}
}

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "download"
		version: "9.0.0"
		source: { url: "https://example.com/fd" }
	}
}

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var mu sync.Mutex
	installed := make(map[string]bool)

	mockTool := &mockToolInstaller{
		installed: make(map[string]*resource.ToolState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			if name == "fd" {
				return nil, fmt.Errorf("simulated install failure for fd")
			}

			// Other tools wait to be cancelled
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}

			mu.Lock()
			installed[name] = true
			mu.Unlock()

			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				BinPath:      filepath.Join("/mock/bin", name),
			}, nil
		},
	}

	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	err = eng.Apply(context.Background(), resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fd")
}

// TestEngine_Apply_ParallelismLimit tests that the parallelism limit is respected.
func TestEngine_Apply_ParallelismLimit(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei
`
	toolDefs := []struct{ cueKey, name string }{
		{"toolA", "tool-a"}, {"toolB", "tool-b"}, {"toolC", "tool-c"},
		{"toolD", "tool-d"}, {"toolE", "tool-e"}, {"toolF", "tool-f"},
	}
	for _, td := range toolDefs {
		cueContent += fmt.Sprintf(`
%s: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "%s"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: { url: "https://example.com/%s" }
	}
}
`, td.cueKey, td.name, td.name)
	}

	require.NoError(t, os.WriteFile(filepath.Join(configDir, "tools.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	mockTool := &mockToolInstaller{
		installed: make(map[string]*resource.ToolState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			current := concurrentCount.Add(1)
			defer concurrentCount.Add(-1)

			for {
				old := maxConcurrent.Load()
				if current <= old || maxConcurrent.CompareAndSwap(old, current) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)

			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				BinPath:      filepath.Join("/mock/bin", name),
			}, nil
		},
	}

	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)
	eng.SetParallelism(2)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	assert.LessOrEqual(t, maxConcurrent.Load(), int32(2), "concurrent execution should not exceed parallelism limit")
	assert.Greater(t, maxConcurrent.Load(), int32(0), "should have some concurrent execution")

	// Verify state
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Len(t, st.Tools, 6)
}

// TestEngine_Apply_RuntimeBeforeTool tests that Runtimes always execute before Tools.
func TestEngine_Apply_RuntimeBeforeTool(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// Runtime and Tool with no dependency between them
	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.5"
		source: {
			url: "https://go.dev/dl/go1.25.5.linux-amd64.tar.gz"
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
	}
}

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "download"
		version: "14.0.0"
		source: { url: "https://example.com/rg" }
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "resources.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var mu sync.Mutex
	var executionOrder []string

	mockTool := &mockToolInstaller{
		installed: make(map[string]*resource.ToolState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "Tool:"+name)
			mu.Unlock()
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				BinPath:      filepath.Join("/mock/bin", name),
			}, nil
		},
	}

	mockRuntime := &mockRuntimeInstaller{
		installed: make(map[string]*resource.RuntimeState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "Runtime:"+name)
			mu.Unlock()

			binDir := res.RuntimeSpec.BinDir
			if binDir == "" {
				binDir = res.RuntimeSpec.ToolBinPath
			}
			return &resource.RuntimeState{
				Type:        res.RuntimeSpec.Type,
				Version:     res.RuntimeSpec.Version,
				InstallPath: filepath.Join("/mock/runtimes", name, res.RuntimeSpec.Version),
				Binaries:    res.RuntimeSpec.Binaries,
				BinDir:      binDir,
				ToolBinPath: res.RuntimeSpec.ToolBinPath,
			}, nil
		},
	}

	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Find indices
	goIndex := -1
	rgIndex := -1
	for i, item := range executionOrder {
		switch item {
		case "Runtime:go":
			goIndex = i
		case "Tool:ripgrep":
			rgIndex = i
		}
	}

	assert.NotEqual(t, -1, goIndex, "go runtime should be installed")
	assert.NotEqual(t, -1, rgIndex, "ripgrep tool should be installed")
	assert.Less(t, goIndex, rgIndex, "runtime must complete before tool even without dependency")
}

// TestEngine_Apply_ParallelRuntimeExecution tests that multiple runtimes are installed concurrently.
func TestEngine_Apply_ParallelRuntimeExecution(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.5"
		source: { url: "https://go.dev/dl/go1.25.5.tar.gz" }
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
	}
}

rustRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "rust"
	spec: {
		type: "download"
		version: "1.80.0"
		source: { url: "https://example.com/rust.tar.gz" }
		binaries: ["rustc", "cargo"]
		toolBinPath: "~/.cargo/bin"
	}
}

nodeRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "node"
	spec: {
		type: "download"
		version: "22.0.0"
		source: { url: "https://example.com/node.tar.gz" }
		binaries: ["node", "npm"]
		toolBinPath: "~/.npm/bin"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "runtimes.cue"), []byte(cueContent), 0644))

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	mockRuntime := &mockRuntimeInstaller{
		installed: make(map[string]*resource.RuntimeState),
		removed:   make(map[string]bool),
		installFunc: func(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
			current := concurrentCount.Add(1)
			defer concurrentCount.Add(-1)

			for {
				old := maxConcurrent.Load()
				if current <= old || maxConcurrent.CompareAndSwap(old, current) {
					break
				}
			}

			time.Sleep(50 * time.Millisecond)

			binDir := res.RuntimeSpec.BinDir
			if binDir == "" {
				binDir = res.RuntimeSpec.ToolBinPath
			}
			return &resource.RuntimeState{
				Type:        res.RuntimeSpec.Type,
				Version:     res.RuntimeSpec.Version,
				InstallPath: filepath.Join("/mock/runtimes", name, res.RuntimeSpec.Version),
				Binaries:    res.RuntimeSpec.Binaries,
				BinDir:      binDir,
				ToolBinPath: res.RuntimeSpec.ToolBinPath,
			}, nil
		},
	}

	mockTool := newMockToolInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// All runtimes should be installed
	assert.Greater(t, maxConcurrent.Load(), int32(1), "expected concurrent runtime execution")

	// Verify state
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Len(t, st.Runtimes, 3)
	assert.Contains(t, st.Runtimes, "go")
	assert.Contains(t, st.Runtimes, "rust")
	assert.Contains(t, st.Runtimes, "node")
}

// TestEngine_Apply_RuntimeWithoutBinDir tests that BinDir defaults to ToolBinPath.
func TestEngine_Apply_RuntimeWithoutBinDir(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	stateDir := filepath.Join(tmpDir, "state")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	// CUE config without explicit BinDir (should default to ToolBinPath)
	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
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

	resources := loadResources(t, configDir)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), store)

	ctx := context.Background()

	// Apply
	err = eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Verify state: BinDir should default to ToolBinPath
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()

	assert.Contains(t, st.Runtimes, "go")
	assert.Equal(t, "~/go/bin", st.Runtimes["go"].BinDir)
	assert.Equal(t, "~/go/bin", st.Runtimes["go"].ToolBinPath)
}
