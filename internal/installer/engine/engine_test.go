package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/graph"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/tool"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
	"pgregory.net/rapid"
)

// mockToolInstaller is a mock implementation for testing.
type mockToolInstaller struct {
	installFunc func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error)
	removeFunc  func(ctx context.Context, st *resource.ToolState, name string) error
}

func (m *mockToolInstaller) Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
	if m.installFunc != nil {
		return m.installFunc(ctx, res, name)
	}
	return &resource.ToolState{
		InstallerRef: res.ToolSpec.InstallerRef,
		Version:      res.ToolSpec.Version,
		InstallPath:  "/tools/" + name,
		BinPath:      "/bin/" + name,
	}, nil
}

func (m *mockToolInstaller) Remove(ctx context.Context, st *resource.ToolState, name string) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, st, name)
	}
	return nil
}

func (m *mockToolInstaller) RegisterRuntime(_ string, _ *tool.RuntimeInfo) {}

func (m *mockToolInstaller) RegisterInstaller(_ string, _ *tool.InstallerInfo) {}

func (m *mockToolInstaller) SetToolBinPaths(_ map[string]string) {}

func (m *mockToolInstaller) SetProgressCallback(_ download.ProgressCallback) {}

func (m *mockToolInstaller) SetOutputCallback(_ download.OutputCallback) {}

// mockRuntimeInstaller is a mock implementation for testing.
type mockRuntimeInstaller struct {
	installFunc func(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error)
	removeFunc  func(ctx context.Context, st *resource.RuntimeState, name string) error
}

func (m *mockRuntimeInstaller) Install(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
	if m.installFunc != nil {
		return m.installFunc(ctx, res, name)
	}
	return &resource.RuntimeState{
		Type:           res.RuntimeSpec.Type,
		Version:        res.RuntimeSpec.Version,
		InstallPath:    "/runtimes/" + name + "/" + res.RuntimeSpec.Version,
		Binaries:       res.RuntimeSpec.Binaries,
		ToolBinPath:    res.RuntimeSpec.ToolBinPath,
		Env:            res.RuntimeSpec.Env,
		TaintOnUpgrade: res.RuntimeSpec.TaintOnUpgrade,
	}, nil
}

func (m *mockRuntimeInstaller) Remove(ctx context.Context, st *resource.RuntimeState, name string) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, st, name)
	}
	return nil
}

func (m *mockRuntimeInstaller) SetProgressCallback(_ download.ProgressCallback) {}

// mockInstallerRepositoryInstaller is a mock implementation for testing.
type mockInstallerRepositoryInstaller struct {
	installFunc func(ctx context.Context, res *resource.InstallerRepository, name string) (*resource.InstallerRepositoryState, error)
	removeFunc  func(ctx context.Context, st *resource.InstallerRepositoryState, name string) error
}

func (m *mockInstallerRepositoryInstaller) Install(ctx context.Context, res *resource.InstallerRepository, name string) (*resource.InstallerRepositoryState, error) {
	if m.installFunc != nil {
		return m.installFunc(ctx, res, name)
	}
	return &resource.InstallerRepositoryState{
		InstallerRef: res.InstallerRepositorySpec.InstallerRef,
		SourceType:   res.InstallerRepositorySpec.Source.Type,
		URL:          res.InstallerRepositorySpec.Source.URL,
	}, nil
}

func (m *mockInstallerRepositoryInstaller) Remove(ctx context.Context, st *resource.InstallerRepositoryState, name string) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, st, name)
	}
	return nil
}

func (m *mockInstallerRepositoryInstaller) SetToolBinPaths(_ map[string]string) {}

func TestNewEngine(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	store, err := state.NewStore[state.UserState](tmpDir)
	require.NoError(t, err)

	toolMock := &mockToolInstaller{}
	runtimeMock := &mockRuntimeInstaller{}
	engine := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	assert.NotNil(t, engine)
}

func TestEngine_Apply(t *testing.T) {
	t.Parallel()
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := `package tomei

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: {
				value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
			}
			archiveType: "tar.gz"
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup mock and store
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	installedTools := make(map[string]*resource.ToolState)
	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			st := &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name + "/" + res.ToolSpec.Version,
				BinPath:      "/bin/" + name,
			}
			installedTools[name] = st
			return st, nil
		},
	}
	runtimeMock := &mockRuntimeInstaller{}

	engine := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	// Run Apply
	err = engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Verify tool was installed
	assert.Contains(t, installedTools, "test-tool")
	assert.Equal(t, "1.0.0", installedTools["test-tool"].Version)

	// Verify state was updated
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)
	assert.NotNil(t, st.Tools["test-tool"])
	assert.Equal(t, "1.0.0", st.Tools["test-tool"].Version)
}

func TestEngine_Apply_NoChanges(t *testing.T) {
	t.Parallel()
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := `package tomei

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: {
				value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
			}
			archiveType: "tar.gz"
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup mock and store with pre-existing state
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with matching version
	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Tools["test-tool"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "1.0.0",
		InstallPath:  "/tools/test-tool/1.0.0",
		BinPath:      "/bin/test-tool",
	}
	err = store.Save(initialState)
	require.NoError(t, err)
	_ = store.Unlock()

	installCalled := false
	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			installCalled = true
			return nil, nil
		},
	}
	runtimeMock := &mockRuntimeInstaller{}

	engine := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	// Run Apply
	err = engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Install should not be called since version matches
	assert.False(t, installCalled)
}

func TestEngine_Apply_WithRuntime(t *testing.T) {
	t.Parallel()
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

runtime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "myruntime"
	spec: {
		type: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/myruntime-1.0.0.tar.gz"
			checksum: {
				value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
			}
		}
		binaries: ["mybin"]
		toolBinPath: "~/myruntime/bin"
		env: {
			MY_HOME: "/runtimes/myruntime/1.0.0"
		}
	}
}

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: {
				value: "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
			}
			archiveType: "tar.gz"
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup store
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	installedRuntimes := make(map[string]*resource.RuntimeState)
	installedTools := make(map[string]*resource.ToolState)

	runtimeMock := &mockRuntimeInstaller{
		installFunc: func(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
			st := &resource.RuntimeState{
				Type:        res.RuntimeSpec.Type,
				Version:     res.RuntimeSpec.Version,
				InstallPath: "/runtimes/" + name + "/" + res.RuntimeSpec.Version,
				Binaries:    res.RuntimeSpec.Binaries,
				ToolBinPath: res.RuntimeSpec.ToolBinPath,
				Env:         res.RuntimeSpec.Env,
			}
			installedRuntimes[name] = st
			return st, nil
		},
	}

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			st := &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name + "/" + res.ToolSpec.Version,
				BinPath:      "/bin/" + name,
			}
			installedTools[name] = st
			return st, nil
		},
	}

	engine := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	// Run Apply
	err = engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Verify runtime was installed
	assert.Contains(t, installedRuntimes, "myruntime")
	assert.Equal(t, "1.0.0", installedRuntimes["myruntime"].Version)

	// Verify tool was installed
	assert.Contains(t, installedTools, "test-tool")
	assert.Equal(t, "1.0.0", installedTools["test-tool"].Version)

	// Verify state was updated
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)
	assert.NotNil(t, st.Runtimes["myruntime"])
	assert.NotNil(t, st.Tools["test-tool"])
}

func TestEngine_TaintDependentTools(t *testing.T) {
	t.Parallel()
	// Create test config directory with CUE file
	// Include both runtime and dependent tool - tool should be tainted when runtime is upgraded
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

runtime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.26.0"
		source: {
			url: "https://example.com/go-1.26.0.tar.gz"
			checksum: {
				value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
			}
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		commands: {
			install: ["go install {{.Package}}@{{.Version}}"]
		}
		taintOnUpgrade: true
	}
}

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "gopls"
	spec: {
		runtimeRef: "go"
		version: "0.16.0"
		package: "golang.org/x/tools/gopls"
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup store with pre-existing state
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with runtime at version 1.25.0 and dependent tool
	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Runtimes["go"] = &resource.RuntimeState{
		Type:           resource.InstallTypeDownload,
		Version:        "1.25.0",
		InstallPath:    "/runtimes/go/1.25.0",
		Binaries:       []string{"go", "gofmt"},
		TaintOnUpgrade: true,
	}
	initialState.Tools["gopls"] = &resource.ToolState{
		RuntimeRef:  "go", // depends on go runtime
		Version:     "0.16.0",
		InstallPath: "/tools/gopls/0.16.0",
		BinPath:     "/bin/gopls",
	}
	err = store.Save(initialState)
	require.NoError(t, err)
	_ = store.Unlock()

	runtimeInstallCalled := false
	runtimeMock := &mockRuntimeInstaller{
		installFunc: func(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
			runtimeInstallCalled = true
			return &resource.RuntimeState{
				Type:           res.RuntimeSpec.Type,
				Version:        res.RuntimeSpec.Version,
				InstallPath:    "/runtimes/" + name + "/" + res.RuntimeSpec.Version,
				Binaries:       res.RuntimeSpec.Binaries,
				TaintOnUpgrade: res.RuntimeSpec.TaintOnUpgrade,
			}, nil
		},
	}

	toolReinstallCalled := false
	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			toolReinstallCalled = true
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				RuntimeRef:   res.ToolSpec.RuntimeRef,
				InstallPath:  "/tools/" + name + "/" + res.ToolSpec.Version,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	engine := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	// Run Apply - should upgrade runtime and reinstall tainted tools
	err = engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Verify runtime was upgraded
	assert.True(t, runtimeInstallCalled)

	// Verify tool was reinstalled due to taint
	assert.True(t, toolReinstallCalled, "tool should be reinstalled after runtime upgrade")

	// Verify final state
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)

	assert.NotNil(t, st.Runtimes["go"])
	assert.Equal(t, "1.26.0", st.Runtimes["go"].Version)

	// Tool should exist and no longer be tainted (it was reinstalled)
	assert.NotNil(t, st.Tools["gopls"])
	assert.False(t, st.Tools["gopls"].IsTainted(), "tool should not be tainted after reinstall")
}

func TestEngine_TaintDependentTools_EmitsEvents(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

runtime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.26.0"
		source: {
			url: "https://example.com/go-1.26.0.tar.gz"
			checksum: {
				value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
			}
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		commands: {
			install: ["go install {{.Package}}@{{.Version}}"]
		}
		taintOnUpgrade: true
	}
}

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "gopls"
	spec: {
		runtimeRef: "go"
		version: "0.16.0"
		package: "golang.org/x/tools/gopls"
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state: runtime at older version, tool depends on it
	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Runtimes["go"] = &resource.RuntimeState{
		Type:           resource.InstallTypeDownload,
		Version:        "1.25.0",
		InstallPath:    "/runtimes/go/1.25.0",
		Binaries:       []string{"go", "gofmt"},
		TaintOnUpgrade: true,
	}
	initialState.Tools["gopls"] = &resource.ToolState{
		RuntimeRef:  "go",
		Version:     "0.16.0",
		InstallPath: "/tools/gopls/0.16.0",
		BinPath:     "/bin/gopls",
	}
	require.NoError(t, store.Save(initialState))
	require.NoError(t, store.Unlock())

	runtimeMock := &mockRuntimeInstaller{}
	toolMock := &mockToolInstaller{}

	// Collect events
	var mu sync.Mutex
	var events []Event
	collectEvents := func(event Event) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}

	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)
	eng.SetEventHandler(collectEvents)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Find taint phase events
	var taintLayerStarts []Event
	var taintStarts []Event
	var taintCompletes []Event
	for _, e := range events {
		if e.Phase == PhaseTaint {
			switch e.Type {
			case EventLayerStart:
				taintLayerStarts = append(taintLayerStarts, e)
			case EventStart:
				taintStarts = append(taintStarts, e)
			case EventComplete:
				taintCompletes = append(taintCompletes, e)
			}
		}
	}

	// Should have 1 taint layer start with gopls in layer nodes
	require.Len(t, taintLayerStarts, 1, "expected 1 PhaseTaint EventLayerStart")
	assert.Contains(t, taintLayerStarts[0].LayerNodes, "Tool/gopls")

	// Should have 1 taint start and 1 complete for gopls
	require.Len(t, taintStarts, 1, "expected 1 PhaseTaint EventStart")
	assert.Equal(t, "gopls", taintStarts[0].Name)
	assert.Equal(t, resource.KindTool, taintStarts[0].Kind)

	require.Len(t, taintCompletes, 1, "expected 1 PhaseTaint EventComplete")
	assert.Equal(t, "gopls", taintCompletes[0].Name)
}

