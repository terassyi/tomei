package executor

import (
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
	"pgregory.net/rapid"
)

// newStateCache creates a StateCache with Lock already acquired and Init called.
func newStateCache(t *testing.T) *StateCache {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewStore[state.UserState](dir)
	require.NoError(t, err)
	require.NoError(t, store.Lock())
	t.Cleanup(func() { _ = store.Unlock() })
	sc := NewStateCache(store)
	sc.Init(state.NewUserState())
	return sc
}

// newStateCacheRapid creates a StateCache for use inside rapid.Check.
func newStateCacheRapid(t *rapid.T) *StateCache {
	dir, err := os.MkdirTemp("", "store-test-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore[state.UserState](dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Lock(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Unlock() })
	sc := NewStateCache(store)
	sc.Init(state.NewUserState())
	return sc
}

// --- StateCache Tests ---

func TestStateCache_InitAndFlush(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := state.NewStore[state.UserState](dir)
	require.NoError(t, err)
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()

	sc := NewStateCache(store)
	st := state.NewUserState()
	st.Tools["rg"] = &resource.ToolState{Version: "14.0.0"}
	sc.Init(st)

	// Save a tool via cachedStore to mark dirty
	ts := NewToolStore(sc)
	require.NoError(t, ts.Save("fd", &resource.ToolState{Version: "9.0.0"}))

	// Flush writes to disk
	require.NoError(t, sc.Flush())

	// Read directly from disk to verify
	diskState, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, "14.0.0", diskState.Tools["rg"].Version)
	assert.Equal(t, "9.0.0", diskState.Tools["fd"].Version)
}

func TestStateCache_FlushOnlyWhenDirty(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := state.NewStore[state.UserState](dir)
	require.NoError(t, err)
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()

	sc := NewStateCache(store)
	sc.Init(state.NewUserState())

	// Flush without changes should be a no-op (no error, no disk write)
	require.NoError(t, sc.Flush())
}

func TestStateCache_Snapshot(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	ts := NewToolStore(sc)

	require.NoError(t, ts.Save("rg", &resource.ToolState{Version: "14.0.0"}))

	snap := sc.Snapshot()
	assert.Equal(t, "14.0.0", snap.Tools["rg"].Version)
}

func TestStateCache_ConcurrentSaveThenFlush(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := state.NewStore[state.UserState](dir)
	require.NoError(t, err)
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()

	sc := NewStateCache(store)
	sc.Init(state.NewUserState())
	ts := NewToolStore(sc)

	const n = 20
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			name := fmt.Sprintf("tool-%d", i)
			_ = ts.Save(name, &resource.ToolState{Version: fmt.Sprintf("1.0.%d", i)})
		})
	}
	wg.Wait()

	require.NoError(t, sc.Flush())

	diskState, err := store.Load()
	require.NoError(t, err)
	for i := range n {
		name := fmt.Sprintf("tool-%d", i)
		assert.NotNil(t, diskState.Tools[name], "tool %s should be on disk", name)
		assert.Equal(t, fmt.Sprintf("1.0.%d", i), diskState.Tools[name].Version)
	}
}

func TestCachedStore_markDirtyViaSave(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store, err := state.NewStore[state.UserState](dir)
	require.NoError(t, err)
	require.NoError(t, store.Lock())
	defer func() { _ = store.Unlock() }()

	sc := NewStateCache(store)
	st := state.NewUserState()
	st.Tools["exa"] = &resource.ToolState{Version: "0.10.0", TaintReason: resource.TaintReasonRuntimeUpgraded}
	sc.Init(st)

	ts := NewToolStore(sc)
	require.NoError(t, ts.Save("exa", &resource.ToolState{Version: "0.10.1"}))

	require.NoError(t, sc.Flush())

	diskState, err := store.Load()
	require.NoError(t, err)
	assert.Equal(t, "0.10.1", diskState.Tools["exa"].Version)
	assert.False(t, diskState.Tools["exa"].IsTainted())
}

// --- cachedStore Integration Tests ---

