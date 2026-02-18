//go:build integration

package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// updateFlagsTestEnv holds common test infrastructure for update-flags integration tests.
type updateFlagsTestEnv struct {
	configDir string
	store     *state.Store[state.UserState]
}

// setupUpdateFlagsTest creates a temporary directory structure, config dir, and state store.
func setupUpdateFlagsTest(t *testing.T) *updateFlagsTestEnv {
	t.Helper()
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	require.NoError(t, os.MkdirAll(configDir, 0755))

	stateDir := filepath.Join(tmpDir, "state")
	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	return &updateFlagsTestEnv{configDir: configDir, store: store}
}

// writeManifest writes a CUE manifest file into the config directory.
func (e *updateFlagsTestEnv) writeManifest(t *testing.T, filename, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(e.configDir, filename), []byte(content), 0644))
}

// loadResources loads resources from the config directory.
func (e *updateFlagsTestEnv) loadResources(t *testing.T) []resource.Resource {
	t.Helper()
	return loadResources(t, e.configDir)
}

// populateState pre-populates the state store with the given initial state.
func (e *updateFlagsTestEnv) populateState(t *testing.T, st *state.UserState) {
	t.Helper()
	require.NoError(t, e.store.Lock())
	require.NoError(t, e.store.Save(st))
	require.NoError(t, e.store.Unlock())
}

// TestEngine_UpdateTools_TaintsNonExactTools tests that SetUpdateTools taints
// non-exact (latest + alias) tools during PlanAll, causing upgrade actions.
func TestEngine_UpdateTools_TaintsNonExactTools(t *testing.T) {
	env := setupUpdateFlagsTest(t)

	// Manifest: two tools — one with empty version (latest), one exact
	cueContent := `package tomei

latestTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "latest-tool"
	spec: {
		installerRef: "download"
		source: { url: "https://example.com/latest-tool" }
	}
}

exactTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "exact-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: { url: "https://example.com/exact-tool" }
	}
}
`
	env.writeManifest(t, "tools.cue", cueContent)
	resources := env.loadResources(t)

	initialState := state.NewUserState()
	initialState.Tools["latest-tool"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "2.0.0",
		VersionKind:  resource.VersionLatest,
		BinPath:      "/mock/bin/latest-tool",
	}
	initialState.Tools["exact-tool"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "1.0.0",
		VersionKind:  resource.VersionExact,
		BinPath:      "/mock/bin/exact-tool",
	}
	env.populateState(t, initialState)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()

	ctx := context.Background()

	// Without update flag: PlanAll shows no actions (versions match state)
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), env.store)
	runtimeActions, _, toolActions, err := eng.PlanAll(ctx, resources)
	require.NoError(t, err)
	assert.Empty(t, runtimeActions)
	assert.Empty(t, toolActions, "expected no actions without --update-tools")

	// With update flag: PlanAll should show update for latest-tool only
	engWithUpdate := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), env.store)
	engWithUpdate.SetUpdateTools(true)

	runtimeActions, _, toolActions, err = engWithUpdate.PlanAll(ctx, resources)
	require.NoError(t, err)
	assert.Empty(t, runtimeActions)
	require.Len(t, toolActions, 1, "expected 1 action for latest-tool")
	assert.Equal(t, "latest-tool", toolActions[0].Name)
	assert.Equal(t, resource.ActionUpgrade, toolActions[0].Type)
}