// TestEngine_TaintDependentTools_Disabled verifies that when TaintOnUpgrade is false,
// runtime upgrade does NOT taint dependent tools.
func TestEngine_TaintDependentTools_Disabled(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

runtime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.26.0"
		source: {
			url: "https://example.com/go-1.26.0.tar.gz"
			checksum: {
				value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
			}
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		commands: {
			install: ["go install {{.Package}}@{{.Version}}"]
		}
		// taintOnUpgrade defaults to false — no taint on upgrade
	}
}

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "gopls"
	spec: {
		runtimeRef: "go"
		version: "0.16.0"
		package: "golang.org/x/tools/gopls"
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state: runtime at older version, tool depends on it
	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Runtimes["go"] = &resource.RuntimeState{
		Type:           resource.InstallTypeDownload,
		Version:        "1.25.0",
		InstallPath:    "/runtimes/go/1.25.0",
		Binaries:       []string{"go", "gofmt"},
		TaintOnUpgrade: false, // explicitly disabled
	}
	initialState.Tools["gopls"] = &resource.ToolState{
		RuntimeRef:  "go",
		Version:     "0.16.0",
		InstallPath: "/tools/gopls/0.16.0",
		BinPath:     "/bin/gopls",
	}
	require.NoError(t, store.Save(initialState))
	require.NoError(t, store.Unlock())

	runtimeMock := &mockRuntimeInstaller{}

	toolInstallCalled := false
	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			toolInstallCalled = true
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				RuntimeRef:   res.ToolSpec.RuntimeRef,
				InstallPath:  "/tools/" + name + "/" + res.ToolSpec.Version,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Tool should NOT be reinstalled because TaintOnUpgrade is false
	assert.False(t, toolInstallCalled, "tool should not be reinstalled when TaintOnUpgrade is false")

	// Verify tool is not tainted in state
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)

	assert.NotNil(t, st.Tools["gopls"])
	assert.False(t, st.Tools["gopls"].IsTainted(), "tool should not be tainted")
	assert.Equal(t, "0.16.0", st.Tools["gopls"].Version)

	// Runtime should be upgraded
	assert.Equal(t, "1.26.0", st.Runtimes["go"].Version)
}

func TestEngine_Removal_EmitsEvents(t *testing.T) {
	t.Parallel()

	// Config with only "bat" — "fzf" is in state and should be removed
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "download"
		version: "0.25.0"
		source: {
			url: "https://example.com/bat-0.25.0.tar.gz"
			checksum: { value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" }
		}
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with bat and fzf
	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Tools["bat"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "0.25.0",
		BinPath:      "/bin/bat",
	}
	initialState.Tools["fzf"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "0.44.0",
		BinPath:      "/bin/fzf",
	}
	require.NoError(t, store.Save(initialState))
	require.NoError(t, store.Unlock())

	toolMock := &mockToolInstaller{
		installFunc: func(_ context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	// Collect events
	var mu sync.Mutex
	var events []Event
	collectEvents := func(event Event) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}

	eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)
	eng.SetEventHandler(collectEvents)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Verify PhaseRemove events
	var removeLayerStarts, removeStarts, removeCompletes []Event
	for _, e := range events {
		if e.Phase == PhaseRemove {
			switch e.Type {
			case EventLayerStart:
				removeLayerStarts = append(removeLayerStarts, e)
			case EventStart:
				removeStarts = append(removeStarts, e)
			case EventComplete:
				removeCompletes = append(removeCompletes, e)
			}
		}
	}

	require.Len(t, removeLayerStarts, 1, "expected 1 PhaseRemove EventLayerStart")
	assert.Contains(t, removeLayerStarts[0].LayerNodes, "Tool/fzf")

	require.Len(t, removeStarts, 1, "expected 1 PhaseRemove EventStart for fzf")
	assert.Equal(t, "fzf", removeStarts[0].Name)
	assert.Equal(t, resource.ActionRemove, removeStarts[0].Action)

	require.Len(t, removeCompletes, 1, "expected 1 PhaseRemove EventComplete for fzf")
	assert.Equal(t, "fzf", removeCompletes[0].Name)
}

func TestEngine_Apply_DependencyOrder(t *testing.T) {
	t.Parallel()
	// Test that DAG-based execution respects dependency order:
	// Runtime(go) -> Tool(pnpm) -> Installer(pnpm) -> Tool(biome)
	// Tool can directly reference Runtime via runtimeRef
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.23.0"
		source: {
			url: "https://example.com/go-1.23.0.tar.gz"
			checksum: { value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" }
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "/runtimes/go/1.23.0"
			GOBIN: "~/go/bin"
		}
		commands: {
			install: ["go install {{.Package}}@{{.Version}}"]
		}
	}
}

pnpmTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "pnpm"
	spec: {
		runtimeRef: "go"
		package: "github.com/pnpm/pnpm"
		version: "v8.0.0"
	}
}

pnpmInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "pnpm"
	spec: {
		type: "delegation"
		toolRef: "pnpm"
		commands: {
			install: ["pnpm add -g {{.Package}}@{{.Version}}"]
		}
	}
}

biomeTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "biome"
	spec: {
		installerRef: "pnpm"
		package: "@biomejs/biome"
		version: "1.5.0"
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Track execution order (protected by mutex for parallel execution)
	var mu sync.Mutex
	var executionOrder []string

	// Setup store
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	runtimeMock := &mockRuntimeInstaller{
		installFunc: func(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "Runtime:"+name)
			mu.Unlock()
			return &resource.RuntimeState{
				Type:        res.RuntimeSpec.Type,
				Version:     res.RuntimeSpec.Version,
				InstallPath: "/runtimes/" + name + "/" + res.RuntimeSpec.Version,
				Binaries:    res.RuntimeSpec.Binaries,
				ToolBinPath: res.RuntimeSpec.ToolBinPath,
				Env:         res.RuntimeSpec.Env,
				Commands:    res.RuntimeSpec.Commands,
			}, nil
		},
	}

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "Tool:"+name)
			mu.Unlock()
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	engine := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	// Run Apply
	err = engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Verify execution order respects dependencies
	// Runtime:go must come before Tool:pnpm
	// Tool:pnpm must come before Tool:biome
	goIndex := -1
	pnpmIndex := -1
	biomeIndex := -1

	for i, item := range executionOrder {
		switch item {
		case "Runtime:go":
			goIndex = i
		case "Tool:pnpm":
			pnpmIndex = i
		case "Tool:biome":
			biomeIndex = i
		}
	}

	assert.NotEqual(t, -1, goIndex, "go runtime should be installed")
	assert.NotEqual(t, -1, pnpmIndex, "pnpm tool should be installed")
	assert.NotEqual(t, -1, biomeIndex, "biome tool should be installed")

	assert.Less(t, goIndex, pnpmIndex, "go runtime must be installed before pnpm tool")
	assert.Less(t, pnpmIndex, biomeIndex, "pnpm tool must be installed before biome tool")
}

func TestEngine_Apply_CircularDependency(t *testing.T) {
	t.Parallel()
	// Test that circular dependencies are detected and rejected
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

installerA: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "installer-a"
	spec: {
		type: "delegation"
		toolRef: "tool-b"
		commands: {
			install: ["install-a {{.Package}}"]
		}
	}
}

toolB: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool-b"
	spec: {
		installerRef: "installer-a"
		version: "1.0.0"
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup store
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	engine := NewEngine(&mockToolInstaller{}, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

	// Run Apply - should fail due to circular dependency
	err = engine.Apply(context.Background(), resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestEngine_Apply_ParallelExecution(t *testing.T) {
	t.Parallel()
	// Test that independent tools are executed in parallel
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

aquaInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: {
		type: "download"
	}
}

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "aqua"
		version: "14.0.0"
		source: {
			url: "https://example.com/ripgrep.tar.gz"
			checksum: { value: "sha256:1111111111111111111111111111111111111111111111111111111111111111" }
		}
	}
}

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version: "9.0.0"
		source: {
			url: "https://example.com/fd.tar.gz"
			checksum: { value: "sha256:2222222222222222222222222222222222222222222222222222222222222222" }
		}
	}
}

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "aqua"
		version: "0.24.0"
		source: {
			url: "https://example.com/bat.tar.gz"
			checksum: { value: "sha256:3333333333333333333333333333333333333333333333333333333333333333" }
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup store
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var mu sync.Mutex
	installedTools := make(map[string]bool)

	// Track concurrent execution
	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			current := concurrentCount.Add(1)
			defer concurrentCount.Add(-1)

			// Update max concurrent (CAS loop)
			for {
				old := maxConcurrent.Load()
				if current <= old || maxConcurrent.CompareAndSwap(old, current) {
					break
				}
			}

			// Simulate work to allow overlap
			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			installedTools[name] = true
			mu.Unlock()

			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	engine := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

	// Run Apply
	err = engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	// All three tools should be installed
	assert.True(t, installedTools["ripgrep"])
	assert.True(t, installedTools["fd"])
	assert.True(t, installedTools["bat"])

	// Verify actual parallelism occurred
	assert.Greater(t, maxConcurrent.Load(), int32(1), "expected concurrent execution of independent tools")
}

