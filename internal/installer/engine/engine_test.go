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
	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/installer/download"
	"github.com/terassyi/toto/internal/installer/tool"
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
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
		Type:        res.RuntimeSpec.Type,
		Version:     res.RuntimeSpec.Version,
		InstallPath: "/runtimes/" + name + "/" + res.RuntimeSpec.Version,
		Binaries:    res.RuntimeSpec.Binaries,
		ToolBinPath: res.RuntimeSpec.ToolBinPath,
		Env:         res.RuntimeSpec.Env,
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

func TestNewEngine(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := state.NewStore[state.UserState](tmpDir)
	require.NoError(t, err)

	toolMock := &mockToolInstaller{}
	runtimeMock := &mockRuntimeInstaller{}
	engine := NewEngine(toolMock, runtimeMock, &mockInstallerRepositoryInstaller{}, store)

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
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

runtime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "myruntime"
	spec: {
		pattern: "download"
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
		pattern: "download"
		version: "1.26.0"
		source: {
			url: "https://example.com/go-1.26.0.tar.gz"
			checksum: {
				value: "sha256:abc123"
			}
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
		}
	}
}

tool: {
	apiVersion: "toto.terassyi.net/v1beta1"
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
		Type:        resource.InstallTypeDownload,
		Version:     "1.25.0",
		InstallPath: "/runtimes/go/1.25.0",
		Binaries:    []string{"go", "gofmt"},
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
				Type:        res.RuntimeSpec.Type,
				Version:     res.RuntimeSpec.Version,
				InstallPath: "/runtimes/" + name + "/" + res.RuntimeSpec.Version,
				Binaries:    res.RuntimeSpec.Binaries,
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

func TestEngine_Apply_DependencyOrder(t *testing.T) {
	// Test that DAG-based execution respects dependency order:
	// Runtime(go) -> Tool(pnpm) -> Installer(pnpm) -> Tool(biome)
	// Tool can directly reference Runtime via runtimeRef
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

goRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		pattern: "download"
		version: "1.23.0"
		source: {
			url: "https://example.com/go-1.23.0.tar.gz"
			checksum: { value: "sha256:abc123" }
		}
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "/runtimes/go/1.23.0"
			GOBIN: "~/go/bin"
		}
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
		}
	}
}

pnpmTool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "pnpm"
	spec: {
		runtimeRef: "go"
		package: "github.com/pnpm/pnpm"
		version: "v8.0.0"
	}
}

pnpmInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "pnpm"
	spec: {
		pattern: "delegation"
		toolRef: "pnpm"
		commands: {
			install: "pnpm add -g {{.Package}}@{{.Version}}"
		}
	}
}

biomeTool: {
	apiVersion: "toto.terassyi.net/v1beta1"
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
	// Test that circular dependencies are detected and rejected
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

installerA: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "installer-a"
	spec: {
		pattern: "delegation"
		toolRef: "tool-b"
		commands: {
			install: "install-a {{.Package}}"
		}
	}
}

toolB: {
	apiVersion: "toto.terassyi.net/v1beta1"
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
	// Test that independent tools are executed in parallel
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

aquaInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: {
		pattern: "download"
	}
}

ripgrep: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "aqua"
		version: "14.0.0"
		source: {
			url: "https://example.com/ripgrep.tar.gz"
			checksum: { value: "sha256:rg" }
		}
	}
}

fd: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version: "9.0.0"
		source: {
			url: "https://example.com/fd.tar.gz"
			checksum: { value: "sha256:fd" }
		}
	}
}

bat: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "aqua"
		version: "0.24.0"
		source: {
			url: "https://example.com/bat.tar.gz"
			checksum: { value: "sha256:bat" }
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

func TestEngine_Apply_ParallelExecution_CancelOnError(t *testing.T) {
	// Test that when one tool fails in a parallel layer, other tools are canceled
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

aquaInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: {
		pattern: "download"
	}
}

ripgrep: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "aqua"
		version: "14.0.0"
		source: {
			url: "https://example.com/ripgrep.tar.gz"
			checksum: { value: "sha256:rg" }
		}
	}
}