// TestEngine_UpdateRuntimes_TaintsNonExactRuntimes tests that SetUpdateRuntimes
// taints alias-versioned runtimes during PlanAll.
func TestEngine_UpdateRuntimes_TaintsNonExactRuntimes(t *testing.T) {
	env := setupUpdateFlagsTest(t)

	cueContent := `package tomei

mockRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "mock"
	spec: {
		type: "delegation"
		version: "stable"
		toolBinPath: "/mock/bin"
		bootstrap: {
			install: ["echo installing"]
			check: ["true"]
		}
	}
}
`
	env.writeManifest(t, "runtime.cue", cueContent)
	resources := env.loadResources(t)

	initialState := state.NewUserState()
	initialState.Runtimes["mock"] = &resource.RuntimeState{
		Type:        resource.InstallTypeDelegation,
		Version:     "1.0.0",
		VersionKind: resource.VersionAlias,
		SpecVersion: "stable",
		ToolBinPath: "/mock/bin",
	}
	env.populateState(t, initialState)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()

	ctx := context.Background()

	// Without update flag: no actions
	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), env.store)
	runtimeActions, _, _, err := eng.PlanAll(ctx, resources)
	require.NoError(t, err)
	assert.Empty(t, runtimeActions)

	// With --update-runtimes: should show update action
	engWithUpdate := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), env.store)
	engWithUpdate.SetUpdateRuntimes(true)

	runtimeActions, _, _, err = engWithUpdate.PlanAll(ctx, resources)
	require.NoError(t, err)
	require.Len(t, runtimeActions, 1, "expected 1 action for mock runtime")
	assert.Equal(t, "mock", runtimeActions[0].Name)
	assert.Equal(t, resource.ActionUpgrade, runtimeActions[0].Type)
}

// TestEngine_UpdateRuntimes_CascadesToTools tests that updating a runtime
// triggers reinstallation of dependent tools only when the version actually changes.
func TestEngine_UpdateRuntimes_CascadesToTools(t *testing.T) {
	env := setupUpdateFlagsTest(t)

	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.26.0"
		source: { url: "https://example.com/go.tar.gz" }
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		commands: {
			install: ["go install {{.Package}}@{{.Version}}"]
			remove: ["rm -f {{.BinPath}}"]
		}
		taintOnUpgrade: true
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
	env.writeManifest(t, "resources.cue", cueContent)
	resources := env.loadResources(t)

	// Pre-populate state: runtime at older version
	initialState := state.NewUserState()
	initialState.Runtimes["go"] = &resource.RuntimeState{
		Type:           resource.InstallTypeDownload,
		Version:        "1.25.5",
		InstallPath:    "/mock/runtimes/go/1.25.5",
		Binaries:       []string{"go", "gofmt"},
		ToolBinPath:    "~/go/bin",
		TaintOnUpgrade: true,
	}
	initialState.Tools["gopls"] = &resource.ToolState{
		RuntimeRef: "go",
		Version:    "v0.16.0",
		BinPath:    "/mock/bin/gopls",
	}
	env.populateState(t, initialState)

	mockTool := newMockToolInstaller()
	// Mock runtime returns updated version
	mockRuntime := newMockRuntimeInstaller()
	mockRuntime.installFunc = func(_ context.Context, res *resource.Runtime, _ string) (*resource.RuntimeState, error) {
		return &resource.RuntimeState{
			Type:           res.RuntimeSpec.Type,
			Version:        "1.26.0",
			InstallPath:    "/mock/runtimes/go/1.26.0",
			Binaries:       res.RuntimeSpec.Binaries,
			ToolBinPath:    res.RuntimeSpec.ToolBinPath,
			Commands:       res.RuntimeSpec.Commands,
			TaintOnUpgrade: res.RuntimeSpec.TaintOnUpgrade,
		}, nil
	}

	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), env.store)

	ctx := context.Background()

	// Apply upgrade — version change 1.25.5 → 1.26.0
	err := eng.Apply(ctx, resources)
	require.NoError(t, err)

	// Verify runtime was upgraded and tool was reinstalled (taint cascade)
	require.NoError(t, env.store.Lock())
	st, err := env.store.Load()
	require.NoError(t, err)
	_ = env.store.Unlock()

	assert.Equal(t, "1.26.0", st.Runtimes["go"].Version)
	assert.Contains(t, mockTool.installed, "gopls", "gopls should be reinstalled due to cascade")
	assert.False(t, st.Tools["gopls"].IsTainted(), "taint should be cleared after reinstall")
}