func TestEngine_Apply_ParallelExecution_ContinueOnError(t *testing.T) {
	t.Parallel()
	// Test that when one tool fails in a parallel layer, other tools continue to completion
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

aquaInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: {
		type: "download"
	}
}

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "aqua"
		version: "14.0.0"
		source: {
			url: "https://example.com/ripgrep.tar.gz"
			checksum: { value: "sha256:1111111111111111111111111111111111111111111111111111111111111111" }
		}
	}
}

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version: "9.0.0"
		source: {
			url: "https://example.com/fd.tar.gz"
			checksum: { value: "sha256:2222222222222222222222222222222222222222222222222222222222222222" }
		}
	}
}

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "aqua"
		version: "0.24.0"
		source: {
			url: "https://example.com/bat.tar.gz"
			checksum: { value: "sha256:3333333333333333333333333333333333333333333333333333333333333333" }
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var mu sync.Mutex
	installedTools := make(map[string]bool)

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			// fd fails immediately
			if name == "fd" {
				return nil, fmt.Errorf("simulated install failure for fd")
			}

			// Other tools simulate work (no cancellation check needed)
			time.Sleep(50 * time.Millisecond)

			mu.Lock()
			installedTools[name] = true
			mu.Unlock()

			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

	// Run Apply - should return error for fd
	err = eng.Apply(context.Background(), resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fd")

	// fd should not be installed
	assert.False(t, installedTools["fd"], "fd should not be installed")

	// Other tools should have completed despite fd's failure
	assert.True(t, installedTools["ripgrep"], "ripgrep should be installed")
	assert.True(t, installedTools["bat"], "bat should be installed")

	// State should be flushed with successful tools
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, loadErr := store.Load()
	require.NoError(t, loadErr)
	assert.Contains(t, st.Tools, "ripgrep", "ripgrep should be in state")
	assert.Contains(t, st.Tools, "bat", "bat should be in state")
	assert.NotContains(t, st.Tools, "fd", "fd should not be in state")
}

func TestEngine_Apply_ParallelExecution_MultipleErrors(t *testing.T) {
	t.Parallel()
	// Test that multiple failures in the same layer are all reported
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

aquaInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: {
		type: "download"
	}
}

toolA: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool-a"
	spec: {
		installerRef: "aqua"
		version: "1.0.0"
		source: {
			url: "https://example.com/a.tar.gz"
			checksum: { value: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" }
		}
	}
}

toolB: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool-b"
	spec: {
		installerRef: "aqua"
		version: "1.0.0"
		source: {
			url: "https://example.com/b.tar.gz"
			checksum: { value: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" }
		}
	}
}

toolC: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "tool-c"
	spec: {
		installerRef: "aqua"
		version: "1.0.0"
		source: {
			url: "https://example.com/c.tar.gz"
			checksum: { value: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc" }
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			// tool-a and tool-b both fail
			if name == "tool-a" {
				return nil, fmt.Errorf("failure-a")
			}
			if name == "tool-b" {
				return nil, fmt.Errorf("failure-b")
			}
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

	err = eng.Apply(context.Background(), resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failure-a")
	assert.Contains(t, err.Error(), "failure-b")

	// tool-c should be in state despite other failures
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, loadErr := store.Load()
	require.NoError(t, loadErr)
	assert.Contains(t, st.Tools, "tool-c", "tool-c should be in state")
}

func TestEngine_Apply_RuntimeBeforeTool_SameLayer(t *testing.T) {
	t.Parallel()
	// Test that Runtime nodes always execute before Tool nodes even in the same layer
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.0"
		source: {
			url: "https://example.com/go.tar.gz"
			checksum: { value: "sha256:4444444444444444444444444444444444444444444444444444444444444444" }
		}
		binaries: ["go"]
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
		source: {
			url: "https://example.com/ripgrep.tar.gz"
			checksum: { value: "sha256:1111111111111111111111111111111111111111111111111111111111111111" }
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var mu sync.Mutex
	var executionOrder []string

	runtimeMock := &mockRuntimeInstaller{
		installFunc: func(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "Runtime:"+name)
			mu.Unlock()
			return &resource.RuntimeState{
				Type:        res.RuntimeSpec.Type,
				Version:     res.RuntimeSpec.Version,
				InstallPath: "/runtimes/" + name,
				Binaries:    res.RuntimeSpec.Binaries,
				ToolBinPath: res.RuntimeSpec.ToolBinPath,
			}, nil
		},
	}

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			mu.Lock()
			executionOrder = append(executionOrder, "Tool:"+name)
			mu.Unlock()
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Runtime must always come before Tool
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
	assert.Less(t, goIndex, rgIndex, "runtime must be installed before tool even without dependency")
}

func TestEngine_Apply_ParallelRuntimeExecution(t *testing.T) {
	t.Parallel()
	// Test that multiple independent runtimes are executed in parallel
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.0"
		source: {
			url: "https://example.com/go.tar.gz"
			checksum: { value: "sha256:4444444444444444444444444444444444444444444444444444444444444444" }
		}
		binaries: ["go"]
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
		source: {
			url: "https://example.com/rust.tar.gz"
			checksum: { value: "sha256:5555555555555555555555555555555555555555555555555555555555555555" }
		}
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
		source: {
			url: "https://example.com/node.tar.gz"
			checksum: { value: "sha256:6666666666666666666666666666666666666666666666666666666666666666" }
		}
		binaries: ["node", "npm"]
		toolBinPath: "~/.npm/bin"
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32
	var mu sync.Mutex
	installedRuntimes := make(map[string]bool)

	runtimeMock := &mockRuntimeInstaller{
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

			mu.Lock()
			installedRuntimes[name] = true
			mu.Unlock()

			return &resource.RuntimeState{
				Type:        res.RuntimeSpec.Type,
				Version:     res.RuntimeSpec.Version,
				InstallPath: "/runtimes/" + name,
				Binaries:    res.RuntimeSpec.Binaries,
				ToolBinPath: res.RuntimeSpec.ToolBinPath,
			}, nil
		},
	}

	eng := NewEngine(&mockToolInstaller{}, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// All runtimes should be installed
	assert.True(t, installedRuntimes["go"])
	assert.True(t, installedRuntimes["rust"])
	assert.True(t, installedRuntimes["node"])

	// Verify actual parallelism occurred
	assert.Greater(t, maxConcurrent.Load(), int32(1), "expected concurrent execution of independent runtimes")
}

func TestEngine_SetParallelism(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input int
		want  int
	}{
		{name: "default", input: 5, want: 5},
		{name: "minimum clamped", input: 0, want: 1},
		{name: "negative clamped", input: -1, want: 1},
		{name: "maximum clamped", input: 100, want: MaxParallelism},
		{name: "valid value", input: 10, want: 10},
		{name: "one", input: 1, want: 1},
		{name: "max boundary", input: MaxParallelism, want: MaxParallelism},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			store, err := state.NewStore[state.UserState](stateDir)
			require.NoError(t, err)

			eng := NewEngine(&mockToolInstaller{}, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)
			eng.SetParallelism(tt.input)
			assert.Equal(t, tt.want, eng.parallelism)
		})
	}
}

func TestEngine_Apply_ParallelismLimit(t *testing.T) {
	t.Parallel()
	// Test that parallelism is limited to the configured value
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")

	// Create 6 tools to exceed parallelism limit of 2
	var sb strings.Builder
	sb.WriteString(`package tomei

aquaInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: { type: "download" }
}
`)
	toolDefs := []struct{ cueKey, name string }{
		{"toolA", "tool-a"}, {"toolB", "tool-b"}, {"toolC", "tool-c"},
		{"toolD", "tool-d"}, {"toolE", "tool-e"}, {"toolF", "tool-f"},
	}
	for _, td := range toolDefs {
		fmt.Fprintf(&sb, `
%s: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "%s"
	spec: {
		installerRef: "aqua"
		version: "1.0.0"
		source: {
			url: "https://example.com/%s.tar.gz"
			checksum: { value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" }
		}
	}
}
`, td.cueKey, td.name, td.name)
	}

	err := os.WriteFile(cueFile, []byte(sb.String()), 0644)
	require.NoError(t, err)

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var concurrentCount atomic.Int32
	var maxConcurrent atomic.Int32

	toolMock := &mockToolInstaller{
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
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)
	eng.SetParallelism(2) // Limit to 2 concurrent

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Max concurrent should not exceed the parallelism limit
	assert.LessOrEqual(t, maxConcurrent.Load(), int32(2), "concurrent execution should not exceed parallelism limit")
	assert.Positive(t, maxConcurrent.Load(), "should have some concurrent execution")
}

func TestEngine_ResolverConfigurer(t *testing.T) {
	t.Parallel()
	// Test that ResolverConfigurer callback is called after state is loaded
	// but before any installation happens
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := `package tomei

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: {
				value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
			}
			archiveType: "tar.gz"
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup store with pre-existing registry state
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with registry info (simulating tomei init)
	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Registry = &state.RegistryState{
		Aqua: &state.AquaRegistryState{
			Ref: "v4.465.0",
		},
	}
	err = store.Save(initialState)
	require.NoError(t, err)
	_ = store.Unlock()

	// Track when configurer is called
	configurerCalled := false
	var capturedRef string
	installCalled := false

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			installCalled = true
			// Verify configurer was called before install
			assert.True(t, configurerCalled, "configurer should be called before install")
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	engine := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

	// Set resolver configurer
	engine.SetResolverConfigurer(func(st *state.UserState) error {
		configurerCalled = true
		if st.Registry != nil && st.Registry.Aqua != nil {
			capturedRef = st.Registry.Aqua.Ref
		}
		return nil
	})

	// Run Apply
	err = engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Verify configurer was called with correct state
	assert.True(t, configurerCalled, "configurer should be called")
	assert.Equal(t, "v4.465.0", capturedRef, "configurer should receive state with registry ref")

	// Verify install was called after configurer
	assert.True(t, installCalled, "install should be called")
}

func TestEngine_ResolverConfigurer_NilRegistry(t *testing.T) {
	t.Parallel()
	// Test that ResolverConfigurer handles nil registry gracefully
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := `package tomei

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: { value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" }
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// No pre-populated state (simulating fresh install without tomei init)

	configurerCalled := false
	var registryIsNil bool

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	engine := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

	engine.SetResolverConfigurer(func(st *state.UserState) error {
		configurerCalled = true
		registryIsNil = (st.Registry == nil)
		return nil
	})

	err = engine.Apply(context.Background(), resources)
	require.NoError(t, err)

	assert.True(t, configurerCalled)
	assert.True(t, registryIsNil, "registry should be nil when not initialized")
}

func TestEngine_PlanAll(t *testing.T) {
	t.Parallel()
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

runtime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "myruntime"
	spec: {
		type: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/myruntime.tar.gz"
			checksum: { value: "sha256:7777777777777777777777777777777777777777777777777777777777777777" }
		}
		binaries: ["mybin"]
		toolBinPath: "~/bin"
	}
}

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: { value: "sha256:8888888888888888888888888888888888888888888888888888888888888888" }
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup store
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	engine := NewEngine(&mockToolInstaller{}, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

	// Run PlanAll
	runtimeActions, _, toolActions, err := engine.PlanAll(context.Background(), resources)
	require.NoError(t, err)

	// Should have one runtime install action
	require.Len(t, runtimeActions, 1)
	assert.Equal(t, resource.ActionInstall, runtimeActions[0].Type)
	assert.Equal(t, "myruntime", runtimeActions[0].Name)

	// Should have one tool install action
	require.Len(t, toolActions, 1)
	assert.Equal(t, resource.ActionInstall, toolActions[0].Type)
	assert.Equal(t, "test-tool", toolActions[0].Name)
}

// --- CUE manifest generation helpers for property tests ---

// generateToolsCUE generates a CUE manifest with an Installer and N tools.
func generateToolsCUE(toolNames []string) string {
	var sb strings.Builder
	sb.WriteString(`package tomei

aquaInstaller: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: { type: "download" }
}
`)
	for _, name := range toolNames {
		fmt.Fprintf(&sb, `
%s: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "%s"
	spec: {
		installerRef: "aqua"
		version: "1.0.0"
		source: {
			url: "https://example.com/%s.tar.gz"
			checksum: { value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" }
		}
	}
}
`, name, name, name)
	}
	return sb.String()
}

// generateRuntimesAndToolsCUE generates a CUE manifest with N runtimes and M tools.
func generateRuntimesAndToolsCUE(runtimeNames, toolNames []string) string {
	var sb strings.Builder
	sb.WriteString("package tomei\n")

	for _, name := range runtimeNames {
		fmt.Fprintf(&sb, `
%sRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "%s"
	spec: {
		type: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/%s.tar.gz"
			checksum: { value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" }
		}
		binaries: ["%s"]
		toolBinPath: "~/%s/bin"
	}
}
`, name, name, name, name, name)
	}

	for _, name := range toolNames {
		fmt.Fprintf(&sb, `
%s: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "%s"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/%s.tar.gz"
			checksum: { value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" }
		}
	}
}
`, name, name, name)
	}

	return sb.String()
}

// newStoreForRapid creates a locked state store for rapid tests.
func newStoreForRapid(t *rapid.T) *state.Store[state.UserState] {
	dir, err := os.MkdirTemp("", "engine-prop-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore[state.UserState](dir)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// --- Property-Based Tests ---

func TestEngine_Property_ParallelSafety(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 15).Draw(t, "numTools")
		toolNames := make([]string, n)
		for i := range n {
			toolNames[i] = fmt.Sprintf("tool%d", i)
		}

		cue := generateToolsCUE(toolNames)
		configDir, err := os.MkdirTemp("", "engine-prop-config-*")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(configDir) })
		if err := os.WriteFile(filepath.Join(configDir, "resources.cue"), []byte(cue), 0644); err != nil {
			t.Fatal(err)
		}
		loader := config.NewLoader(nil)
		resources, err := loader.Load(configDir)
		if err != nil {
			t.Fatal(err)
		}

		store := newStoreForRapid(t)

		var mu sync.Mutex
		installed := make(map[string]bool)

		toolMock := &mockToolInstaller{
			installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
				time.Sleep(5 * time.Millisecond)
				mu.Lock()
				installed[name] = true
				mu.Unlock()
				return &resource.ToolState{
					InstallerRef: res.ToolSpec.InstallerRef,
					Version:      res.ToolSpec.Version,
					InstallPath:  "/tools/" + name,
					BinPath:      "/bin/" + name,
				}, nil
			},
		}

		eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

		if err := eng.Apply(context.Background(), resources); err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		// Property: every tool must be installed
		for _, name := range toolNames {
			if !installed[name] {
				t.Fatalf("tool %s not installed", name)
			}
		}

		// Property: state must contain all tools
		if err := store.Lock(); err != nil {
			t.Fatal(err)
		}
		st, err := store.Load()
		if err != nil {
			t.Fatal(err)
		}
		_ = store.Unlock()

		for _, name := range toolNames {
			if _, ok := st.Tools[name]; !ok {
				t.Fatalf("tool %s missing from state", name)
			}
		}
	})
}

func TestEngine_Property_RuntimeBeforeTool(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		nRuntimes := rapid.IntRange(1, 5).Draw(t, "numRuntimes")
		nTools := rapid.IntRange(1, 5).Draw(t, "numTools")

		runtimeNames := make([]string, nRuntimes)
		for i := range nRuntimes {
			runtimeNames[i] = fmt.Sprintf("rt%d", i)
		}
		toolNames := make([]string, nTools)
		for i := range nTools {
			toolNames[i] = fmt.Sprintf("tl%d", i)
		}

		cue := generateRuntimesAndToolsCUE(runtimeNames, toolNames)
		configDir, err := os.MkdirTemp("", "engine-prop-config-*")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(configDir) })
		if err := os.WriteFile(filepath.Join(configDir, "resources.cue"), []byte(cue), 0644); err != nil {
			t.Fatal(err)
		}
		loader := config.NewLoader(nil)
		resources, err := loader.Load(configDir)
		if err != nil {
			t.Fatal(err)
		}

		store := newStoreForRapid(t)

		var mu sync.Mutex
		var order []string

		runtimeMock := &mockRuntimeInstaller{
			installFunc: func(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
				time.Sleep(5 * time.Millisecond)
				mu.Lock()
				order = append(order, "R:"+name)
				mu.Unlock()
				return &resource.RuntimeState{
					Type:        res.RuntimeSpec.Type,
					Version:     res.RuntimeSpec.Version,
					InstallPath: "/runtimes/" + name,
					Binaries:    res.RuntimeSpec.Binaries,
					ToolBinPath: res.RuntimeSpec.ToolBinPath,
				}, nil
			},
		}

		toolMock := &mockToolInstaller{
			installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
				mu.Lock()
				order = append(order, "T:"+name)
				mu.Unlock()
				return &resource.ToolState{
					InstallerRef: res.ToolSpec.InstallerRef,
					Version:      res.ToolSpec.Version,
					InstallPath:  "/tools/" + name,
					BinPath:      "/bin/" + name,
				}, nil
			},
		}

		eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

		if err := eng.Apply(context.Background(), resources); err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		// Property: all runtime entries must appear before all tool entries
		lastRuntimeIdx := -1
		firstToolIdx := len(order)
		for i, entry := range order {
			if strings.HasPrefix(entry, "R:") {
				if i > lastRuntimeIdx {
					lastRuntimeIdx = i
				}
			}
			if strings.HasPrefix(entry, "T:") {
				if i < firstToolIdx {
					firstToolIdx = i
				}
			}
		}

		if lastRuntimeIdx >= firstToolIdx {
			t.Fatalf("runtime executed after tool: order=%v lastRuntime=%d firstTool=%d", order, lastRuntimeIdx, firstToolIdx)
		}
	})
}

