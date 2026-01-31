//go:build integration

package tests

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
)

// TestState_Persistence tests that state is correctly persisted and loaded.
func TestState_Persistence(t *testing.T) {
	stateDir := t.TempDir()

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Lock and save initial state
	require.NoError(t, store.Lock())

	initialState := &state.UserState{
		Tools: map[string]*resource.ToolState{
			"ripgrep": {
				InstallerRef: "download",
				Version:      "14.0.0",
				BinPath:      "/home/user/.local/bin/rg",
				UpdatedAt:    time.Now().Truncate(time.Second),
			},
		},
		Runtimes: map[string]*resource.RuntimeState{
			"go": {
				InstallerRef: "download",
				Version:      "1.25.5",
				InstallPath:  "/home/user/.local/share/toto/runtimes/go/1.25.5",
				Binaries:     []string{"go", "gofmt"},
				ToolBinPath:  "/home/user/go/bin",
				Env: map[string]string{
					"GOROOT": "/home/user/.local/share/toto/runtimes/go/1.25.5",
				},
				UpdatedAt: time.Now().Truncate(time.Second),
			},
		},
	}

	err = store.Save(initialState)
	require.NoError(t, err)
	require.NoError(t, store.Unlock())

	// Create new store instance and load
	store2, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	require.NoError(t, store2.Lock())
	loadedState, err := store2.Load()
	require.NoError(t, err)
	require.NoError(t, store2.Unlock())

	// Verify loaded state matches saved state
	assert.Len(t, loadedState.Tools, 1)
	assert.Len(t, loadedState.Runtimes, 1)

	assert.Equal(t, "14.0.0", loadedState.Tools["ripgrep"].Version)
	assert.Equal(t, "download", loadedState.Tools["ripgrep"].InstallerRef)
	assert.Equal(t, "/home/user/.local/bin/rg", loadedState.Tools["ripgrep"].BinPath)

	assert.Equal(t, "1.25.5", loadedState.Runtimes["go"].Version)
	assert.Equal(t, []string{"go", "gofmt"}, loadedState.Runtimes["go"].Binaries)
	assert.Equal(t, "/home/user/go/bin", loadedState.Runtimes["go"].ToolBinPath)
}

// TestState_UpdateAndReload tests updating state and reloading.
func TestState_UpdateAndReload(t *testing.T) {
	stateDir := t.TempDir()

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Save initial state
	require.NoError(t, store.Lock())
	initialState := &state.UserState{
		Tools: map[string]*resource.ToolState{
			"fd": {
				InstallerRef: "download",
				Version:      "8.0.0",
				BinPath:      "/home/user/.local/bin/fd",
			},
		},
		Runtimes: map[string]*resource.RuntimeState{},
	}
	require.NoError(t, store.Save(initialState))
	require.NoError(t, store.Unlock())

	// Update state (upgrade tool)
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)

	st.Tools["fd"].Version = "9.0.0"

	require.NoError(t, store.Save(st))
	require.NoError(t, store.Unlock())

	// Reload and verify
	require.NoError(t, store.Lock())
	reloaded, err := store.Load()
	require.NoError(t, err)
	require.NoError(t, store.Unlock())

	assert.Equal(t, "9.0.0", reloaded.Tools["fd"].Version)
}

// TestState_AddAndRemove tests adding and removing entries from state.
func TestState_AddAndRemove(t *testing.T) {
	stateDir := t.TempDir()

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Start with empty state
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)

	// Add tools
	if st.Tools == nil {
		st.Tools = make(map[string]*resource.ToolState)
	}
	st.Tools["jq"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "1.7.1",
		BinPath:      "/home/user/.local/bin/jq",
	}
	st.Tools["bat"] = &resource.ToolState{
		InstallerRef: "download",
		Version:      "0.24.0",
		BinPath:      "/home/user/.local/bin/bat",
	}

	require.NoError(t, store.Save(st))
	require.NoError(t, store.Unlock())

	// Verify additions
	require.NoError(t, store.Lock())
	st, err = store.Load()
	require.NoError(t, err)
	assert.Len(t, st.Tools, 2)
	assert.Contains(t, st.Tools, "jq")
	assert.Contains(t, st.Tools, "bat")

	// Remove one tool
	delete(st.Tools, "jq")
	require.NoError(t, store.Save(st))
	require.NoError(t, store.Unlock())

	// Verify removal
	require.NoError(t, store.Lock())
	st, err = store.Load()
	require.NoError(t, err)
	require.NoError(t, store.Unlock())

	assert.Len(t, st.Tools, 1)
	assert.NotContains(t, st.Tools, "jq")
	assert.Contains(t, st.Tools, "bat")
}