// TestEngine_UpdateRuntimes_NoCascadeWhenVersionUnchanged tests that when
// a runtime is upgraded but the resolved version stays the same,
// dependent tools are NOT cascaded.
func TestEngine_UpdateRuntimes_NoCascadeWhenVersionUnchanged(t *testing.T) {
	env := setupUpdateFlagsTest(t)

	cueContent := `package tomei

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "go"
	spec: {
		type: "download"
		version: "1.25.5"
		source: { url: "https://example.com/go.tar.gz" }
		binaries: ["go", "gofmt"]
		toolBinPath: "~/go/bin"
		commands: {
			install: ["go install {{.Package}}@{{.Version}}"]
			remove: ["rm -f {{.BinPath}}"]
		}
		taintOnUpgrade: true
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
	env.writeManifest(t, "resources.cue", cueContent)
	resources := env.loadResources(t)

	// Pre-populate state: runtime with alias version kind, already resolved to 1.25.5
	initialState := state.NewUserState()
	initialState.Runtimes["go"] = &resource.RuntimeState{
		Type:           resource.InstallTypeDownload,
		Version:        "1.25.5",
		InstallPath:    "/mock/runtimes/go/1.25.5",
		Binaries:       []string{"go", "gofmt"},
		ToolBinPath:    "~/go/bin",
		TaintOnUpgrade: true,
	}
	initialState.Tools["gopls"] = &resource.ToolState{
		RuntimeRef: "go",
		Version:    "v0.16.0",
		BinPath:    "/mock/bin/gopls",
	}
	env.populateState(t, initialState)

	// Track tool installations count
	toolInstallCount := 0
	mockTool := newMockToolInstaller()
	mockTool.installFunc = func(_ context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
		toolInstallCount++
		return &resource.ToolState{
			RuntimeRef: res.ToolSpec.RuntimeRef,
			Package:    res.ToolSpec.Package,
			Version:    res.ToolSpec.Version,
			BinPath:    filepath.Join("/mock/bin", name),
		}, nil
	}

	// Runtime installer always returns the SAME version (no actual change)
	mockRuntime := newMockRuntimeInstaller()
	mockRuntime.installFunc = func(_ context.Context, res *resource.Runtime, name string) (*resource.RuntimeState, error) {
		return &resource.RuntimeState{
			Type:           res.RuntimeSpec.Type,
			Version:        "1.25.5", // same version — no actual upgrade
			InstallPath:    filepath.Join("/mock/runtimes", name, "1.25.5"),
			Binaries:       res.RuntimeSpec.Binaries,
			ToolBinPath:    res.RuntimeSpec.ToolBinPath,
			Commands:       res.RuntimeSpec.Commands,
			TaintOnUpgrade: res.RuntimeSpec.TaintOnUpgrade,
		}, nil
	}

	eng := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), env.store)

	ctx := context.Background()

	// Apply with state already matching — idempotent, no install calls
	err := eng.Apply(ctx, resources)
	require.NoError(t, err)
	assert.Equal(t, 0, toolInstallCount, "no tool installs on idempotent apply")

	// Taint runtime via --update-runtimes and apply.
	// The runtime will be "reinstalled" (mock returns same version 1.25.5).
	// Since version is unchanged, cascade should NOT happen.
	engWithUpdate := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), env.store)
	engWithUpdate.SetUpdateRuntimes(true)

	err = engWithUpdate.Apply(ctx, resources)
	require.NoError(t, err)

	// Tool install count should still be 0 (no cascade)
	assert.Equal(t, 0, toolInstallCount,
		"expected no tool reinstallation when runtime version is unchanged")
}

// TestEngine_UpdateTools_SyncVsUpdate tests that --sync only taints VersionLatest,
// while --update-tools taints both VersionLatest and VersionAlias.
func TestEngine_UpdateTools_SyncVsUpdate(t *testing.T) {
	env := setupUpdateFlagsTest(t)

	// Manifest with latest-tool (version omitted) and alias-tool and exact-tool
	cueContent := `package tomei

latestTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "latest-tool"
	spec: {
		installerRef: "download"
		source: { url: "https://example.com/latest-tool" }
	}
}

aliasTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "alias-tool"
	spec: {
		installerRef: "download"
		version: "stable"
		source: { url: "https://example.com/alias-tool" }
	}
}

exactTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "exact-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: { url: "https://example.com/exact-tool" }
	}
}
`
	env.writeManifest(t, "tools.cue", cueContent)
	resources := env.loadResources(t)

	initialState := state.NewUserState()
	initialState.Tools["latest-tool"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "2.0.0",
		VersionKind:  resource.VersionLatest,
		BinPath:      "/mock/bin/latest-tool",
	}
	initialState.Tools["alias-tool"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "2.0.0",
		VersionKind:  resource.VersionAlias,
		SpecVersion:  "stable",
		BinPath:      "/mock/bin/alias-tool",
	}
	initialState.Tools["exact-tool"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "1.0.0",
		VersionKind:  resource.VersionExact,
		BinPath:      "/mock/bin/exact-tool",
	}
	env.populateState(t, initialState)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()

	ctx := context.Background()

	// --sync only: should taint latest-tool only (not alias-tool)
	engSync := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), env.store)
	engSync.SetSyncMode(true)

	_, _, toolActions, err := engSync.PlanAll(ctx, resources)
	require.NoError(t, err)
	require.Len(t, toolActions, 1, "sync should taint only latest-tool")
	assert.Equal(t, "latest-tool", toolActions[0].Name)

	// --update-tools: should taint latest-tool AND alias-tool
	engUpdate := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), env.store)
	engUpdate.SetUpdateTools(true)

	_, _, toolActions, err = engUpdate.PlanAll(ctx, resources)
	require.NoError(t, err)
	require.Len(t, toolActions, 2, "update-tools should taint latest + alias")
	names := map[string]bool{}
	for _, a := range toolActions {
		names[a.Name] = true
	}
	assert.True(t, names["latest-tool"])
	assert.True(t, names["alias-tool"])
	assert.False(t, names["exact-tool"], "exact-tool should not be tainted")
}

// TestEngine_UpdateAll_TaintsBothToolsAndRuntimes tests the --update-all
// behavior: both non-exact tools and runtimes are tainted.
func TestEngine_UpdateAll_TaintsBothToolsAndRuntimes(t *testing.T) {
	env := setupUpdateFlagsTest(t)

	cueContent := `package tomei

mockRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Runtime"
	metadata: name: "mock"
	spec: {
		type: "delegation"
		version: "stable"
		toolBinPath: "/mock/bin"
		bootstrap: {
			install: ["echo installing"]
			check: ["true"]
		}
	}
}

latestTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "latest-tool"
	spec: {
		installerRef: "download"
		source: { url: "https://example.com/latest-tool" }
	}
}

exactTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "exact-tool"
	spec: {
		installerRef: "download"
		version: "1.0.0"
		source: { url: "https://example.com/exact-tool" }
	}
}
`
	env.writeManifest(t, "resources.cue", cueContent)
	resources := env.loadResources(t)

	initialState := state.NewUserState()
	initialState.Runtimes["mock"] = &resource.RuntimeState{
		Type:        resource.InstallTypeDelegation,
		Version:     "1.0.0",
		VersionKind: resource.VersionAlias,
		SpecVersion: "stable",
		ToolBinPath: "/mock/bin",
	}
	initialState.Tools["latest-tool"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "2.0.0",
		VersionKind:  resource.VersionLatest,
		BinPath:      "/mock/bin/latest-tool",
	}
	initialState.Tools["exact-tool"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "1.0.0",
		VersionKind:  resource.VersionExact,
		BinPath:      "/mock/bin/exact-tool",
	}
	env.populateState(t, initialState)

	mockTool := newMockToolInstaller()
	mockRuntime := newMockRuntimeInstaller()

	ctx := context.Background()

	// --update-all: both flags on
	engAll := engine.NewEngine(mockTool, mockRuntime, newMockInstallerRepositoryInstaller(), env.store)
	engAll.SetUpdateTools(true)
	engAll.SetUpdateRuntimes(true)

	runtimeActions, _, toolActions, err := engAll.PlanAll(ctx, resources)
	require.NoError(t, err)

	// Runtime with alias should be tainted
	require.Len(t, runtimeActions, 1, "expected 1 runtime action for alias runtime")
	assert.Equal(t, "mock", runtimeActions[0].Name)

	// latest-tool should be tainted; exact-tool should not
	require.Len(t, toolActions, 1, "expected 1 tool action for latest-tool")
	assert.Equal(t, "latest-tool", toolActions[0].Name)
}