func TestEngine_Property_ParallelismLimit(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		nTools := rapid.IntRange(2, 20).Draw(t, "numTools")
		parallelism := rapid.IntRange(1, 10).Draw(t, "parallelism")

		toolNames := make([]string, nTools)
		for i := range nTools {
			toolNames[i] = fmt.Sprintf("tool%d", i)
		}

		cue := generateToolsCUE(toolNames)
		configDir, err := os.MkdirTemp("", "engine-prop-config-*")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(configDir) })
		if err := os.WriteFile(filepath.Join(configDir, "resources.cue"), []byte(cue), 0644); err != nil {
			t.Fatal(err)
		}
		loader := config.NewLoader(nil)
		resources, err := loader.Load(configDir)
		if err != nil {
			t.Fatal(err)
		}

		store := newStoreForRapid(t)

		var concurrentCount atomic.Int32
		var maxConcurrent atomic.Int32

		toolMock := &mockToolInstaller{
			installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
				current := concurrentCount.Add(1)
				defer concurrentCount.Add(-1)

				for {
					old := maxConcurrent.Load()
					if current <= old || maxConcurrent.CompareAndSwap(old, current) {
						break
					}
				}

				time.Sleep(5 * time.Millisecond)

				return &resource.ToolState{
					InstallerRef: res.ToolSpec.InstallerRef,
					Version:      res.ToolSpec.Version,
					InstallPath:  "/tools/" + name,
					BinPath:      "/bin/" + name,
				}, nil
			},
		}

		eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)
		eng.SetParallelism(parallelism)

		if err := eng.Apply(context.Background(), resources); err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		// Property: maxConcurrent must never exceed parallelism
		if int(maxConcurrent.Load()) > parallelism {
			t.Fatalf("maxConcurrent %d exceeded parallelism %d", maxConcurrent.Load(), parallelism)
		}
	})
}

func TestEngine_Property_ContinueOnError(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		n := rapid.IntRange(2, 10).Draw(t, "numTools")
		failIdx := rapid.IntRange(0, n-1).Draw(t, "failIdx")

		toolNames := make([]string, n)
		for i := range n {
			toolNames[i] = fmt.Sprintf("tool%d", i)
		}
		failName := toolNames[failIdx]

		cue := generateToolsCUE(toolNames)
		configDir, err := os.MkdirTemp("", "engine-prop-config-*")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(configDir) })
		if err := os.WriteFile(filepath.Join(configDir, "resources.cue"), []byte(cue), 0644); err != nil {
			t.Fatal(err)
		}
		loader := config.NewLoader(nil)
		resources, err := loader.Load(configDir)
		if err != nil {
			t.Fatal(err)
		}

		store := newStoreForRapid(t)

		var mu sync.Mutex
		installed := make(map[string]bool)

		toolMock := &mockToolInstaller{
			installFunc: func(_ context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
				if name == failName {
					return nil, fmt.Errorf("simulated failure for %s", name)
				}

				time.Sleep(50 * time.Millisecond)

				mu.Lock()
				installed[name] = true
				mu.Unlock()

				return &resource.ToolState{
					InstallerRef: res.ToolSpec.InstallerRef,
					Version:      res.ToolSpec.Version,
					InstallPath:  "/tools/" + name,
					BinPath:      "/bin/" + name,
				}, nil
			},
		}

		eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

		err = eng.Apply(context.Background(), resources)

		// Property: Apply must return an error
		if err == nil {
			t.Fatal("expected error from Apply")
		}

		// Property: the failed tool must NOT be in installed map
		if installed[failName] {
			t.Fatalf("failed tool %s should not be installed", failName)
		}

		// Property: all non-failing tools must be installed
		for _, name := range toolNames {
			if name == failName {
				continue
			}
			if !installed[name] {
				t.Fatalf("tool %s should have been installed despite %s failing", name, failName)
			}
		}

		// Property: state must contain all non-failing tools but not the failed one
		if lockErr := store.Lock(); lockErr != nil {
			t.Fatal(lockErr)
		}
		st, loadErr := store.Load()
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		_ = store.Unlock()

		if _, ok := st.Tools[failName]; ok {
			t.Fatalf("failed tool %s should not be in state", failName)
		}
		for _, name := range toolNames {
			if name == failName {
				continue
			}
			if _, ok := st.Tools[name]; !ok {
				t.Fatalf("tool %s should be in state despite %s failing", name, failName)
			}
		}
	})
}

func TestEngine_Apply_ToolSet(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	installedTools := make(map[string]*resource.ToolState)
	var mu sync.Mutex
	toolMock := &mockToolInstaller{
		installFunc: func(_ context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			st := &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}
			mu.Lock()
			installedTools[name] = st
			mu.Unlock()
			return st, nil
		},
	}
	runtimeMock := &mockRuntimeInstaller{}

	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	resources := []resource.Resource{
		&resource.Installer{
			BaseResource:  resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindInstaller, Metadata: resource.Metadata{Name: "aqua"}},
			InstallerSpec: &resource.InstallerSpec{Type: resource.InstallTypeDownload},
		},
		&resource.ToolSet{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindToolSet, Metadata: resource.Metadata{Name: "cli-tools"}},
			ToolSetSpec: &resource.ToolSetSpec{
				InstallerRef: "aqua",
				Tools: map[string]resource.ToolItem{
					"fd":  {Version: "9.0.0"},
					"bat": {Version: "0.24.0"},
				},
			},
		},
	}

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Both tools should be installed
	mu.Lock()
	assert.Contains(t, installedTools, "fd")
	assert.Contains(t, installedTools, "bat")
	assert.Equal(t, "9.0.0", installedTools["fd"].Version)
	assert.Equal(t, "0.24.0", installedTools["bat"].Version)
	mu.Unlock()

	// Verify state
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)
	assert.NotNil(t, st.Tools["fd"])
	assert.NotNil(t, st.Tools["bat"])
}

func TestEngine_Apply_ToolSet_DisabledItem(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	installedTools := make(map[string]bool)
	var mu sync.Mutex
	toolMock := &mockToolInstaller{
		installFunc: func(_ context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			mu.Lock()
			installedTools[name] = true
			mu.Unlock()
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}
	runtimeMock := &mockRuntimeInstaller{}

	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	disabled := false
	resources := []resource.Resource{
		&resource.Installer{
			BaseResource:  resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindInstaller, Metadata: resource.Metadata{Name: "aqua"}},
			InstallerSpec: &resource.InstallerSpec{Type: resource.InstallTypeDownload},
		},
		&resource.ToolSet{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindToolSet, Metadata: resource.Metadata{Name: "cli-tools"}},
			ToolSetSpec: &resource.ToolSetSpec{
				InstallerRef: "aqua",
				Tools: map[string]resource.ToolItem{
					"fd":  {Version: "9.0.0"},
					"bat": {Version: "0.24.0", Enabled: &disabled},
				},
			},
		},
	}

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	mu.Lock()
	assert.True(t, installedTools["fd"])
	assert.False(t, installedTools["bat"], "disabled tool should not be installed")
	mu.Unlock()
}

func TestEngine_Apply_ToolSet_NameConflict(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	toolMock := &mockToolInstaller{}
	runtimeMock := &mockRuntimeInstaller{}
	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	resources := []resource.Resource{
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "fd"}},
			ToolSpec:     &resource.ToolSpec{InstallerRef: "aqua", Version: "9.0.0"},
		},
		&resource.ToolSet{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindToolSet, Metadata: resource.Metadata{Name: "cli-tools"}},
			ToolSetSpec: &resource.ToolSetSpec{
				InstallerRef: "aqua",
				Tools: map[string]resource.ToolItem{
					"fd": {Version: "10.0.0"},
				},
			},
		},
	}

	err = eng.Apply(context.Background(), resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name conflict")
}

func TestCheckRemovalDependencies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		runtimeRemovals []string
		remainingTools  []*resource.Tool
		wantErr         bool
		errContains     string
	}{
		{
			name:            "no removals",
			runtimeRemovals: nil,
			remainingTools: []*resource.Tool{
				{BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "gopls"}}, ToolSpec: &resource.ToolSpec{RuntimeRef: "go"}},
			},
			wantErr: false,
		},
		{
			name:            "removal with no dependents",
			runtimeRemovals: []string{"go"},
			remainingTools: []*resource.Tool{
				{BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "fzf"}}, ToolSpec: &resource.ToolSpec{InstallerRef: "download"}},
			},
			wantErr: false,
		},
		{
			name:            "removal blocked by dependent tool",
			runtimeRemovals: []string{"go"},
			remainingTools: []*resource.Tool{
				{BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "gopls"}}, ToolSpec: &resource.ToolSpec{RuntimeRef: "go"}},
			},
			wantErr:     true,
			errContains: `tool "gopls" depends on runtime "go"`,
		},
		{
			name:            "removal blocked by multiple dependents",
			runtimeRemovals: []string{"go"},
			remainingTools: []*resource.Tool{
				{BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "gopls"}}, ToolSpec: &resource.ToolSpec{RuntimeRef: "go"}},
				{BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "staticcheck"}}, ToolSpec: &resource.ToolSpec{RuntimeRef: "go"}},
			},
			wantErr:     true,
			errContains: "cannot remove runtime",
		},
		{
			name:            "dependent tool not in remaining (both removed)",
			runtimeRemovals: []string{"go"},
			remainingTools:  nil,
			wantErr:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkRemovalDependencies(tt.runtimeRemovals, tt.remainingTools)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestEngine_Apply_RemoveRuntimeWithDependentTool(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	stateDir := t.TempDir()

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

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	toolMock := &mockToolInstaller{}
	runtimeMock := &mockRuntimeInstaller{}
	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	// First apply: install both
	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Second apply: remove runtime only, keep gopls
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
	resourcesV2, err := loader.Load(configDir)
	require.NoError(t, err)

	err = eng.Apply(context.Background(), resourcesV2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot remove runtime")
	assert.Contains(t, err.Error(), "gopls")

	// Verify runtime was NOT removed from state
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	_ = store.Unlock()
	assert.NotNil(t, st.Runtimes["go"], "runtime should still be in state")
}

func TestEngine_PlanAll_RemoveRuntimeWithDependentTool(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	stateDir := t.TempDir()

	// Pre-populate state as if runtime + tool were installed
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Runtimes["go"] = &resource.RuntimeState{
		Version:     "1.0.0",
		InstallPath: "/runtimes/go/1.0.0",
		ToolBinPath: "~/go/bin",
	}
	initialState.Tools["gopls"] = &resource.ToolState{
		Version:    "v0.21.0",
		RuntimeRef: "go",
		BinPath:    "~/go/bin/gopls",
	}
	require.NoError(t, store.Save(initialState))
	require.NoError(t, store.Unlock())

	// Config with only gopls (runtime removed)
	cueContent := `package tomei

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

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	toolMock := &mockToolInstaller{}
	runtimeMock := &mockRuntimeInstaller{}
	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	_, _, _, err = eng.PlanAll(context.Background(), resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot remove runtime")
	assert.Contains(t, err.Error(), "gopls")
}

func TestEngine_Apply_RemoveRuntimeAndDependentTool(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	stateDir := t.TempDir()

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

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	removedTools := make(map[string]bool)
	removedRuntimes := make(map[string]bool)

	toolMock := &mockToolInstaller{
		removeFunc: func(ctx context.Context, st *resource.ToolState, name string) error {
			removedTools[name] = true
			return nil
		},
	}
	runtimeMock := &mockRuntimeInstaller{
		removeFunc: func(ctx context.Context, st *resource.RuntimeState, name string) error {
			removedRuntimes[name] = true
			return nil
		},
	}
	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	// First apply: install both
	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Second apply: remove both (empty config with just a placeholder)
	cueContentV2 := `package tomei

placeholder: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fzf"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/fzf.tar.gz"
			checksum: value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		}
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContentV2), 0644))
	resourcesV2, err := loader.Load(configDir)
	require.NoError(t, err)

	err = eng.Apply(context.Background(), resourcesV2)
	require.NoError(t, err)

	assert.True(t, removedTools["gopls"], "gopls should be removed")
	assert.True(t, removedRuntimes["go"], "go runtime should be removed")
}