func TestToolStore_SaveAndLoad(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	ts := NewToolStore(sc)

	toolState := &resource.ToolState{
		InstallerRef: "download",
		Version:      "14.1.1",
		InstallPath:  "/tools/ripgrep/14.1.1",
		BinPath:      "/bin/rg",
	}

	require.NoError(t, ts.Save("ripgrep", toolState))

	loaded, exists, err := ts.Load("ripgrep")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "14.1.1", loaded.Version)
}

func TestToolStore_Delete(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	ts := NewToolStore(sc)

	require.NoError(t, ts.Save("ripgrep", &resource.ToolState{Version: "14.1.1"}))
	require.NoError(t, ts.Delete("ripgrep"))

	_, exists, err := ts.Load("ripgrep")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestToolStore_LoadNotFound(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	ts := NewToolStore(sc)

	_, exists, err := ts.Load("nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRuntimeStore_SaveAndLoad(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	rs := NewRuntimeStore(sc)

	runtimeState := &resource.RuntimeState{
		Version:     "1.23.0",
		InstallPath: "/runtimes/go/1.23.0",
		BinDir:      "/runtimes/go/1.23.0/bin",
	}

	require.NoError(t, rs.Save("go", runtimeState))

	loaded, exists, err := rs.Load("go")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "1.23.0", loaded.Version)
}

func TestRuntimeStore_Delete(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	rs := NewRuntimeStore(sc)

	require.NoError(t, rs.Save("go", &resource.RuntimeState{Version: "1.23.0"}))
	require.NoError(t, rs.Delete("go"))

	_, exists, err := rs.Load("go")
	require.NoError(t, err)
	assert.False(t, exists)
}

// --- Concurrency Integration Tests ---

func TestToolStore_ConcurrentSave(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	ts := NewToolStore(sc)

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := range n {
		wg.Go(func() {
			name := fmt.Sprintf("tool-%d", i)
			errs[i] = ts.Save(name, &resource.ToolState{
				Version:     fmt.Sprintf("1.0.%d", i),
				InstallPath: fmt.Sprintf("/tools/%s", name),
				BinPath:     fmt.Sprintf("/bin/%s", name),
			})
		})
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "save failed for tool-%d", i)
	}

	for i := range n {
		name := fmt.Sprintf("tool-%d", i)
		loaded, exists, err := ts.Load(name)
		require.NoError(t, err)
		assert.True(t, exists, "tool %s missing", name)
		assert.Equal(t, fmt.Sprintf("1.0.%d", i), loaded.Version)
	}
}

func TestRuntimeStore_ConcurrentSave(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	rs := NewRuntimeStore(sc)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := range n {
		wg.Go(func() {
			name := fmt.Sprintf("runtime-%d", i)
			errs[i] = rs.Save(name, &resource.RuntimeState{
				Version:     fmt.Sprintf("1.%d.0", i),
				InstallPath: fmt.Sprintf("/runtimes/%s", name),
				BinDir:      fmt.Sprintf("/runtimes/%s/bin", name),
			})
		})
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "save failed for runtime-%d", i)
	}

	for i := range n {
		name := fmt.Sprintf("runtime-%d", i)
		loaded, exists, err := rs.Load(name)
		require.NoError(t, err)
		assert.True(t, exists, "runtime %s missing", name)
		assert.Equal(t, fmt.Sprintf("1.%d.0", i), loaded.Version)
	}
}

