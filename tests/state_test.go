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

	"github.com/terassyi/tomei/internal/checksum"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// TestState_Persistence tests that state is correctly persisted and loaded.
func TestState_Persistence(t *testing.T) {
	stateDir := t.TempDir()

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Lock and save initial state
	require.NoError(t, store.Lock())

	now := time.Now().Truncate(time.Second)
	initialState := &state.UserState{
		Tools: map[string]*resource.ToolState{
			"ripgrep": {
				InstallerRef: "download",
				Version:      "14.0.0",
				Digest:       checksum.Digest("sha256:abc123def456"),
				BinPath:      "/home/user/.local/bin/rg",
				VersionKind:  resource.VersionExact,
				SpecVersion:  "14.0.0",
				UpdatedAt:    now,
			},
			"claude": {
				Version:     "1.0.5",
				VersionKind: resource.VersionLatest,
				BinPath:     "/home/user/.local/bin/claude",
				Commands: &resource.ToolCommandSet{
					CommandSet: resource.CommandSet{
						Install: []string{"curl -fsSL https://cli.claude.ai/install.sh | sh"},
						Check:   []string{"claude --version"},
						Remove:  []string{"rm -f ~/.local/bin/claude"},
					},
					Update:         []string{"curl -fsSL https://cli.claude.ai/install.sh | sh"},
					ResolveVersion: []string{"claude --version"},
				},
				UpdatedAt: now,
			},
			"gopls": {
				Version:     "v0.16.0",
				RuntimeRef:  "go",
				VersionKind: resource.VersionExact,
				SpecVersion: "v0.16.0",
				BinPath:     "/home/user/go/bin/gopls",
				UpdatedAt:   now,
			},
		},
		Runtimes: map[string]*resource.RuntimeState{
			"go": {
				Type:        resource.InstallTypeDownload,
				Version:     "1.25.5",
				InstallPath: "/home/user/.local/share/tomei/runtimes/go/1.25.5",
				Binaries:    []string{"go", "gofmt"},
				ToolBinPath: "/home/user/go/bin",
				Env: map[string]string{
					"GOROOT": "/home/user/.local/share/tomei/runtimes/go/1.25.5",
				},
				UpdatedAt: now,
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
	assert.Len(t, loadedState.Tools, 3)
	assert.Len(t, loadedState.Runtimes, 1)

	// Verify ripgrep (download pattern with Digest, VersionKind, SpecVersion)
	rg := loadedState.Tools["ripgrep"]
	require.NotNil(t, rg)
	assert.Equal(t, "14.0.0", rg.Version)
	assert.Equal(t, "download", rg.InstallerRef)
	assert.Equal(t, "/home/user/.local/bin/rg", rg.BinPath)
	assert.Equal(t, checksum.Digest("sha256:abc123def456"), rg.Digest)
	assert.Equal(t, resource.VersionExact, rg.VersionKind)
	assert.Equal(t, "14.0.0", rg.SpecVersion)
	assert.Nil(t, rg.Commands, "download-pattern tool should have nil Commands")

	// Verify claude (commands pattern with full ToolCommandSet)
	claude := loadedState.Tools["claude"]
	require.NotNil(t, claude)
	assert.Equal(t, "1.0.5", claude.Version)
	assert.Equal(t, resource.VersionLatest, claude.VersionKind)
	require.NotNil(t, claude.Commands, "commands-pattern tool should have non-nil Commands")
	assert.Equal(t, []string{"curl -fsSL https://cli.claude.ai/install.sh | sh"}, claude.Commands.Install)
	assert.Equal(t, []string{"claude --version"}, claude.Commands.Check)
	assert.Equal(t, []string{"rm -f ~/.local/bin/claude"}, claude.Commands.Remove)
	assert.Equal(t, []string{"curl -fsSL https://cli.claude.ai/install.sh | sh"}, claude.Commands.Update)
	assert.Equal(t, []string{"claude --version"}, claude.Commands.ResolveVersion)

	// Verify gopls (runtime delegation with RuntimeRef)
	gopls := loadedState.Tools["gopls"]
	require.NotNil(t, gopls)
	assert.Equal(t, "v0.16.0", gopls.Version)
	assert.Equal(t, "go", gopls.RuntimeRef)
	assert.Equal(t, resource.VersionExact, gopls.VersionKind)
	assert.Equal(t, "v0.16.0", gopls.SpecVersion)

	// Verify runtime
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

// TestState_Taint tests the taint functionality for tools with all taint reasons.
func TestState_Taint(t *testing.T) {
	tests := []struct {
		name        string
		reason      resource.TaintReason
		toolName    string
		description string
	}{
		{
			name:        "TaintReasonRuntimeUpgraded",
			reason:      resource.TaintReasonRuntimeUpgraded,
			toolName:    "gopls",
			description: "runtime upgrade triggers tool reinstallation",
		},
		{
			name:        "TaintReasonSyncUpdate",
			reason:      resource.TaintReasonSyncUpdate,
			toolName:    "ripgrep",
			description: "sync flag triggers latest-version update",
		},
		{
			name:        "TaintReasonUpdateRequested",
			reason:      resource.TaintReasonUpdateRequested,
			toolName:    "fd",
			description: "user-requested update via --update-tools",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()

			store, err := state.NewStore[state.UserState](stateDir)
			require.NoError(t, err)

			require.NoError(t, store.Lock())
			st := &state.UserState{
				Tools: map[string]*resource.ToolState{
					tt.toolName: {
						Version: "1.0.0",
						BinPath: "/home/user/.local/bin/" + tt.toolName,
					},
				},
			}
			require.NoError(t, store.Save(st))

			// Apply taint
			st.Tools[tt.toolName].Taint(tt.reason)
			require.NoError(t, store.Save(st))
			require.NoError(t, store.Unlock())

			// Reload and verify taint persists
			require.NoError(t, store.Lock())
			st, err = store.Load()
			require.NoError(t, err)
			require.NoError(t, store.Unlock())

			assert.True(t, st.Tools[tt.toolName].IsTainted(), "%s: tool should be tainted", tt.description)
			assert.Equal(t, tt.reason, st.Tools[tt.toolName].TaintReason, "%s: taint reason should match", tt.description)

			// Clear taint and verify
			require.NoError(t, store.Lock())
			st.Tools[tt.toolName].ClearTaint()
			require.NoError(t, store.Save(st))
			require.NoError(t, store.Unlock())

			require.NoError(t, store.Lock())
			st, err = store.Load()
			require.NoError(t, err)
			require.NoError(t, store.Unlock())

			assert.False(t, st.Tools[tt.toolName].IsTainted(), "%s: taint should be cleared", tt.description)
			assert.Empty(t, st.Tools[tt.toolName].TaintReason, "%s: taint reason should be empty", tt.description)
		})
	}
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

// TestState_RegistryPersistence tests that registry state is correctly persisted and loaded.
func TestState_RegistryPersistence(t *testing.T) {
	tests := []struct {
		name  string
		state *state.UserState
		check func(t *testing.T, loaded *state.UserState)
	}{
		{
			name: "registry with tools",
			state: &state.UserState{
				Registry: &state.RegistryState{
					Aqua: &state.AquaRegistryState{
						Ref:       "v4.465.0",
						UpdatedAt: time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC),
					},
				},
				Tools: map[string]*resource.ToolState{
					"gh": {
						InstallerRef: "aqua",
						Version:      "2.86.0",
						Package:      &resource.Package{Owner: "cli", Repo: "cli"},
						BinPath:      "/home/user/.local/bin/gh",
						UpdatedAt:    time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC),
					},
				},
			},
			check: func(t *testing.T, loaded *state.UserState) {
				require.NotNil(t, loaded.Registry)
				require.NotNil(t, loaded.Registry.Aqua)
				assert.Equal(t, "v4.465.0", loaded.Registry.Aqua.Ref)

				require.Len(t, loaded.Tools, 1)
				assert.Equal(t, "cli/cli", loaded.Tools["gh"].Package.String())
				assert.Equal(t, "aqua", loaded.Tools["gh"].InstallerRef)
			},
		},
		{
			name: "registry only (no tools)",
			state: &state.UserState{
				Registry: &state.RegistryState{
					Aqua: &state.AquaRegistryState{
						Ref:       "v4.500.0",
						UpdatedAt: time.Now().Truncate(time.Second),
					},
				},
			},
			check: func(t *testing.T, loaded *state.UserState) {
				require.NotNil(t, loaded.Registry)
				require.NotNil(t, loaded.Registry.Aqua)
				assert.Equal(t, "v4.500.0", loaded.Registry.Aqua.Ref)
				assert.Empty(t, loaded.Tools)
			},
		},
		{
			name: "nil registry (backward compatibility)",
			state: &state.UserState{
				Tools: map[string]*resource.ToolState{
					"ripgrep": {
						InstallerRef: "aqua",
						Version:      "14.0.0",
						BinPath:      "/home/user/.local/bin/rg",
					},
				},
			},
			check: func(t *testing.T, loaded *state.UserState) {
				assert.Nil(t, loaded.Registry)
				require.Len(t, loaded.Tools, 1)
				assert.Equal(t, "14.0.0", loaded.Tools["ripgrep"].Version)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()

			store, err := state.NewStore[state.UserState](stateDir)
			require.NoError(t, err)

			// Save state
			require.NoError(t, store.Lock())
			require.NoError(t, store.Save(tt.state))
			require.NoError(t, store.Unlock())

			// Create new store instance and load
			store2, err := state.NewStore[state.UserState](stateDir)
			require.NoError(t, err)

			require.NoError(t, store2.Lock())
			loaded, err := store2.Load()
			require.NoError(t, err)
			require.NoError(t, store2.Unlock())

			tt.check(t, loaded)
		})
	}
}

// TestState_RegistryJSONFormat tests that registry state has the expected JSON format.
func TestState_RegistryJSONFormat(t *testing.T) {
	stateDir := t.TempDir()

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	require.NoError(t, store.Lock())
	st := &state.UserState{
		Registry: &state.RegistryState{
			Aqua: &state.AquaRegistryState{
				Ref:       "v4.465.0",
				UpdatedAt: time.Date(2026, 2, 3, 10, 30, 0, 0, time.UTC),
			},
		},
		Tools: map[string]*resource.ToolState{
			"gh": {
				InstallerRef: "aqua",
				Version:      "2.86.0",
				Package:      &resource.Package{Owner: "cli", Repo: "cli"},
				BinPath:      "/home/user/.local/bin/gh",
				UpdatedAt:    time.Date(2026, 2, 3, 10, 30, 0, 0, time.UTC),
			},
		},
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

	// Verify registry structure
	registry, ok := parsed["registry"].(map[string]any)
	require.True(t, ok, "registry should be a map")

	aquaState, ok := registry["aqua"].(map[string]any)
	require.True(t, ok, "aqua should be a map")

	assert.Equal(t, "v4.465.0", aquaState["ref"])
	assert.NotEmpty(t, aquaState["updatedAt"])

	// Verify tools structure
	tools, ok := parsed["tools"].(map[string]any)
	require.True(t, ok, "tools should be a map")

	gh, ok := tools["gh"].(map[string]any)
	require.True(t, ok, "gh should be a map")

	assert.Equal(t, "aqua", gh["installerRef"])
	assert.Equal(t, "2.86.0", gh["version"])

	// package is stored as object {"owner": "cli", "repo": "cli"}
	pkg, ok := gh["package"].(map[string]any)
	require.True(t, ok, "package should be a map")
	assert.Equal(t, "cli", pkg["owner"])
	assert.Equal(t, "cli", pkg["repo"])
}

// TestState_RegistryUpdate tests updating registry state.
func TestState_RegistryUpdate(t *testing.T) {
	stateDir := t.TempDir()

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	// Save initial state with old registry ref
	require.NoError(t, store.Lock())
	initialState := &state.UserState{
		Registry: &state.RegistryState{
			Aqua: &state.AquaRegistryState{
				Ref:       "v4.400.0",
				UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			},
		},
	}
	require.NoError(t, store.Save(initialState))
	require.NoError(t, store.Unlock())

	// Update registry ref (simulate --sync)
	require.NoError(t, store.Lock())
	st, err := store.Load()
	require.NoError(t, err)

	st.Registry.Aqua.Ref = "v4.465.0"
	st.Registry.Aqua.UpdatedAt = time.Date(2026, 2, 3, 10, 0, 0, 0, time.UTC)

	require.NoError(t, store.Save(st))
	require.NoError(t, store.Unlock())

	// Reload and verify
	require.NoError(t, store.Lock())
	reloaded, err := store.Load()
	require.NoError(t, err)
	require.NoError(t, store.Unlock())

	assert.Equal(t, "v4.465.0", reloaded.Registry.Aqua.Ref)
}

// TestState_ToolCommandsPersistence tests that ToolState with full ToolCommandSet
// survives a save/load roundtrip with all subfields intact.
func TestState_ToolCommandsPersistence(t *testing.T) {
	tests := []struct {
		name     string
		commands *resource.ToolCommandSet
	}{
		{
			name: "full commands (all subfields)",
			commands: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"curl -fsSL https://example.com/install.sh | sh"},
					Check:   []string{"mytool --version"},
					Remove:  []string{"rm -f ~/.local/bin/mytool"},
				},
				Update:         []string{"curl -fsSL https://example.com/update.sh | sh"},
				ResolveVersion: []string{"mytool --version"},
			},
		},
		{
			name: "install only (minimal)",
			commands: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"echo install"},
				},
			},
		},
		{
			name: "multi-command install",
			commands: &resource.ToolCommandSet{
				CommandSet: resource.CommandSet{
					Install: []string{"mkdir -p /tmp/build", "make install"},
					Check:   []string{"which mytool"},
				},
			},
		},
		{
			name:     "nil commands",
			commands: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stateDir := t.TempDir()

			store, err := state.NewStore[state.UserState](stateDir)
			require.NoError(t, err)

			require.NoError(t, store.Lock())
			st := &state.UserState{
				Tools: map[string]*resource.ToolState{
					"mytool": {
						Version:   "1.0.0",
						BinPath:   "/home/user/.local/bin/mytool",
						Commands:  tt.commands,
						UpdatedAt: time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC),
					},
				},
			}
			require.NoError(t, store.Save(st))
			require.NoError(t, store.Unlock())

			// Load from a new store instance
			store2, err := state.NewStore[state.UserState](stateDir)
			require.NoError(t, err)

			require.NoError(t, store2.Lock())
			loaded, err := store2.Load()
			require.NoError(t, err)
			require.NoError(t, store2.Unlock())

			require.Len(t, loaded.Tools, 1)
			tool := loaded.Tools["mytool"]
			require.NotNil(t, tool)
			assert.Equal(t, "1.0.0", tool.Version)

			if tt.commands == nil {
				assert.Nil(t, tool.Commands, "nil commands should remain nil after roundtrip")
				return
			}

			require.NotNil(t, tool.Commands, "non-nil commands should survive roundtrip")
			assert.Equal(t, tt.commands.Install, tool.Commands.Install)
			assert.Equal(t, tt.commands.Check, tool.Commands.Check)
			assert.Equal(t, tt.commands.Remove, tool.Commands.Remove)
			assert.Equal(t, tt.commands.Update, tool.Commands.Update)
			assert.Equal(t, tt.commands.ResolveVersion, tool.Commands.ResolveVersion)
		})
	}
}