func TestApplyUpdateTaints_SyncMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		tools          map[string]*resource.ToolState
		wantTainted    []string
		wantNotTainted []string
	}{
		{
			name: "latest tool is tainted",
			tools: map[string]*resource.ToolState{
				"fd": {
					InstallerRef: "aqua",
					Version:      "9.0.0",
					VersionKind:  resource.VersionLatest,
					InstallPath:  "/tools/fd",
					BinPath:      "/bin/fd",
				},
			},
			wantTainted: []string{"fd"},
		},
		{
			name: "exact version tool is not tainted",
			tools: map[string]*resource.ToolState{
				"rg": {
					InstallerRef: "aqua",
					Version:      "14.0.0",
					VersionKind:  resource.VersionExact,
					InstallPath:  "/tools/rg",
					BinPath:      "/bin/rg",
				},
			},
			wantNotTainted: []string{"rg"},
		},
		{
			name: "alias version tool is not tainted by sync",
			tools: map[string]*resource.ToolState{
				"rustc": {
					Version:     "1.83.0",
					VersionKind: resource.VersionAlias,
					SpecVersion: "stable",
				},
			},
			wantNotTainted: []string{"rustc"},
		},
		{
			name: "mixed: only latest tools are tainted",
			tools: map[string]*resource.ToolState{
				"fd": {
					InstallerRef: "aqua",
					Version:      "9.0.0",
					VersionKind:  resource.VersionLatest,
					InstallPath:  "/tools/fd",
					BinPath:      "/bin/fd",
				},
				"rg": {
					InstallerRef: "aqua",
					Version:      "14.0.0",
					VersionKind:  resource.VersionExact,
					InstallPath:  "/tools/rg",
					BinPath:      "/bin/rg",
				},
				"bat": {
					InstallerRef: "aqua",
					Version:      "0.24.0",
					VersionKind:  resource.VersionLatest,
					InstallPath:  "/tools/bat",
					BinPath:      "/bin/bat",
				},
			},
			wantTainted:    []string{"fd", "bat"},
			wantNotTainted: []string{"rg"},
		},
		{
			name:  "no tools",
			tools: map[string]*resource.ToolState{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := state.NewUserState()
			st.Tools = tt.tools

			ApplyUpdateTaints(st, UpdateConfig{SyncMode: true})

			for _, name := range tt.wantTainted {
				assert.True(t, st.Tools[name].IsTainted(), "tool %s should be tainted", name)
				assert.Equal(t, resource.TaintReasonSyncUpdate, st.Tools[name].TaintReason)
			}
			for _, name := range tt.wantNotTainted {
				assert.False(t, st.Tools[name].IsTainted(), "tool %s should not be tainted", name)
			}
		})
	}
}

func TestApplyUpdateTaints_UpdateTools(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		tools          map[string]*resource.ToolState
		wantTainted    []string
		wantNotTainted []string
	}{
		{
			name: "latest tool is tainted",
			tools: map[string]*resource.ToolState{
				"fd": {
					InstallerRef: "aqua",
					Version:      "9.0.0",
					VersionKind:  resource.VersionLatest,
					InstallPath:  "/tools/fd",
					BinPath:      "/bin/fd",
				},
			},
			wantTainted: []string{"fd"},
		},
		{
			name: "alias tool is tainted",
			tools: map[string]*resource.ToolState{
				"rustfmt": {
					Version:     "1.83.0",
					VersionKind: resource.VersionAlias,
					SpecVersion: "stable",
					RuntimeRef:  "rust",
				},
			},
			wantTainted: []string{"rustfmt"},
		},
		{
			name: "exact version tool is not tainted",
			tools: map[string]*resource.ToolState{
				"rg": {
					InstallerRef: "aqua",
					Version:      "14.0.0",
					VersionKind:  resource.VersionExact,
					InstallPath:  "/tools/rg",
					BinPath:      "/bin/rg",
				},
			},
			wantNotTainted: []string{"rg"},
		},
		{
			name: "mixed: latest and alias tainted, exact not",
			tools: map[string]*resource.ToolState{
				"fd": {
					InstallerRef: "aqua",
					Version:      "9.0.0",
					VersionKind:  resource.VersionLatest,
					InstallPath:  "/tools/fd",
					BinPath:      "/bin/fd",
				},
				"rg": {
					InstallerRef: "aqua",
					Version:      "14.0.0",
					VersionKind:  resource.VersionExact,
					InstallPath:  "/tools/rg",
					BinPath:      "/bin/rg",
				},
				"rustfmt": {
					Version:     "1.83.0",
					VersionKind: resource.VersionAlias,
					SpecVersion: "stable",
					RuntimeRef:  "rust",
				},
			},
			wantTainted:    []string{"fd", "rustfmt"},
			wantNotTainted: []string{"rg"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := state.NewUserState()
			st.Tools = tt.tools

			ApplyUpdateTaints(st, UpdateConfig{UpdateTools: true})

			for _, name := range tt.wantTainted {
				assert.True(t, st.Tools[name].IsTainted(), "tool %s should be tainted", name)
				assert.Equal(t, resource.TaintReasonUpdateRequested, st.Tools[name].TaintReason)
			}
			for _, name := range tt.wantNotTainted {
				assert.False(t, st.Tools[name].IsTainted(), "tool %s should not be tainted", name)
			}
		})
	}
}

func TestApplyUpdateTaints_UpdateRuntimes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name           string
		runtimes       map[string]*resource.RuntimeState
		wantTainted    []string
		wantNotTainted []string
	}{
		{
			name: "alias runtime is tainted",
			runtimes: map[string]*resource.RuntimeState{
				"rust": {
					Type:        resource.InstallTypeDelegation,
					Version:     "1.83.0",
					VersionKind: resource.VersionAlias,
					SpecVersion: "stable",
					ToolBinPath: "~/.cargo/bin",
				},
			},
			wantTainted: []string{"rust"},
		},
		{
			name: "latest runtime is tainted",
			runtimes: map[string]*resource.RuntimeState{
				"node": {
					Type:        resource.InstallTypeDownload,
					Version:     "22.0.0",
					VersionKind: resource.VersionLatest,
					ToolBinPath: "~/.local/bin",
				},
			},
			wantTainted: []string{"node"},
		},
		{
			name: "exact version runtime is not tainted",
			runtimes: map[string]*resource.RuntimeState{
				"go": {
					Type:        resource.InstallTypeDownload,
					Version:     "1.25.6",
					VersionKind: resource.VersionExact,
					SpecVersion: "1.25.6",
					ToolBinPath: "~/go/bin",
				},
			},
			wantNotTainted: []string{"go"},
		},
		{
			name: "mixed: alias and latest tainted, exact not",
			runtimes: map[string]*resource.RuntimeState{
				"rust": {
					Type:        resource.InstallTypeDelegation,
					Version:     "1.83.0",
					VersionKind: resource.VersionAlias,
					SpecVersion: "stable",
					ToolBinPath: "~/.cargo/bin",
				},
				"go": {
					Type:        resource.InstallTypeDownload,
					Version:     "1.25.6",
					VersionKind: resource.VersionExact,
					SpecVersion: "1.25.6",
					ToolBinPath: "~/go/bin",
				},
			},
			wantTainted:    []string{"rust"},
			wantNotTainted: []string{"go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			st := state.NewUserState()
			st.Runtimes = tt.runtimes

			ApplyUpdateTaints(st, UpdateConfig{UpdateRuntimes: true})

			for _, name := range tt.wantTainted {
				assert.True(t, st.Runtimes[name].IsTainted(), "runtime %s should be tainted", name)
				assert.Equal(t, resource.TaintReasonUpdateRequested, st.Runtimes[name].TaintReason)
			}
			for _, name := range tt.wantNotTainted {
				assert.False(t, st.Runtimes[name].IsTainted(), "runtime %s should not be tainted", name)
			}
		})
	}
}

func TestEngine_SyncMode_Apply(t *testing.T) {
	t.Parallel()
	// End-to-end: sync mode triggers reinstall of latest-specified tool
	configDir := t.TempDir()
	stateDir := t.TempDir()

	cueContent := `package tomei

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "download"
		version: "9.0.0"
		source: {
			url: "https://example.com/fd.tar.gz"
			checksum: { value: "sha256:2222222222222222222222222222222222222222222222222222222222222222" }
		}
	}
}
`
	cueFile := filepath.Join(configDir, "tools.cue")
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with latest-specified tool (already installed)
	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Tools["fd"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "9.0.0",
		VersionKind:  resource.VersionLatest,
		InstallPath:  "/tools/fd",
		BinPath:      "/bin/fd",
	}
	require.NoError(t, store.Save(initialState))
	require.NoError(t, store.Unlock())

	installCalled := false
	toolMock := &mockToolInstaller{
		installFunc: func(_ context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			installCalled = true
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)
	eng.SetUpdateConfig(UpdateConfig{SyncMode: true})

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Tool should be reinstalled because it was tainted by sync mode
	assert.True(t, installCalled, "latest tool should be reinstalled in sync mode")
}

func TestEngine_SyncMode_ExactVersionNotReinstalled(t *testing.T) {
	t.Parallel()
	// Sync mode should NOT reinstall tools with exact version
	configDir := t.TempDir()
	stateDir := t.TempDir()

	cueContent := `package tomei

rg: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "download"
		version: "14.0.0"
		source: {
			url: "https://example.com/rg.tar.gz"
			checksum: { value: "sha256:1111111111111111111111111111111111111111111111111111111111111111" }
		}
	}
}
`
	cueFile := filepath.Join(configDir, "tools.cue")
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with exact version tool
	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Tools["rg"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "14.0.0",
		VersionKind:  resource.VersionExact,
		InstallPath:  "/tools/rg",
		BinPath:      "/bin/rg",
	}
	require.NoError(t, store.Save(initialState))
	require.NoError(t, store.Unlock())

	installCalled := false
	toolMock := &mockToolInstaller{
		installFunc: func(_ context.Context, _ *resource.Tool, _ string) (*resource.ToolState, error) {
			installCalled = true
			return nil, nil
		},
	}

	eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)
	eng.SetUpdateConfig(UpdateConfig{SyncMode: true})

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	assert.False(t, installCalled, "exact version tool should not be reinstalled in sync mode")
}