func TestToolAndRuntimeStore_ConcurrentMixed(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	ts := NewToolStore(sc)
	rs := NewRuntimeStore(sc)

	const nTools = 10
	const nRuntimes = 5
	var wg sync.WaitGroup

	toolErrs := make([]error, nTools)
	for i := range nTools {
		wg.Go(func() {
			name := fmt.Sprintf("tool-%d", i)
			toolErrs[i] = ts.Save(name, &resource.ToolState{
				Version: fmt.Sprintf("1.0.%d", i),
			})
		})
	}

	runtimeErrs := make([]error, nRuntimes)
	for i := range nRuntimes {
		wg.Go(func() {
			name := fmt.Sprintf("runtime-%d", i)
			runtimeErrs[i] = rs.Save(name, &resource.RuntimeState{
				Version: fmt.Sprintf("2.%d.0", i),
			})
		})
	}

	wg.Wait()

	for i, err := range toolErrs {
		require.NoError(t, err, "tool save failed for tool-%d", i)
	}
	for i, err := range runtimeErrs {
		require.NoError(t, err, "runtime save failed for runtime-%d", i)
	}

	for i := range nTools {
		name := fmt.Sprintf("tool-%d", i)
		_, exists, err := ts.Load(name)
		require.NoError(t, err)
		assert.True(t, exists, "tool %s missing", name)
	}

	for i := range nRuntimes {
		name := fmt.Sprintf("runtime-%d", i)
		_, exists, err := rs.Load(name)
		require.NoError(t, err)
		assert.True(t, exists, "runtime %s missing", name)
	}
}

// --- Property-Based Tests ---

func TestToolStore_Property_ConcurrentSavePreservesAll(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		sc := newStateCacheRapid(t)
		ts := NewToolStore(sc)

		n := rapid.IntRange(2, 15).Draw(t, "numTools")
		type toolDef struct {
			name    string
			version string
		}
		tools := make([]toolDef, n)
		for i := range n {
			tools[i] = toolDef{
				name:    fmt.Sprintf("tool-%d", i),
				version: rapid.StringMatching(`[0-9]+\.[0-9]+\.[0-9]+`).Draw(t, fmt.Sprintf("version-%d", i)),
			}
		}

		var wg sync.WaitGroup
		for _, td := range tools {
			wg.Go(func() {
				if err := ts.Save(td.name, &resource.ToolState{Version: td.version}); err != nil {
					t.Fatalf("save failed for %s: %v", td.name, err)
				}
			})
		}
		wg.Wait()

		// Property: every tool must be present with its version
		for _, td := range tools {
			loaded, exists, err := ts.Load(td.name)
			if err != nil {
				t.Fatalf("load failed for %s: %v", td.name, err)
			}
			if !exists {
				t.Fatalf("tool %s missing after concurrent save", td.name)
			}
			if loaded.Version != td.version {
				t.Fatalf("tool %s: got version %q, want %q", td.name, loaded.Version, td.version)
			}
		}
	})
}

func TestToolStore_Property_SaveDeleteConsistency(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		sc := newStateCacheRapid(t)
		ts := NewToolStore(sc)

		n := rapid.IntRange(2, 10).Draw(t, "numTools")

		// Save all tools first (sequential)
		for i := range n {
			name := fmt.Sprintf("tool-%d", i)
			if err := ts.Save(name, &resource.ToolState{Version: fmt.Sprintf("1.0.%d", i)}); err != nil {
				t.Fatalf("save failed for %s: %v", name, err)
			}
		}

		// Randomly decide which tools to delete
		deleteSet := make(map[string]bool)
		for i := range n {
			if rapid.Bool().Draw(t, fmt.Sprintf("delete-%d", i)) {
				deleteSet[fmt.Sprintf("tool-%d", i)] = true
			}
		}

		// Concurrent delete
		var wg sync.WaitGroup
		for name := range deleteSet {
			wg.Go(func() {
				if err := ts.Delete(name); err != nil {
					t.Fatalf("delete failed for %s: %v", name, err)
				}
			})
		}
		wg.Wait()

		// Property: deleted tools gone, others remain
		for i := range n {
			name := fmt.Sprintf("tool-%d", i)
			_, exists, err := ts.Load(name)
			if err != nil {
				t.Fatalf("load failed for %s: %v", name, err)
			}
			if deleteSet[name] && exists {
				t.Fatalf("tool %s should have been deleted", name)
			}
			if !deleteSet[name] && !exists {
				t.Fatalf("tool %s should still exist", name)
			}
		}
	})
}

// --- InstallerRepository Store Tests ---

