package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
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
		InstallerRef: res.RuntimeSpec.InstallerRef,
		Version:      res.RuntimeSpec.Version,
		InstallPath:  "/runtimes/" + name + "/" + res.RuntimeSpec.Version,
		Binaries:     res.RuntimeSpec.Binaries,
		ToolBinPath:  res.RuntimeSpec.ToolBinPath,
		Env:          res.RuntimeSpec.Env,
	}, nil
}

func (m *mockRuntimeInstaller) Remove(ctx context.Context, st *resource.RuntimeState, name string) error {
	if m.removeFunc != nil {
		return m.removeFunc(ctx, st, name)
	}
	return nil
}

func TestNewEngine(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := state.NewStore[state.UserState](tmpDir)
	require.NoError(t, err)

	toolMock := &mockToolInstaller{}
	runtimeMock := &mockRuntimeInstaller{}
	engine := NewEngine(toolMock, runtimeMock, store)

	assert.NotNil(t, engine)
}

func TestEngine_Apply(t *testing.T) {
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := `package toto

tool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: {
				value: "sha256:abc123"
			}
			archiveType: "tar.gz"
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
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

	engine := NewEngine(toolMock, runtimeMock, store)

	// Run Apply
	err = engine.Apply(context.Background(), configDir)
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
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "tools.cue")
	cueContent := `package toto

tool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: {
				value: "sha256:abc123"
			}
			archiveType: "tar.gz"
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
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

	engine := NewEngine(toolMock, runtimeMock, store)

	// Run Apply
	err = engine.Apply(context.Background(), configDir)
	require.NoError(t, err)

	// Install should not be called since version matches
	assert.False(t, installCalled)
}

func TestEngine_Apply_WithRuntime(t *testing.T) {
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

runtime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "myruntime"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/myruntime-1.0.0.tar.gz"
			checksum: {
				value: "sha256:abc123"
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
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: {
				value: "sha256:def456"
			}
			archiveType: "tar.gz"
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
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
				InstallerRef: res.RuntimeSpec.InstallerRef,
				Version:      res.RuntimeSpec.Version,
				InstallPath:  "/runtimes/" + name + "/" + res.RuntimeSpec.Version,
				Binaries:     res.RuntimeSpec.Binaries,
				ToolBinPath:  res.RuntimeSpec.ToolBinPath,
				Env:          res.RuntimeSpec.Env,
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

	engine := NewEngine(toolMock, runtimeMock, store)

	// Run Apply
	err = engine.Apply(context.Background(), configDir)
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
	// Create test config directory with CUE file
	// Include both runtime and dependent tool - tool should be tainted when runtime is upgraded
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

runtime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		installerRef: "download"
		version: "1.26.0"
		source: {
			url: "https://example.com/go-1.26.0.tar.gz"
			checksum: {
				value: "sha256:abc123"
			}
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
	}
}

tool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "gopls"
	spec: {
		installerRef: "go"
		version: "0.16.0"
		runtimeRef: "go"
		pkg: "golang.org/x/tools/gopls"
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Setup store with pre-existing state
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with runtime at version 1.25.0 and dependent tool
	require.NoError(t, store.Lock())
	initialState := state.NewUserState()
	initialState.Runtimes["go"] = &resource.RuntimeState{
		InstallerRef: "download",
		Version:      "1.25.0",
		InstallPath:  "/runtimes/go/1.25.0",
		Binaries:     []string{"go", "gofmt"},
	}
	initialState.Tools["gopls"] = &resource.ToolState{
		InstallerRef: "go",
		Version:      "0.16.0",
		RuntimeRef:   "go", // depends on go runtime
		InstallPath:  "/tools/gopls/0.16.0",
		BinPath:      "/bin/gopls",
	}
	err = store.Save(initialState)
	require.NoError(t, err)
	_ = store.Unlock()

	runtimeInstallCalled := false
	runtimeMock := &mockRuntimeInstaller{
		installFunc: func(ctx context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
			runtimeInstallCalled = true
			return &resource.RuntimeState{
				InstallerRef: res.RuntimeSpec.InstallerRef,
				Version:      res.RuntimeSpec.Version,
				InstallPath:  "/runtimes/" + name + "/" + res.RuntimeSpec.Version,
				Binaries:     res.RuntimeSpec.Binaries,
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

	engine := NewEngine(toolMock, runtimeMock, store)

	// Run Apply - should upgrade runtime and reinstall tainted tools
	err = engine.Apply(context.Background(), configDir)
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

func TestEngine_PlanAll(t *testing.T) {
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

runtime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "myruntime"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/myruntime.tar.gz"
			checksum: { value: "sha256:abc" }
		}
		binaries: ["mybin"]
		toolBinPath: "~/bin"
	}
}

tool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "test-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/test-tool.tar.gz"
			checksum: { value: "sha256:def" }
		}
	}
}
`
	err := os.WriteFile(cueFile, []byte(cueContent), 0644)
	require.NoError(t, err)

	// Setup store
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	engine := NewEngine(&mockToolInstaller{}, &mockRuntimeInstaller{}, store)

	// Run PlanAll
	runtimeActions, toolActions, err := engine.PlanAll(context.Background(), configDir)
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