func TestEngine_Apply_InstallerRepository(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

repo: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "InstallerRepository"
	metadata: name: "bitnami"
	spec: {
		installerRef: "helm"
		source: {
			type: "delegation"
			url:  "https://charts.bitnami.com/bitnami"
			commands: {
				install: ["helm repo add bitnami https://charts.bitnami.com/bitnami"]
				check:   ["helm repo list | grep bitnami"]
				remove:  ["helm repo remove bitnami"]
			}
		}
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	installedRepos := make(map[string]*resource.InstallerRepositoryState)
	repoMock := &mockInstallerRepositoryInstaller{
		installFunc: func(_ context.Context, res *resource.InstallerRepository, name string) (*resource.InstallerRepositoryState, error) {
			st := &resource.InstallerRepositoryState{
				InstallerRef: res.InstallerRepositorySpec.InstallerRef,
				SourceType:   res.InstallerRepositorySpec.Source.Type,
				URL:          res.InstallerRepositorySpec.Source.URL,
			}
			installedRepos[name] = st
			return st, nil
		},
	}

	eng := NewEngine(&mockToolInstaller{}, &mockRuntimeInstaller{}, repoMock, store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	require.Contains(t, installedRepos, "bitnami")
	assert.Equal(t, "helm", installedRepos["bitnami"].InstallerRef)
	assert.Equal(t, resource.InstallerRepositorySourceDelegation, installedRepos["bitnami"].SourceType)
	assert.Equal(t, "https://charts.bitnami.com/bitnami", installedRepos["bitnami"].URL)
}

func TestEngine_Apply_InstallerRepositoryWithTool(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

repo: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "InstallerRepository"
	metadata: name: "bitnami"
	spec: {
		installerRef: "helm"
		source: {
			type: "delegation"
			url:  "https://charts.bitnami.com/bitnami"
			commands: {
				install: ["helm repo add bitnami https://charts.bitnami.com/bitnami"]
				check:   ["helm repo list | grep bitnami"]
				remove:  ["helm repo remove bitnami"]
			}
		}
	}
}

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "nginx"
	spec: {
		installerRef: "download"
		repositoryRef: "bitnami"
		version: "1.0.0"
		source: {
			url: "https://example.com/nginx.tar.gz"
			checksum: value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
			archiveType: "tar.gz"
		}
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Track execution order
	var execOrder []string
	var mu sync.Mutex

	repoMock := &mockInstallerRepositoryInstaller{
		installFunc: func(_ context.Context, res *resource.InstallerRepository, name string) (*resource.InstallerRepositoryState, error) {
			mu.Lock()
			execOrder = append(execOrder, "repo:"+name)
			mu.Unlock()
			return &resource.InstallerRepositoryState{
				InstallerRef: res.InstallerRepositorySpec.InstallerRef,
				SourceType:   res.InstallerRepositorySpec.Source.Type,
				URL:          res.InstallerRepositorySpec.Source.URL,
			}, nil
		},
	}

	toolMock := &mockToolInstaller{
		installFunc: func(_ context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			mu.Lock()
			execOrder = append(execOrder, "tool:"+name)
			mu.Unlock()
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				Version:      res.ToolSpec.Version,
			}, nil
		},
	}

	eng := NewEngine(toolMock, &mockRuntimeInstaller{}, repoMock, store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// InstallerRepository must be installed before Tool
	require.Len(t, execOrder, 2)
	assert.Equal(t, "repo:bitnami", execOrder[0])
	assert.Equal(t, "tool:nginx", execOrder[1])
}

func TestEngine_Apply_InstallerRepository_Remove(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with an installer repository
	require.NoError(t, store.Lock())
	st := state.NewUserState()
	st.InstallerRepositories["bitnami"] = &resource.InstallerRepositoryState{
		InstallerRef: "helm",
		SourceType:   resource.InstallerRepositorySourceDelegation,
		URL:          "https://charts.bitnami.com/bitnami",
	}
	require.NoError(t, store.Save(st))

	removedRepos := make(map[string]bool)
	repoMock := &mockInstallerRepositoryInstaller{
		removeFunc: func(_ context.Context, _ *resource.InstallerRepositoryState, name string) error {
			removedRepos[name] = true
			return nil
		},
	}

	eng := NewEngine(&mockToolInstaller{}, &mockRuntimeInstaller{}, repoMock, store)
	eng.SetUpdateConfig(UpdateConfig{SyncMode: true})

	// Apply with empty resources - should trigger removal
	err = eng.Apply(context.Background(), []resource.Resource{})
	require.NoError(t, err)

	assert.True(t, removedRepos["bitnami"])
}

func TestEngine_PlanAll_InstallerRepository(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

repo: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "InstallerRepository"
	metadata: name: "bitnami"
	spec: {
		installerRef: "helm"
		source: {
			type: "delegation"
			url:  "https://charts.bitnami.com/bitnami"
			commands: {
				install: ["helm repo add bitnami https://charts.bitnami.com/bitnami"]
				check:   ["helm repo list | grep bitnami"]
				remove:  ["helm repo remove bitnami"]
			}
		}
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	eng := NewEngine(&mockToolInstaller{}, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

	runtimeActions, repoActions, toolActions, err := eng.PlanAll(context.Background(), resources)
	require.NoError(t, err)

	assert.Empty(t, runtimeActions)
	assert.Empty(t, toolActions)
	require.Len(t, repoActions, 1)
	assert.Equal(t, "bitnami", repoActions[0].Name)
	assert.Equal(t, resource.ActionInstall, repoActions[0].Type)
}

func TestAppendBuiltinInstallers(t *testing.T) {
	t.Parallel()
	t.Run("adds download and aqua when absent", func(t *testing.T) {
		t.Parallel()
		resources := []resource.Resource{
			&resource.Tool{
				BaseResource: resource.BaseResource{
					APIVersion:   resource.GroupVersion,
					ResourceKind: resource.KindTool,
					Metadata:     resource.Metadata{Name: "jq"},
				},
				ToolSpec: &resource.ToolSpec{InstallerRef: "aqua", Version: "1.7.1"},
			},
		}

		result := AppendBuiltinInstallers(resources)

		// Original tool + download + aqua
		require.Len(t, result, 3)

		names := make(map[string]bool)
		for _, res := range result {
			if res.Kind() == resource.KindInstaller {
				names[res.Name()] = true
			}
		}
		assert.True(t, names["download"])
		assert.True(t, names["aqua"])
	})

	t.Run("does not duplicate existing installer", func(t *testing.T) {
		t.Parallel()
		existing := &resource.Installer{
			BaseResource: resource.BaseResource{
				APIVersion:   resource.GroupVersion,
				ResourceKind: resource.KindInstaller,
				Metadata:     resource.Metadata{Name: "aqua"},
			},
			InstallerSpec: &resource.InstallerSpec{Type: resource.InstallTypeDownload},
		}
		resources := []resource.Resource{existing}

		result := AppendBuiltinInstallers(resources)

		// existing aqua + added download
		require.Len(t, result, 2)

		var count int
		for _, res := range result {
			if res.Kind() == resource.KindInstaller && res.Name() == "aqua" {
				count++
			}
		}
		assert.Equal(t, 1, count, "aqua should not be duplicated")
	})
}

func TestEngine_Apply_EventLayerStart(t *testing.T) {
	t.Parallel()
	// Setup CUE config with runtime and tool (2 layers: runtime first, then tool)
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

runtime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.0"
		source: {
			url: "https://example.com/go-1.25.0.tar.gz"
			checksum: value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		}
		binaries: ["go"]
		toolBinPath: "~/go/bin"
	}
}

tool: {
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
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Collect events
	var mu sync.Mutex
	var events []Event
	collectEvents := func(event Event) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}

	eng := NewEngine(&mockToolInstaller{}, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)
	eng.SetEventHandler(collectEvents)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Filter EventLayerStart events
	var layerEvents []Event
	for _, e := range events {
		if e.Type == EventLayerStart {
			layerEvents = append(layerEvents, e)
		}
	}

	// Should have at least 2 layers (runtime layer, then tool layer)
	require.GreaterOrEqual(t, len(layerEvents), 2, "expected at least 2 EventLayerStart events")

	// Verify first layer start event
	first := layerEvents[0]
	assert.Equal(t, 0, first.Layer)
	assert.Equal(t, len(layerEvents), first.TotalLayers)

	// Verify AllLayerNodes is populated and matches TotalLayers
	assert.Len(t, first.AllLayerNodes, first.TotalLayers)

	// Verify Installer/InstallerRepository nodes are excluded from LayerNodes
	for _, le := range layerEvents {
		for _, nodeName := range le.LayerNodes {
			assert.False(t, strings.HasPrefix(nodeName, "Installer/"),
				"Installer nodes should be excluded from LayerNodes, got: %s", nodeName)
			assert.False(t, strings.HasPrefix(nodeName, "InstallerRepository/"),
				"InstallerRepository nodes should be excluded from LayerNodes, got: %s", nodeName)
		}
	}

	// Verify AllLayerNodes also excludes Installer/InstallerRepository
	for _, layerNodes := range first.AllLayerNodes {
		for _, nodeName := range layerNodes {
			assert.False(t, strings.HasPrefix(nodeName, "Installer/"),
				"Installer nodes should be excluded from AllLayerNodes, got: %s", nodeName)
			assert.False(t, strings.HasPrefix(nodeName, "InstallerRepository/"),
				"InstallerRepository nodes should be excluded from AllLayerNodes, got: %s", nodeName)
		}
	}

	// Verify that Runtime/go appears in some layer and Tool/gopls in a later layer
	foundRuntime := false
	foundTool := false
	runtimeLayer := -1
	toolLayer := -1
	for _, le := range layerEvents {
		for _, nodeName := range le.LayerNodes {
			if nodeName == "Runtime/go" {
				foundRuntime = true
				runtimeLayer = le.Layer
			}
			if nodeName == "Tool/gopls" {
				foundTool = true
				toolLayer = le.Layer
			}
		}
	}
	assert.True(t, foundRuntime, "Runtime/go should appear in LayerNodes")
	assert.True(t, foundTool, "Tool/gopls should appear in LayerNodes")
	assert.Less(t, runtimeLayer, toolLayer, "Runtime should be in an earlier layer than Tool")
}

func TestEngine_Apply_EventComplete_InstallPath(t *testing.T) {
	t.Parallel()
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package tomei

runtime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.0"
		source: {
			url: "https://example.com/go-1.25.0.tar.gz"
			checksum: value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		}
		binaries: ["go"]
		toolBinPath: "~/go/bin"
	}
}

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "download"
		version: "0.25.0"
		source: {
			url: "https://example.com/bat-0.25.0.tar.gz"
			checksum: value: "sha256:a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"
			archiveType: "tar.gz"
		}
	}
}
`
	require.NoError(t, os.WriteFile(cueFile, []byte(cueContent), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Collect events
	var mu sync.Mutex
	var events []Event
	collectEvents := func(event Event) {
		mu.Lock()
		events = append(events, event)
		mu.Unlock()
	}

	eng := NewEngine(&mockToolInstaller{}, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)
	eng.SetEventHandler(collectEvents)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Find EventComplete events and verify InstallPath
	for _, e := range events {
		if e.Type != EventComplete {
			continue
		}
		switch e.Kind {
		case resource.KindRuntime:
			assert.NotEmpty(t, e.InstallPath,
				"EventComplete for runtime %s should have InstallPath", e.Name)
			assert.Contains(t, e.InstallPath, e.Name,
				"InstallPath should contain the runtime name")
		case resource.KindTool:
			assert.NotEmpty(t, e.InstallPath,
				"EventComplete for tool %s should have InstallPath", e.Name)
			assert.Contains(t, e.InstallPath, e.Name,
				"InstallPath should contain the tool name")
		}
	}
}

// --- Delegation Serialization Tests ---

func TestEngine_DetermineInstallMethod_Commands(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	eng := NewEngine(&mockToolInstaller{}, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

	tests := []struct {
		name string
		tool *resource.Tool
		want string
	}{
		{
			name: "commands pattern",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "claude"}},
				ToolSpec: &resource.ToolSpec{
					Commands: &resource.ToolCommandSet{
						CommandSet: resource.CommandSet{
							Install: []string{"curl -fsSL https://cli.claude.ai/install.sh | sh"},
						},
					},
				},
			},
			want: "commands",
		},
		{
			name: "runtime delegation",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "gopls"}},
				ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Package: &resource.Package{Name: "golang.org/x/tools/gopls"}},
			},
			want: "go install",
		},
		{
			name: "installer delegation",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "rg"}},
				ToolSpec:     &resource.ToolSpec{InstallerRef: "aqua", Version: "14.0.0"},
			},
			want: "aqua install",
		},
		{
			name: "download pattern",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "gh"}},
				ToolSpec:     &resource.ToolSpec{InstallerRef: "download", Version: "2.0.0"},
			},
			want: "download",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := eng.determineInstallMethod(tt.tool)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDelegationKeyForTool(t *testing.T) {
	t.Parallel()

	// Helper to build a resourceMap with an Installer
	makeResourceMap := func(installers ...*resource.Installer) map[string]resource.Resource {
		m := make(map[string]resource.Resource)
		for _, inst := range installers {
			id := fmt.Sprintf("Installer/%s", inst.Name())
			m[id] = inst
		}
		return m
	}

	tests := []struct {
		name        string
		tool        *resource.Tool
		resourceMap map[string]resource.Resource
		want        string
	}{
		{
			name: "runtime go",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "gopls"}},
				ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Package: &resource.Package{Name: "golang.org/x/tools/gopls"}},
			},
			resourceMap: nil,
			want:        "runtime:go",
		},
		{
			name: "runtime pnpm",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "prettier"}},
				ToolSpec:     &resource.ToolSpec{RuntimeRef: "pnpm", Package: &resource.Package{Name: "prettier"}},
			},
			resourceMap: nil,
			want:        "runtime:pnpm",
		},
		{
			name: "installer download type",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "rg"}},
				ToolSpec:     &resource.ToolSpec{InstallerRef: "download", Version: "14.0.0"},
			},
			resourceMap: makeResourceMap(&resource.Installer{
				BaseResource:  resource.BaseResource{Metadata: resource.Metadata{Name: "download"}},
				InstallerSpec: &resource.InstallerSpec{Type: resource.InstallTypeDownload},
			}),
			want: "",
		},
		{
			name: "installer aqua (download type)",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "fd"}},
				ToolSpec:     &resource.ToolSpec{InstallerRef: "aqua", Version: "9.0.0"},
			},
			resourceMap: makeResourceMap(&resource.Installer{
				BaseResource:  resource.BaseResource{Metadata: resource.Metadata{Name: "aqua"}},
				InstallerSpec: &resource.InstallerSpec{Type: resource.InstallTypeDownload},
			}),
			want: "",
		},
		{
			name: "installer binstall (delegation type)",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "rg"}},
				ToolSpec:     &resource.ToolSpec{InstallerRef: "binstall", Package: &resource.Package{Name: "ripgrep"}},
			},
			resourceMap: makeResourceMap(&resource.Installer{
				BaseResource:  resource.BaseResource{Metadata: resource.Metadata{Name: "binstall"}},
				InstallerSpec: &resource.InstallerSpec{Type: resource.InstallTypeDelegation},
			}),
			want: "installer:binstall",
		},
		{
			name: "runtime takes precedence over installer",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "tool"}},
				ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", InstallerRef: "x", Package: &resource.Package{Name: "example.com/tool"}},
			},
			resourceMap: makeResourceMap(&resource.Installer{
				BaseResource:  resource.BaseResource{Metadata: resource.Metadata{Name: "x"}},
				InstallerSpec: &resource.InstallerSpec{Type: resource.InstallTypeDelegation},
			}),
			want: "runtime:go",
		},
		{
			name: "no refs (empty)",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "tool"}},
				ToolSpec:     &resource.ToolSpec{Version: "1.0.0"},
			},
			resourceMap: nil,
			want:        "",
		},
		{
			name: "installer not in resource map",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "tool"}},
				ToolSpec:     &resource.ToolSpec{InstallerRef: "unknown", Package: &resource.Package{Name: "pkg"}},
			},
			resourceMap: nil,
			want:        "",
		},
		{
			name: "commands pattern returns empty key",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "claude"}},
				ToolSpec: &resource.ToolSpec{
					Commands: &resource.ToolCommandSet{
						CommandSet: resource.CommandSet{
							Install: []string{"curl -fsSL https://cli.claude.ai/install.sh | sh"},
						},
					},
				},
			},
			resourceMap: nil,
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := delegationKeyForTool(tt.tool, tt.resourceMap)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestPartitionToolsByDelegation(t *testing.T) {
	t.Parallel()

	makeNode := func(name string) *graph.Node {
		return &graph.Node{ID: graph.NewNodeID(resource.KindTool, name), Kind: resource.KindTool, Name: name}
	}
	makeTool := func(name, runtimeRef, installerRef string) *resource.Tool {
		return &resource.Tool{
			BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: name}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: runtimeRef, InstallerRef: installerRef, Version: "1.0.0"},
		}
	}
	makeResourceMap := func(tools ...*resource.Tool) map[string]resource.Resource {
		m := make(map[string]resource.Resource)
		for _, t := range tools {
			id := graph.NewNodeID(resource.KindTool, t.Name()).String()
			m[id] = t
		}
		return m
	}

	tests := []struct {
		name               string
		nodes              []*graph.Node
		resourceMap        map[string]resource.Resource
		wantDownloadCount  int
		wantGroupCount     int
		wantGroupToolNames [][]string // each group's tool names (sorted by delegation key)
	}{
		{
			name:  "all download tools",
			nodes: []*graph.Node{makeNode("rg"), makeNode("fd"), makeNode("bat")},
			resourceMap: makeResourceMap(
				makeTool("rg", "", "aqua"),
				makeTool("fd", "", "aqua"),
				makeTool("bat", "", "aqua"),
			),
			wantDownloadCount: 3,
			wantGroupCount:    0,
		},
		{
			name:  "same runtime 3 tools",
			nodes: []*graph.Node{makeNode("gopls"), makeNode("goimports"), makeNode("staticcheck")},
			resourceMap: makeResourceMap(
				makeTool("gopls", "go", ""),
				makeTool("goimports", "go", ""),
				makeTool("staticcheck", "go", ""),
			),
			wantDownloadCount:  0,
			wantGroupCount:     1,
			wantGroupToolNames: [][]string{{"gopls", "goimports", "staticcheck"}},
		},
		{
			name:  "different runtimes",
			nodes: []*graph.Node{makeNode("gopls"), makeNode("prettier")},
			resourceMap: makeResourceMap(
				makeTool("gopls", "go", ""),
				makeTool("prettier", "pnpm", ""),
			),
			wantDownloadCount: 0,
			wantGroupCount:    2,
			// Sorted by delegation key: "runtime:go" < "runtime:pnpm"
			wantGroupToolNames: [][]string{{"gopls"}, {"prettier"}},
		},
		{
			name:  "mixed download and delegation",
			nodes: []*graph.Node{makeNode("rg"), makeNode("gopls"), makeNode("fd"), makeNode("goimports")},
			resourceMap: makeResourceMap(
				makeTool("rg", "", "aqua"),
				makeTool("gopls", "go", ""),
				makeTool("fd", "", "aqua"),
				makeTool("goimports", "go", ""),
			),
			wantDownloadCount:  2,
			wantGroupCount:     1,
			wantGroupToolNames: [][]string{{"gopls", "goimports"}},
		},
		{
			name: "commands-pattern tool goes to download nodes",
			nodes: []*graph.Node{
				makeNode("claude"),
				makeNode("gopls"),
			},
			resourceMap: func() map[string]resource.Resource {
				commandsTool := &resource.Tool{
					BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "claude"}},
					ToolSpec: &resource.ToolSpec{
						Version: "1.0.0",
						Commands: &resource.ToolCommandSet{
							CommandSet: resource.CommandSet{Install: []string{"curl -fsSL https://example.com/install.sh | sh"}},
						},
					},
				}
				delegationTool := makeTool("gopls", "go", "")
				return makeResourceMap(commandsTool, delegationTool)
			}(),
			wantDownloadCount:  1,
			wantGroupCount:     1,
			wantGroupToolNames: [][]string{{"gopls"}},
		},
		{
			name:              "empty input",
			nodes:             nil,
			resourceMap:       nil,
			wantDownloadCount: 0,
			wantGroupCount:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			downloadNodes, delegationGroups := partitionToolsByDelegation(tt.nodes, tt.resourceMap)
			assert.Len(t, downloadNodes, tt.wantDownloadCount)
			assert.Len(t, delegationGroups, tt.wantGroupCount)

			// Assert group membership when specified
			if tt.wantGroupToolNames != nil {
				for i, wantNames := range tt.wantGroupToolNames {
					var gotNames []string
					for _, n := range delegationGroups[i] {
						gotNames = append(gotNames, n.Name)
					}
					assert.Equal(t, wantNames, gotNames, "group %d tool names", i)
				}
			}
		})
	}
}

func TestEngine_Apply_DelegationToolsSerialized(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Track per-runtime concurrency
	var goCurrentCount atomic.Int32
	var goMaxConcurrent atomic.Int32
	var mu sync.Mutex
	installedTools := make(map[string]bool)

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			if res.ToolSpec.RuntimeRef == "go" {
				current := goCurrentCount.Add(1)
				defer goCurrentCount.Add(-1)

				// Assert serialization immediately: no more than 1 concurrent go delegation
				require.Equal(t, int32(1), current, "go delegation tools must run sequentially, but %s saw concurrency %d", name, current)

				// Track max for extra safety
				for {
					old := goMaxConcurrent.Load()
					if current <= old || goMaxConcurrent.CompareAndSwap(old, current) {
						break
					}
				}

				// Simulate some work
				time.Sleep(20 * time.Millisecond)
			}

			mu.Lock()
			installedTools[name] = true
			mu.Unlock()

			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				RuntimeRef:   res.ToolSpec.RuntimeRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	runtimeMock := &mockRuntimeInstaller{}
	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	// 3 go delegation tools + 2 download tools
	resources := []resource.Resource{
		&resource.Runtime{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindRuntime, Metadata: resource.Metadata{Name: "go"}},
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDownload,
				Version:     "1.23.0",
				Binaries:    []string{"go"},
				ToolBinPath: "~/go/bin",
				Source: &resource.DownloadSource{
					URL:      "https://example.com/go.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
				},
			},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "gopls"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.21.0", Package: &resource.Package{Name: "golang.org/x/tools/gopls"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "goimports"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.33.0", Package: &resource.Package{Name: "golang.org/x/tools/cmd/goimports"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "staticcheck"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.5.1", Package: &resource.Package{Name: "honnef.co/go/tools/cmd/staticcheck"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "rg"}},
			ToolSpec: &resource.ToolSpec{InstallerRef: "download", Version: "14.0.0", Source: &resource.DownloadSource{
				URL:      "https://example.com/rg.tar.gz",
				Checksum: &resource.Checksum{Value: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
			}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "fd"}},
			ToolSpec: &resource.ToolSpec{InstallerRef: "download", Version: "9.0.0", Source: &resource.DownloadSource{
				URL:      "https://example.com/fd.tar.gz",
				Checksum: &resource.Checksum{Value: "sha256:2222222222222222222222222222222222222222222222222222222222222222"},
			}},
		},
	}

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// All 5 tools should be installed
	assert.True(t, installedTools["gopls"])
	assert.True(t, installedTools["goimports"])
	assert.True(t, installedTools["staticcheck"])
	assert.True(t, installedTools["rg"])
	assert.True(t, installedTools["fd"])

	// Go delegation never exceeded concurrency 1
	assert.Equal(t, int32(1), goMaxConcurrent.Load(), "go delegation tools should run sequentially")
}

func TestEngine_Apply_DifferentDelegationGroupsParallel(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Track per-runtime concurrency and cross-group overlap.
	// Use a barrier pattern instead of time.Sleep to prove overlap deterministically:
	// each group's first tool blocks until all groups have entered their first tool,
	// guaranteeing concurrent execution is detected regardless of scheduler behavior.
	var goCurrentCount, rustCurrentCount atomic.Int32
	var mu sync.Mutex
	installedTools := make(map[string]bool)

	// Barrier: both groups signal arrival, then wait for each other.
	// Channels MUST be buffered so the non-blocking send always succeeds
	// regardless of goroutine scheduling order.
	goArrived := make(chan struct{}, 1)
	rustArrived := make(chan struct{}, 1)
	var overlapDetected atomic.Bool

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			switch res.ToolSpec.RuntimeRef {
			case "go":
				current := goCurrentCount.Add(1)
				defer goCurrentCount.Add(-1)
				require.Equal(t, int32(1), current, "go tools must be sequential")

				// First go tool: signal arrival and wait for rust group
				if current == 1 {
					select {
					case goArrived <- struct{}{}:
					default:
					}
					select {
					case <-rustArrived:
						overlapDetected.Store(true)
					case <-time.After(5 * time.Second):
						// Timeout fallback: don't block the test forever
					}
				}
			case "rust":
				current := rustCurrentCount.Add(1)
				defer rustCurrentCount.Add(-1)
				require.Equal(t, int32(1), current, "rust tools must be sequential")

				// First rust tool: signal arrival and wait for go group
				if current == 1 {
					select {
					case rustArrived <- struct{}{}:
					default:
					}
					select {
					case <-goArrived:
						overlapDetected.Store(true)
					case <-time.After(5 * time.Second):
						// Timeout fallback
					}
				}
			}

			mu.Lock()
			installedTools[name] = true
			mu.Unlock()

			return &resource.ToolState{
				RuntimeRef:  res.ToolSpec.RuntimeRef,
				Version:     res.ToolSpec.Version,
				InstallPath: "/tools/" + name,
				BinPath:     "/bin/" + name,
			}, nil
		},
	}

	runtimeMock := &mockRuntimeInstaller{}
	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	// 2 runtimes x 2 tools
	resources := []resource.Resource{
		&resource.Runtime{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindRuntime, Metadata: resource.Metadata{Name: "go"}},
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDownload,
				Version:     "1.23.0",
				Binaries:    []string{"go"},
				ToolBinPath: "~/go/bin",
				Source: &resource.DownloadSource{
					URL:      "https://example.com/go.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
				},
			},
		},
		&resource.Runtime{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindRuntime, Metadata: resource.Metadata{Name: "rust"}},
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDownload,
				Version:     "1.82.0",
				Binaries:    []string{"rustc", "cargo"},
				ToolBinPath: "~/.cargo/bin",
				Source: &resource.DownloadSource{
					URL:      "https://example.com/rust.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
				},
			},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "gopls"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.21.0", Package: &resource.Package{Name: "golang.org/x/tools/gopls"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "goimports"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.33.0", Package: &resource.Package{Name: "golang.org/x/tools/cmd/goimports"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "sd"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "rust", Version: "1.0.0", Package: &resource.Package{Name: "sd"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "bat"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "rust", Version: "0.24.0", Package: &resource.Package{Name: "bat"}},
		},
	}

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// All 4 tools installed
	assert.True(t, installedTools["gopls"])
	assert.True(t, installedTools["goimports"])
	assert.True(t, installedTools["sd"])
	assert.True(t, installedTools["bat"])

	// Different groups should have run in parallel (proven by barrier, not timing)
	assert.True(t, overlapDetected.Load(), "different delegation groups should run in parallel (barrier handshake)")
}

func TestEngine_Apply_DelegationGroupErrorStopsGroup(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var mu sync.Mutex
	installedTools := make(map[string]bool)

	// Track execution order of go delegation tools
	var executionOrder []string

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			if res.ToolSpec.RuntimeRef == "go" {
				mu.Lock()
				executionOrder = append(executionOrder, name)
				isSecond := len(executionOrder) >= 2 && name == executionOrder[1]
				mu.Unlock()

				// Second delegation tool in execution order fails
				if isSecond {
					return nil, fmt.Errorf("simulated failure for %s", name)
				}
			}

			mu.Lock()
			installedTools[name] = true
			mu.Unlock()

			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				RuntimeRef:   res.ToolSpec.RuntimeRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	runtimeMock := &mockRuntimeInstaller{}
	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	// 3 go delegation tools + 1 download tool
	resources := []resource.Resource{
		&resource.Runtime{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindRuntime, Metadata: resource.Metadata{Name: "go"}},
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDownload,
				Version:     "1.23.0",
				Binaries:    []string{"go"},
				ToolBinPath: "~/go/bin",
				Source: &resource.DownloadSource{
					URL:      "https://example.com/go.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
				},
			},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "gopls"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.21.0", Package: &resource.Package{Name: "golang.org/x/tools/gopls"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "goimports"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.33.0", Package: &resource.Package{Name: "golang.org/x/tools/cmd/goimports"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "staticcheck"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.5.1", Package: &resource.Package{Name: "honnef.co/go/tools/cmd/staticcheck"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "rg"}},
			ToolSpec: &resource.ToolSpec{InstallerRef: "download", Version: "14.0.0", Source: &resource.DownloadSource{
				URL:      "https://example.com/rg.tar.gz",
				Checksum: &resource.Checksum{Value: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
			}},
		},
	}

	err = eng.Apply(context.Background(), resources)
	require.Error(t, err, "should fail because one go delegation tool fails")

	// Download tool should still complete
	assert.True(t, installedTools["rg"], "download tool should complete despite delegation group error")

	// Not all 3 go tools should have been executed (group stops on error)
	goToolsExecuted := 0
	for _, name := range []string{"gopls", "goimports", "staticcheck"} {
		if installedTools[name] {
			goToolsExecuted++
		}
	}
	assert.Less(t, goToolsExecuted, 3, "not all go delegation tools should complete when one fails")
}

func TestEngine_Apply_AllDelegationSingleGroup(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var mu sync.Mutex
	installedTools := make(map[string]bool)

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			time.Sleep(10 * time.Millisecond)
			mu.Lock()
			installedTools[name] = true
			mu.Unlock()
			return &resource.ToolState{
				RuntimeRef:  res.ToolSpec.RuntimeRef,
				Version:     res.ToolSpec.Version,
				InstallPath: "/tools/" + name,
				BinPath:     "/bin/" + name,
			}, nil
		},
	}

	runtimeMock := &mockRuntimeInstaller{}
	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

	// All tools in same delegation group (no download tools)
	resources := []resource.Resource{
		&resource.Runtime{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindRuntime, Metadata: resource.Metadata{Name: "go"}},
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDownload,
				Version:     "1.23.0",
				Binaries:    []string{"go"},
				ToolBinPath: "~/go/bin",
				Source: &resource.DownloadSource{
					URL:      "https://example.com/go.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
				},
			},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "gopls"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.21.0", Package: &resource.Package{Name: "golang.org/x/tools/gopls"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "goimports"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.33.0", Package: &resource.Package{Name: "golang.org/x/tools/cmd/goimports"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "staticcheck"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.5.1", Package: &resource.Package{Name: "honnef.co/go/tools/cmd/staticcheck"}},
		},
	}

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err, "should not deadlock with all-delegation single group")

	assert.True(t, installedTools["gopls"])
	assert.True(t, installedTools["goimports"])
	assert.True(t, installedTools["staticcheck"])
}

func TestEngine_Apply_DelegationWithParallelismOne(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	var mu sync.Mutex
	installedTools := make(map[string]bool)

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			time.Sleep(5 * time.Millisecond)
			mu.Lock()
			installedTools[name] = true
			mu.Unlock()
			return &resource.ToolState{
				InstallerRef: res.ToolSpec.InstallerRef,
				RuntimeRef:   res.ToolSpec.RuntimeRef,
				Version:      res.ToolSpec.Version,
				InstallPath:  "/tools/" + name,
				BinPath:      "/bin/" + name,
			}, nil
		},
	}

	runtimeMock := &mockRuntimeInstaller{}
	eng := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)
	eng.SetParallelism(1)

	// Mix of delegation and download tools
	resources := []resource.Resource{
		&resource.Runtime{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindRuntime, Metadata: resource.Metadata{Name: "go"}},
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDownload,
				Version:     "1.23.0",
				Binaries:    []string{"go"},
				ToolBinPath: "~/go/bin",
				Source: &resource.DownloadSource{
					URL:      "https://example.com/go.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
				},
			},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "gopls"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.21.0", Package: &resource.Package{Name: "golang.org/x/tools/gopls"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "goimports"}},
			ToolSpec:     &resource.ToolSpec{RuntimeRef: "go", Version: "v0.33.0", Package: &resource.Package{Name: "golang.org/x/tools/cmd/goimports"}},
		},
		&resource.Tool{
			BaseResource: resource.BaseResource{APIVersion: resource.GroupVersion, ResourceKind: resource.KindTool, Metadata: resource.Metadata{Name: "rg"}},
			ToolSpec: &resource.ToolSpec{InstallerRef: "download", Version: "14.0.0", Source: &resource.DownloadSource{
				URL:      "https://example.com/rg.tar.gz",
				Checksum: &resource.Checksum{Value: "sha256:1111111111111111111111111111111111111111111111111111111111111111"},
			}},
		},
	}

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err, "should not deadlock with parallelism=1")

	assert.True(t, installedTools["gopls"])
	assert.True(t, installedTools["goimports"])
	assert.True(t, installedTools["rg"])
}

func TestEngine_Property_DelegationSerialization(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		nRuntimes := rapid.IntRange(1, 3).Draw(t, "numRuntimes")
		nToolsPerRuntime := rapid.IntRange(1, 5).Draw(t, "toolsPerRuntime")
		nDownloadTools := rapid.IntRange(0, 5).Draw(t, "downloadTools")
		parallelism := rapid.IntRange(1, 10).Draw(t, "parallelism")

		// Build resources programmatically
		var resources []resource.Resource

		// Track per-runtime concurrent counts
		runtimeConcurrent := make([]atomic.Int32, nRuntimes)

		for i := range nRuntimes {
			rtName := fmt.Sprintf("rt%d", i)
			resources = append(resources, &resource.Runtime{
				BaseResource: resource.BaseResource{
					APIVersion:   resource.GroupVersion,
					ResourceKind: resource.KindRuntime,
					Metadata:     resource.Metadata{Name: rtName},
				},
				RuntimeSpec: &resource.RuntimeSpec{
					Type:        resource.InstallTypeDownload,
					Version:     "1.0.0",
					Binaries:    []string{rtName},
					ToolBinPath: fmt.Sprintf("~/%s/bin", rtName),
					Source: &resource.DownloadSource{
						URL:      fmt.Sprintf("https://example.com/%s.tar.gz", rtName),
						Checksum: &resource.Checksum{Value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
					},
				},
			})

			for j := range nToolsPerRuntime {
				toolName := fmt.Sprintf("rt%d-tool%d", i, j)
				resources = append(resources, &resource.Tool{
					BaseResource: resource.BaseResource{
						APIVersion:   resource.GroupVersion,
						ResourceKind: resource.KindTool,
						Metadata:     resource.Metadata{Name: toolName},
					},
					ToolSpec: &resource.ToolSpec{
						RuntimeRef: rtName,
						Version:    "1.0.0",
						Package:    &resource.Package{Name: fmt.Sprintf("example.com/%s", toolName)},
					},
				})
			}
		}

		for i := range nDownloadTools {
			toolName := fmt.Sprintf("dl-tool%d", i)
			resources = append(resources, &resource.Tool{
				BaseResource: resource.BaseResource{
					APIVersion:   resource.GroupVersion,
					ResourceKind: resource.KindTool,
					Metadata:     resource.Metadata{Name: toolName},
				},
				ToolSpec: &resource.ToolSpec{
					InstallerRef: "download",
					Version:      "1.0.0",
					Source: &resource.DownloadSource{
						URL:      fmt.Sprintf("https://example.com/%s.tar.gz", toolName),
						Checksum: &resource.Checksum{Value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
					},
				},
			})
		}

		dir, err := os.MkdirTemp("", "engine-delegation-prop-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir) // Clean up per trial, not at end of all trials
		store, err := state.NewStore[state.UserState](dir)
		if err != nil {
			t.Fatal(err)
		}

		var installedMu sync.Mutex
		installed := make(map[string]bool)

		toolMock := &mockToolInstaller{
			installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
				// Track per-runtime concurrency
				if res.ToolSpec.RuntimeRef != "" {
					for i := range nRuntimes {
						rtName := fmt.Sprintf("rt%d", i)
						if res.ToolSpec.RuntimeRef == rtName {
							current := runtimeConcurrent[i].Add(1)
							defer runtimeConcurrent[i].Add(-1)
							if current > 1 {
								t.Fatalf("runtime %s had concurrent count %d > 1 for tool %s", rtName, current, name)
							}
							break
						}
					}
				}

				time.Sleep(5 * time.Millisecond)

				installedMu.Lock()
				installed[name] = true
				installedMu.Unlock()

				return &resource.ToolState{
					InstallerRef: res.ToolSpec.InstallerRef,
					RuntimeRef:   res.ToolSpec.RuntimeRef,
					Version:      res.ToolSpec.Version,
					InstallPath:  "/tools/" + name,
					BinPath:      "/bin/" + name,
				}, nil
			},
		}

		eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)
		eng.SetParallelism(parallelism)

		if err := eng.Apply(context.Background(), resources); err != nil {
			t.Fatalf("Apply failed: %v", err)
		}

		// Property: every tool must be installed
		totalTools := nRuntimes*nToolsPerRuntime + nDownloadTools
		if len(installed) != totalTools {
			t.Fatalf("expected %d installed tools, got %d", totalTools, len(installed))
		}
	})
}

// commandsToolCUE generates a CUE manifest for a commands-pattern tool.
// Optional fields (update, check, remove) are included only when non-empty.
func commandsToolCUE(name, install string, opts ...string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, `package tomei