func TestInstallerRepositoryStore_SaveAndLoad(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	irs := NewInstallerRepositoryStore(sc)

	repoState := &resource.InstallerRepositoryState{
		InstallerRef:  "helm",
		SourceType:    resource.InstallerRepositorySourceDelegation,
		RemoveCommand: []string{"helm repo remove bitnami"},
	}

	require.NoError(t, irs.Save("bitnami", repoState))

	loaded, exists, err := irs.Load("bitnami")
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, "helm", loaded.InstallerRef)
	assert.Equal(t, resource.InstallerRepositorySourceDelegation, loaded.SourceType)
}

func TestInstallerRepositoryStore_Delete(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	irs := NewInstallerRepositoryStore(sc)

	require.NoError(t, irs.Save("bitnami", &resource.InstallerRepositoryState{InstallerRef: "helm"}))
	require.NoError(t, irs.Delete("bitnami"))

	_, exists, err := irs.Load("bitnami")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestInstallerRepositoryStore_LoadNotFound(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	irs := NewInstallerRepositoryStore(sc)

	_, exists, err := irs.Load("nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestInstallerRepositoryStore_ConcurrentSave(t *testing.T) {
	t.Parallel()
	sc := newStateCache(t)
	irs := NewInstallerRepositoryStore(sc)

	const n = 10
	var wg sync.WaitGroup
	errs := make([]error, n)

	for i := range n {
		wg.Go(func() {
			name := fmt.Sprintf("repo-%d", i)
			errs[i] = irs.Save(name, &resource.InstallerRepositoryState{
				InstallerRef: "helm",
				SourceType:   resource.InstallerRepositorySourceDelegation,
			})
		})
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "save failed for repo-%d", i)
	}

	for i := range n {
		name := fmt.Sprintf("repo-%d", i)
		_, exists, err := irs.Load(name)
		require.NoError(t, err)
		assert.True(t, exists, "repo %s missing", name)
	}
}

func TestToolStore_Property_ConcurrentSameTool_LastWriteWins(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		sc := newStateCacheRapid(t)
		ts := NewToolStore(sc)

		// Multiple goroutines write different versions to the same tool name
		versions := rapid.SliceOfN(
			rapid.StringMatching(`[0-9]+\.[0-9]+\.[0-9]+`),
			2, 10,
		).Draw(t, "versions")

		var wg sync.WaitGroup
		for _, v := range versions {
			wg.Go(func() {
				_ = ts.Save("contested", &resource.ToolState{Version: v})
			})
		}
		wg.Wait()

		// Property: final value must be one of the written versions
		loaded, exists, err := ts.Load("contested")
		if err != nil {
			t.Fatalf("load failed: %v", err)
		}
		if !exists {
			t.Fatal("contested tool missing")
		}
		if !slices.Contains(versions, loaded.Version) {
			t.Fatalf("final version %q not in written versions %v", loaded.Version, versions)
		}
	})
}

// --- StateCache Property Tests ---

func TestStateCache_Property_ConcurrentSaveThenFlush(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		sc := newStateCacheRapid(t)
		ts := NewToolStore(sc)

		n := rapid.IntRange(2, 15).Draw(t, "numTools")
		type toolDef struct {
			name    string
			version string
		}
		tools := make([]toolDef, n)
		for i := range n {
			tools[i] = toolDef{
				name:    fmt.Sprintf("tool-%d", i),
				version: rapid.StringMatching(`[0-9]+\.[0-9]+\.[0-9]+`).Draw(t, fmt.Sprintf("version-%d", i)),
			}
		}

		var wg sync.WaitGroup
		for _, td := range tools {
			wg.Go(func() {
				_ = ts.Save(td.name, &resource.ToolState{Version: td.version})
			})
		}
		wg.Wait()

		if err := sc.Flush(); err != nil {
			t.Fatalf("flush failed: %v", err)
		}

		// Property: snapshot reflects all saves
		snap := sc.Snapshot()
		for _, td := range tools {
			v, ok := snap.Tools[td.name]
			if !ok {
				t.Fatalf("tool %s missing from snapshot after flush", td.name)
			}
			if v.Version != td.version {
				t.Fatalf("tool %s: snapshot version %q, want %q", td.name, v.Version, td.version)
			}
		}
	})
}
