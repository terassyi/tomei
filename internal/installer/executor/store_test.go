package executor

import (
	"fmt"
	"os"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
	"pgregory.net/rapid"
)

// newLockedFactory creates a StateStoreFactory with Lock already acquired.
func newLockedFactory(t *testing.T) *StateStoreFactory {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewStore[state.UserState](dir)
	require.NoError(t, err)
	require.NoError(t, store.Lock())
	t.Cleanup(func() { _ = store.Unlock() })
	return NewStateStoreFactory(store)
}

// newLockedFactoryRapid creates a StateStoreFactory for use inside rapid.Check.
func newLockedFactoryRapid(t *rapid.T) *StateStoreFactory {
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
	return NewStateStoreFactory(store)
}

// --- Integration Tests ---

func TestToolStateStore_SaveAndLoad(t *testing.T) {
	f := newLockedFactory(t)
	ts := f.ToolStore()

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

func TestToolStateStore_Delete(t *testing.T) {
	f := newLockedFactory(t)
	ts := f.ToolStore()

	require.NoError(t, ts.Save("ripgrep", &resource.ToolState{Version: "14.1.1"}))
	require.NoError(t, ts.Delete("ripgrep"))

	_, exists, err := ts.Load("ripgrep")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestToolStateStore_LoadNotFound(t *testing.T) {
	f := newLockedFactory(t)
	ts := f.ToolStore()

	_, exists, err := ts.Load("nonexistent")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestRuntimeStateStore_SaveAndLoad(t *testing.T) {
	f := newLockedFactory(t)
	rs := f.RuntimeStore()

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

func TestRuntimeStateStore_Delete(t *testing.T) {
	f := newLockedFactory(t)
	rs := f.RuntimeStore()

	require.NoError(t, rs.Save("go", &resource.RuntimeState{Version: "1.23.0"}))
	require.NoError(t, rs.Delete("go"))

	_, exists, err := rs.Load("go")
	require.NoError(t, err)
	assert.False(t, exists)
}

// --- Concurrency Integration Tests ---

func TestToolStateStore_ConcurrentSave(t *testing.T) {
	f := newLockedFactory(t)
	ts := f.ToolStore()

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

func TestRuntimeStateStore_ConcurrentSave(t *testing.T) {
	f := newLockedFactory(t)
	rs := f.RuntimeStore()

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

func TestToolAndRuntimeStateStore_ConcurrentMixed(t *testing.T) {
	f := newLockedFactory(t)
	ts := f.ToolStore()
	rs := f.RuntimeStore()

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

func TestToolStateStore_Property_ConcurrentSavePreservesAll(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := newLockedFactoryRapid(t)
		ts := f.ToolStore()

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

func TestToolStateStore_Property_SaveDeleteConsistency(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := newLockedFactoryRapid(t)
		ts := f.ToolStore()

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

func TestToolStateStore_Property_ConcurrentSameTool_LastWriteWins(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		f := newLockedFactoryRapid(t)
		ts := f.ToolStore()

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