fd: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version: "9.0.0"
		source: {
			url: "https://example.com/fd.tar.gz"
			checksum: { value: "sha256:fd" }
		}
	}
}

bat: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "aqua"
		version: "0.24.0"
		source: {
			url: "https://example.com/bat.tar.gz"
			checksum: { value: "sha256:bat" }
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
	var canceledCount atomic.Int32

	toolMock := &mockToolInstaller{
		installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
			// fd fails immediately
			if name == "fd" {
				return nil, fmt.Errorf("simulated install failure for fd")
			}

			// Other tools simulate work and check for cancellation
			select {
			case <-ctx.Done():
				canceledCount.Add(1)
				return nil, ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}

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

	// Run Apply - should return error
	err = eng.Apply(context.Background(), resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fd")

	// fd should not be installed
	assert.False(t, installedTools["fd"], "fd should not be installed")
}

func TestEngine_Apply_RuntimeBeforeTool_SameLayer(t *testing.T) {
	// Test that Runtime nodes always execute before Tool nodes even in the same layer
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

goRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		pattern: "download"
		version: "1.25.0"
		source: {
			url: "https://example.com/go.tar.gz"
			checksum: { value: "sha256:go" }
		}
		binaries: ["go"]
		toolBinPath: "~/go/bin"
	}
}

ripgrep: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "ripgrep"
	spec: {
		installerRef: "download"
		version: "14.0.0"
		source: {
			url: "https://example.com/ripgrep.tar.gz"
			checksum: { value: "sha256:rg" }
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
	// Test that multiple independent runtimes are executed in parallel
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

goRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		pattern: "download"
		version: "1.25.0"
		source: {
			url: "https://example.com/go.tar.gz"
			checksum: { value: "sha256:go" }
		}
		binaries: ["go"]
		toolBinPath: "~/go/bin"
	}
}

rustRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "rust"
	spec: {
		pattern: "download"
		version: "1.80.0"
		source: {
			url: "https://example.com/rust.tar.gz"
			checksum: { value: "sha256:rust" }
		}
		binaries: ["rustc", "cargo"]
		toolBinPath: "~/.cargo/bin"
	}
}

nodeRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "node"
	spec: {
		pattern: "download"
		version: "22.0.0"
		source: {
			url: "https://example.com/node.tar.gz"
			checksum: { value: "sha256:node" }
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
	// Test that parallelism is limited to the configured value
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")

	// Create 6 tools to exceed parallelism limit of 2
	var sb strings.Builder
	sb.WriteString(`package toto

aquaInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: { pattern: "download" }
}
`)
	toolDefs := []struct{ cueKey, name string }{
		{"toolA", "tool-a"}, {"toolB", "tool-b"}, {"toolC", "tool-c"},
		{"toolD", "tool-d"}, {"toolE", "tool-e"}, {"toolF", "tool-f"},
	}
	for _, td := range toolDefs {
		fmt.Fprintf(&sb, `
%s: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "%s"
	spec: {
		installerRef: "aqua"
		version: "1.0.0"
		source: {
			url: "https://example.com/%s.tar.gz"
			checksum: { value: "sha256:%s" }
		}
	}
}
`, td.cueKey, td.name, td.name, td.name)
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
	// Test that ResolverConfigurer callback is called after state is loaded
	// but before any installation happens
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

	// Load resources from config
	loader := config.NewLoader(nil)
	resources, err := loader.Load(configDir)
	require.NoError(t, err)

	// Setup store with pre-existing registry state
	stateDir := t.TempDir()
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Pre-populate state with registry info (simulating toto init)
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
	// Test that ResolverConfigurer handles nil registry gracefully
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
			checksum: { value: "sha256:abc123" }
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

	// No pre-populated state (simulating fresh install without toto init)

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
	// Create test config directory with CUE file
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

runtime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "myruntime"
	spec: {
		pattern: "download"
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
	sb.WriteString(`package toto

aquaInstaller: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Installer"
	metadata: name: "aqua"
	spec: { pattern: "download" }
}
`)
	for _, name := range toolNames {
		fmt.Fprintf(&sb, `
%s: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "%s"
	spec: {
		installerRef: "aqua"
		version: "1.0.0"
		source: {
			url: "https://example.com/%s.tar.gz"
			checksum: { value: "sha256:%s" }
		}
	}
}
`, name, name, name, name)
	}
	return sb.String()
}

// generateRuntimesAndToolsCUE generates a CUE manifest with N runtimes and M tools.
func generateRuntimesAndToolsCUE(runtimeNames, toolNames []string) string {
	var sb strings.Builder
	sb.WriteString("package toto\n")

	for _, name := range runtimeNames {
		fmt.Fprintf(&sb, `
%sRuntime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "%s"
	spec: {
		pattern: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/%s.tar.gz"
			checksum: { value: "sha256:%s" }
		}
		binaries: ["%s"]
		toolBinPath: "~/%s/bin"
	}
}
`, name, name, name, name, name, name)
	}

	for _, name := range toolNames {
		fmt.Fprintf(&sb, `
%s: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "%s"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/%s.tar.gz"
			checksum: { value: "sha256:%s" }
		}
	}
}
`, name, name, name, name)
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

func TestEngine_Property_CancelOnError(t *testing.T) {
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
			installFunc: func(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
				if name == failName {
					return nil, fmt.Errorf("simulated failure for %s", name)
				}

				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(50 * time.Millisecond):
				}

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

		// Property: the failed tool must NOT be in state
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
	})
}

func TestEngine_Apply_ToolSet(t *testing.T) {
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
	configDir := t.TempDir()
	stateDir := t.TempDir()

	// Initial config: runtime + delegated tool
	cueContent := `package toto

runtime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		pattern: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/go.tar.gz"
			checksum: value: "sha256:abc123"
		}
		binaries: ["go"]
		toolBinPath: "~/go/bin"
	}
}

gopls: {
	apiVersion: "toto.terassyi.net/v1beta1"
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
	cueContentV2 := `package toto

gopls: {
	apiVersion: "toto.terassyi.net/v1beta1"
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
	cueContent := `package toto

gopls: {
	apiVersion: "toto.terassyi.net/v1beta1"
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
	configDir := t.TempDir()
	stateDir := t.TempDir()

	// Initial config: runtime + delegated tool
	cueContent := `package toto

runtime: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		pattern: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/go.tar.gz"
			checksum: value: "sha256:abc123"
		}
		binaries: ["go"]
		toolBinPath: "~/go/bin"
	}
}

gopls: {
	apiVersion: "toto.terassyi.net/v1beta1"
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
	cueContentV2 := `package toto

placeholder: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fzf"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: {
			url: "https://example.com/fzf.tar.gz"
			checksum: value: "sha256:abc123"
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

func TestEngine_SyncMode_TaintLatestTools(t *testing.T) {
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
			name: "alias version tool is not tainted",
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
			stateDir := t.TempDir()
			store, err := state.NewStore[state.UserState](stateDir)
			require.NoError(t, err)

			// Pre-populate state
			require.NoError(t, store.Lock())
			initialState := state.NewUserState()
			initialState.Tools = tt.tools
			require.NoError(t, store.Save(initialState))
			require.NoError(t, store.Unlock())

			eng := NewEngine(&mockToolInstaller{}, &mockRuntimeInstaller{}, &mockInstallerRepositoryInstaller{}, store)
			eng.SetSyncMode(true)

			// Call taintLatestTools directly
			require.NoError(t, store.Lock())
			st, err := store.Load()
			require.NoError(t, err)
			err = eng.taintLatestTools(st)
			require.NoError(t, err)
			require.NoError(t, store.Unlock())

			// Verify tainted
			for _, name := range tt.wantTainted {
				assert.True(t, st.Tools[name].IsTainted(), "tool %s should be tainted", name)
				assert.Equal(t, "sync_update", st.Tools[name].TaintReason)
			}

			// Verify not tainted
			for _, name := range tt.wantNotTainted {
				assert.False(t, st.Tools[name].IsTainted(), "tool %s should not be tainted", name)
			}
		})
	}
}

func TestEngine_SyncMode_Apply(t *testing.T) {
	// End-to-end: sync mode triggers reinstall of latest-specified tool
	configDir := t.TempDir()
	stateDir := t.TempDir()

	cueContent := `package toto

fd: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "download"
		version: "9.0.0"
		source: {
			url: "https://example.com/fd.tar.gz"
			checksum: { value: "sha256:fd" }
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
	eng.SetSyncMode(true)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	// Tool should be reinstalled because it was tainted by sync mode
	assert.True(t, installCalled, "latest tool should be reinstalled in sync mode")
}

func TestEngine_SyncMode_ExactVersionNotReinstalled(t *testing.T) {
	// Sync mode should NOT reinstall tools with exact version
	configDir := t.TempDir()
	stateDir := t.TempDir()

	cueContent := `package toto

rg: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "download"
		version: "14.0.0"
		source: {
			url: "https://example.com/rg.tar.gz"
			checksum: { value: "sha256:rg" }
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
	eng.SetSyncMode(true)

	err = eng.Apply(context.Background(), resources)
	require.NoError(t, err)

	assert.False(t, installCalled, "exact version tool should not be reinstalled in sync mode")
}

func TestEngine_Apply_InstallerRepository(t *testing.T) {
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

repo: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "InstallerRepository"
	metadata: name: "bitnami"
	spec: {
		installerRef: "helm"
		source: {
			type: "delegation"
			url:  "https://charts.bitnami.com/bitnami"
			commands: {
				install: "helm repo add bitnami https://charts.bitnami.com/bitnami"
				check:   "helm repo list | grep bitnami"
				remove:  "helm repo remove bitnami"
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
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

repo: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "InstallerRepository"
	metadata: name: "bitnami"
	spec: {
		installerRef: "helm"
		source: {
			type: "delegation"
			url:  "https://charts.bitnami.com/bitnami"
			commands: {
				install: "helm repo add bitnami https://charts.bitnami.com/bitnami"
				check:   "helm repo list | grep bitnami"
				remove:  "helm repo remove bitnami"
			}
		}
	}
}

tool: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "nginx"
	spec: {
		installerRef: "download"
		repositoryRef: "bitnami"
		version: "1.0.0"
		source: {
			url: "https://example.com/nginx.tar.gz"
			checksum: value: "sha256:abc123"
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
	eng.SetSyncMode(true)

	// Apply with empty resources - should trigger removal
	err = eng.Apply(context.Background(), []resource.Resource{})
	require.NoError(t, err)

	assert.True(t, removedRepos["bitnami"])
}

func TestEngine_PlanAll_InstallerRepository(t *testing.T) {
	configDir := t.TempDir()
	cueFile := filepath.Join(configDir, "resources.cue")
	cueContent := `package toto

repo: {
	apiVersion: "toto.terassyi.net/v1beta1"
	kind: "InstallerRepository"
	metadata: name: "bitnami"
	spec: {
		installerRef: "helm"
		source: {
			type: "delegation"
			url:  "https://charts.bitnami.com/bitnami"
			commands: {
				install: "helm repo add bitnami https://charts.bitnami.com/bitnami"
				check:   "helm repo list | grep bitnami"
				remove:  "helm repo remove bitnami"
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