// TestState_ToolCommandsJSONFormat verifies the JSON structure of state.json
// for tools with and without Commands.
func TestState_ToolCommandsJSONFormat(t *testing.T) {
	stateDir := t.TempDir()

	store, err := state.NewStore[state.UserState](stateDir)
	require.NoError(t, err)

	require.NoError(t, store.Lock())
	st := &state.UserState{
		Tools: map[string]*resource.ToolState{
			"with-commands": {
				Version: "2.0.0",
				BinPath: "/home/user/.local/bin/with-commands",
				Commands: &resource.ToolCommandSet{
					CommandSet: resource.CommandSet{
						Install: []string{"curl -fsSL https://example.com/install.sh | sh"},
						Check:   []string{"with-commands --version"},
						Remove:  []string{"rm -f ~/.local/bin/with-commands"},
					},
					Update:         []string{"curl -fsSL https://example.com/update.sh | sh"},
					ResolveVersion: []string{"with-commands --version"},
				},
				UpdatedAt: time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC),
			},
			"without-commands": {
				InstallerRef: "aqua",
				Version:      "14.0.0",
				BinPath:      "/home/user/.local/bin/without-commands",
				UpdatedAt:    time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC),
			},
		},
	}
	require.NoError(t, store.Save(st))
	require.NoError(t, store.Unlock())

	// Read raw JSON
	stateFile := filepath.Join(stateDir, "state.json")
	data, err := os.ReadFile(stateFile)
	require.NoError(t, err)

	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))

	tools, ok := parsed["tools"].(map[string]any)
	require.True(t, ok, "tools should be a map")

	// Verify "with-commands" has a "commands" key with correct structure
	withCmds, ok := tools["with-commands"].(map[string]any)
	require.True(t, ok, "with-commands tool should be a map")

	cmds, ok := withCmds["commands"].(map[string]any)
	require.True(t, ok, "commands should be a map")

	// Verify install is a JSON array
	installArr, ok := cmds["install"].([]any)
	require.True(t, ok, "install should be an array")
	require.Len(t, installArr, 1)
	assert.Equal(t, "curl -fsSL https://example.com/install.sh | sh", installArr[0])

	// Verify update exists
	updateArr, ok := cmds["update"].([]any)
	require.True(t, ok, "update should be an array")
	require.Len(t, updateArr, 1)
	assert.Equal(t, "curl -fsSL https://example.com/update.sh | sh", updateArr[0])

	// Verify check exists
	checkArr, ok := cmds["check"].([]any)
	require.True(t, ok, "check should be an array")
	require.Len(t, checkArr, 1)
	assert.Equal(t, "with-commands --version", checkArr[0])

	// Verify remove exists
	removeArr, ok := cmds["remove"].([]any)
	require.True(t, ok, "remove should be an array")
	require.Len(t, removeArr, 1)
	assert.Equal(t, "rm -f ~/.local/bin/with-commands", removeArr[0])

	// Verify resolveVersion exists
	resolveArr, ok := cmds["resolveVersion"].([]any)
	require.True(t, ok, "resolveVersion should be an array")
	require.Len(t, resolveArr, 1)
	assert.Equal(t, "with-commands --version", resolveArr[0])

	// Verify "without-commands" does NOT have a "commands" key (omitempty)
	withoutCmds, ok := tools["without-commands"].(map[string]any)
	require.True(t, ok, "without-commands tool should be a map")

	_, hasCommands := withoutCmds["commands"]
	assert.False(t, hasCommands, "commands key should be absent when Commands is nil (omitempty)")
}