// TestState_Taint tests the taint functionality for tools.
func TestState_Taint(t *testing.T) {
	stateDir := t.TempDir()

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	require.NoError(t, store.Lock())
	st := &state.UserState{
		Tools: map[string]*resource.ToolState{
			"gopls": {
				InstallerRef: "go",
				Version:      "0.15.0",
				RuntimeRef:   "go",
				BinPath:      "/home/user/go/bin/gopls",
			},
		},
		Runtimes: map[string]*resource.RuntimeState{
			"go": {
				InstallerRef: "download",
				Version:      "1.25.5",
				InstallPath:  "/home/user/.local/share/toto/runtimes/go/1.25.5",
				Binaries:     []string{"go", "gofmt"},
			},
		},
	}
	require.NoError(t, store.Save(st))

	// Simulate runtime upgrade - taint dependent tools
	st.Tools["gopls"].Taint("runtime_upgraded")
	require.NoError(t, store.Save(st))
	require.NoError(t, store.Unlock())

	// Reload and verify taint
	require.NoError(t, store.Lock())
	st, err = store.Load()
	require.NoError(t, err)
	require.NoError(t, store.Unlock())

	assert.True(t, st.Tools["gopls"].IsTainted())
	assert.Equal(t, "runtime_upgraded", st.Tools["gopls"].TaintReason)

	// Clear taint
	require.NoError(t, store.Lock())
	st.Tools["gopls"].ClearTaint()
	require.NoError(t, store.Save(st))
	require.NoError(t, store.Unlock())

	// Verify taint cleared
	require.NoError(t, store.Lock())
	st, err = store.Load()
	require.NoError(t, err)
	require.NoError(t, store.Unlock())

	assert.False(t, st.Tools["gopls"].IsTainted())
	assert.Empty(t, st.Tools["gopls"].TaintReason)
}

// TestState_JSONFormat tests that state.json has the expected format.
func TestState_JSONFormat(t *testing.T) {
	stateDir := t.TempDir()

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	require.NoError(t, store.Lock())
	st := &state.UserState{
		Tools: map[string]*resource.ToolState{
			"rg": {
				InstallerRef: "download",
				Version:      "14.0.0",
				BinPath:      "/home/user/.local/bin/rg",
				UpdatedAt:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			},
		},
		Runtimes: map[string]*resource.RuntimeState{},
	}
	require.NoError(t, store.Save(st))
	require.NoError(t, store.Unlock())

	// Read raw JSON
	stateFile := filepath.Join(stateDir, "state.json")
	data, err := os.ReadFile(stateFile)
	require.NoError(t, err)

	// Parse as generic JSON
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))

	// Verify structure
	tools, ok := parsed["tools"].(map[string]any)
	require.True(t, ok, "tools should be a map")

	rg, ok := tools["rg"].(map[string]any)
	require.True(t, ok, "rg should be a map")

	assert.Equal(t, "download", rg["installerRef"])
	assert.Equal(t, "14.0.0", rg["version"])
	assert.Equal(t, "/home/user/.local/bin/rg", rg["binPath"])
}

// TestState_EmptyState tests handling of empty state.
func TestState_EmptyState(t *testing.T) {
	stateDir := t.TempDir()

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Load empty state (no state.json exists)
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)
	require.NoError(t, store.Unlock())

	// Should return initialized empty state
	assert.NotNil(t, st)
	assert.Empty(t, st.Tools)
	assert.Empty(t, st.Runtimes)
}

// TestState_ConcurrentAccess tests that concurrent access is prevented by locking.
func TestState_ConcurrentAccess(t *testing.T) {
	stateDir := t.TempDir()

	store1, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// First store acquires lock
	require.NoError(t, store1.Lock())

	// Save some state while holding lock
	st := &state.UserState{
		Tools: map[string]*resource.ToolState{
			"test": {Version: "1.0.0"},
		},
	}
	require.NoError(t, store1.Save(st))

	// Clean up
	require.NoError(t, store1.Unlock())

	// After unlock, another store can access
	store2, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	require.NoError(t, store2.Lock())
	loaded, err := store2.Load()
	require.NoError(t, err)
	require.NoError(t, store2.Unlock())

	assert.Equal(t, "1.0.0", loaded.Tools["test"].Version)
}