tool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "%s"
	spec: {
		commands: {
			install: ["%s"]`, name, install)
	for i := 0; i+1 < len(opts); i += 2 {
		fmt.Fprintf(&sb, "\n\t\t\t%s:  [\"%s\"]", opts[i], opts[i+1])
	}
	sb.WriteString(`
		}
	}
}
`)
	return sb.String()
}

func TestEngine_Apply_CommandsPattern(t *testing.T) {
	t.Parallel()

	// Create test config directory with CUE file for a commands-pattern tool
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := commandsToolCUE("claude", "curl -fsSL https://cli.claude.ai/install.sh | sh",
		"update", "claude update",
		"check", "claude --version",
		"remove", "claude uninstall",
	)
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup mock and store
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	installedTools := make(map[string]*resource.ToolState)
	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			require.NotNil(t, res.ToolSpec.Commands)
			require.Equal(t, []string{"curl -fsSL https://cli.claude.ai/install.sh | sh"}, res.ToolSpec.Commands.Install)
			st := &resource.ToolState{
				Version:  "1.0.0",
				Commands: res.ToolSpec.Commands,
			}
			installedTools[name] = st
			return st, nil
		},
	}

	eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Verify tool was installed
	assert.Contains(t, installedTools, "claude")
	assert.Equal(t, "1.0.0", installedTools["claude"].Version)
	assert.NotNil(t, installedTools["claude"].Commands)

	// Verify state was updated
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)
	assert.NotNil(t, st.Tools["claude"])
	assert.Equal(t, "1.0.0", st.Tools["claude"].Version)
	assert.NotNil(t, st.Tools["claude"].Commands)
}

func TestEngine_Apply_CommandsPattern_NoSpuriousUpgrade(t *testing.T) {
	t.Parallel()

	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := commandsToolCUE("claude", "curl -fsSL https://cli.claude.ai/install.sh | sh",
		"check", "claude --version",
	)
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup mock and store with pre-existing state
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state matching the spec
	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Tools["claude"] = &resource.ToolState{
		Version:     "1.0.0",
		VersionKind: resource.VersionLatest,
		Commands: &resource.ToolCommandSet{
			CommandSet: resource.CommandSet{
				Install: []string{"curl -fsSL https://cli.claude.ai/install.sh | sh"},
				Check:   []string{"claude --version"},
			},
		},
	}
	err = store.Save(initialState)
	require.NoError(t, err)
	_ = store.Unlock()

	installCalled := false
	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			installCalled = true
			return nil, nil
		},
	}

	eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Install should not be called since tool is already installed
	assert.False(t, installCalled)
}

func TestEngine_Apply_UpdateTools_CommandsPattern(t *testing.T) {
	t.Parallel()

	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := commandsToolCUE("claude", "curl -fsSL https://cli.claude.ai/install.sh | sh",
		"update", "claude update",
		"check", "claude --version",
	)
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup mock and store with pre-existing state (VersionLatest so taint fires)
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Tools["claude"] = &resource.ToolState{
		Version:     "1.0.0",
		VersionKind: resource.VersionLatest,
		Commands: &resource.ToolCommandSet{
			CommandSet: resource.CommandSet{
				Install: []string{"curl -fsSL https://cli.claude.ai/install.sh | sh"},
				Check:   []string{"claude --version"},
			},
			Update: []string{"claude update"},
		},
	}
	err = store.Save(initialState)
	require.NoError(t, err)
	_ = store.Unlock()

	installCalled := false
	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			installCalled = true
			return &resource.ToolState{
				Version:     "1.1.0",
				VersionKind: resource.VersionLatest,
				Commands:    res.ToolSpec.Commands,
			}, nil
		},
	}

	eng := NewEngine(toolMock, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)
	eng.SetUpdateConfig(UpdateConfig{UpdateTools: true})

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Install should be called due to --update-tools taint
	assert.True(t, installCalled)

	// Verify state was updated
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()
	st, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, "1.1.0", st.Tools["claude"].Version)
}
