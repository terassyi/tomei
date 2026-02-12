package state

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/terassyi/tomei/internal/resource"
)

func TestStore_LockUnlock(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore[UserState](dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// Lock should succeed
	if err := store.Lock(); err != nil {
		t.Fatalf("failed to lock: %v", err)
	}

	// Lock file should contain PID
	data, err := os.ReadFile(store.LockPath())
	if err != nil {
		t.Fatalf("failed to read lock file: %v", err)
	}
	if len(data) == 0 {
		t.Error("lock file should contain PID")
	}

	// Unlock should succeed
	if err := store.Unlock(); err != nil {
		t.Fatalf("failed to unlock: %v", err)
	}
}

func TestStore_LoadSave(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore[UserState](dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	if err := store.Lock(); err != nil {
		t.Fatalf("failed to lock: %v", err)
	}
	defer func() { _ = store.Unlock() }()

	// Load should return empty state for new file
	state, err := store.Load()
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}
	if state == nil {
		t.Fatal("state should not be nil")
	}

	// Initialize and save state
	state.Version = Version
	state.Runtimes = map[string]*resource.RuntimeState{
		"go": {
			Type:        resource.InstallTypeDownload,
			Version:     "1.25.1",
			InstallPath: "/home/user/.local/share/tomei/runtimes/go/1.25.1",
			Binaries:    []string{"go", "gofmt"},
			ToolBinPath: "/home/user/go/bin",
			UpdatedAt:   time.Now(),
		},
	}
	state.Tools = map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "aqua",
			Version:      "14.0.0",
			InstallPath:  "/home/user/.local/share/tomei/tools/ripgrep/14.0.0",
			BinPath:      "/home/user/.local/bin/rg",
			UpdatedAt:    time.Now(),
		},
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(store.StatePath()); os.IsNotExist(err) {
		t.Error("state file should exist")
	}

	// Load and verify
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("failed to load saved state: %v", err)
	}
	if loaded.Version != Version {
		t.Errorf("expected version %q, got %q", Version, loaded.Version)
	}
	if len(loaded.Runtimes) != 1 {
		t.Errorf("expected 1 runtime, got %d", len(loaded.Runtimes))
	}
	if len(loaded.Tools) != 1 {
		t.Errorf("expected 1 tool, got %d", len(loaded.Tools))
	}
	if loaded.Runtimes["go"].Version != "1.25.1" {
		t.Errorf("expected go version 1.25.1, got %s", loaded.Runtimes["go"].Version)
	}
	if loaded.Tools["ripgrep"].Version != "14.0.0" {
		t.Errorf("expected ripgrep version 14.0.0, got %s", loaded.Tools["ripgrep"].Version)
	}
}

func TestStore_LoadReadOnly(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, store *Store[UserState])
		wantErr bool
		check   func(t *testing.T, st *UserState)
	}{
		{
			name:  "returns empty state when file does not exist",
			setup: func(t *testing.T, store *Store[UserState]) {},
			check: func(t *testing.T, st *UserState) {
				if st == nil {
					t.Fatal("state should not be nil")
				}
			},
		},
		{
			name: "reads existing state without lock",
			setup: func(t *testing.T, store *Store[UserState]) {
				if err := store.Lock(); err != nil {
					t.Fatalf("failed to lock: %v", err)
				}
				st := &UserState{
					Version: Version,
					Tools: map[string]*resource.ToolState{
						"gh": {Version: "2.86.0", UpdatedAt: time.Now()},
					},
				}
				if err := store.Save(st); err != nil {
					t.Fatalf("failed to save: %v", err)
				}
				if err := store.Unlock(); err != nil {
					t.Fatalf("failed to unlock: %v", err)
				}
			},
			check: func(t *testing.T, st *UserState) {
				if st.Version != Version {
					t.Errorf("expected version %q, got %q", Version, st.Version)
				}
				if len(st.Tools) != 1 {
					t.Errorf("expected 1 tool, got %d", len(st.Tools))
				}
			},
		},
		{
			name: "returns error on corrupted JSON",
			setup: func(t *testing.T, store *Store[UserState]) {
				if err := os.WriteFile(store.StatePath(), []byte("invalid{"), 0644); err != nil {
					t.Fatalf("failed to write: %v", err)
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := NewStore[UserState](dir)
			if err != nil {
				t.Fatalf("failed to create store: %v", err)
			}

			tt.setup(t, store)

			st, err := store.LoadReadOnly()
			if tt.wantErr {
				if err == nil {
					t.Error("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, st)
			}
		})
	}
}

func TestStore_LoadWithoutLock(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore[UserState](dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	_, err = store.Load()
	if err == nil {
		t.Error("Load without lock should fail")
	}
}

func TestStore_SaveWithoutLock(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore[UserState](dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	state := NewUserState()
	err = store.Save(state)
	if err == nil {
		t.Error("Save without lock should fail")
	}
}

func TestStore_AtomicWrite(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore[UserState](dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	if err := store.Lock(); err != nil {
		t.Fatalf("failed to lock: %v", err)
	}
	defer func() { _ = store.Unlock() }()

	state := NewUserState()
	state.Tools = map[string]*resource.ToolState{
		"test": {
			InstallerRef: "aqua",
			Version:      "1.0.0",
			UpdatedAt:    time.Now(),
		},
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	// Temp file should not exist after successful save
	tmpPath := store.StatePath() + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful save")
	}
}

func TestStore_SystemState(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore[SystemState](dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	if err := store.Lock(); err != nil {
		t.Fatalf("failed to lock: %v", err)
	}
	defer func() { _ = store.Unlock() }()

	state := NewSystemState()
	state.SystemInstallers = map[string]*resource.SystemInstallerState{
		"apt": {
			Version:   "1",
			UpdatedAt: time.Now(),
		},
	}
	state.SystemPackages = map[string]*resource.SystemPackageSetState{
		"cli-tools": {
			InstallerRef:      "apt",
			Packages:          []string{"jq", "curl"},
			InstalledVersions: map[string]string{"jq": "1.6", "curl": "7.81.0"},
			UpdatedAt:         time.Now(),
		},
	}

	if err := store.Save(state); err != nil {
		t.Fatalf("failed to save: %v", err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	if len(loaded.SystemInstallers) != 1 {
		t.Errorf("expected 1 installer, got %d", len(loaded.SystemInstallers))
	}
	if len(loaded.SystemPackages) != 1 {
		t.Errorf("expected 1 package set, got %d", len(loaded.SystemPackages))
	}
}

func TestNewStore_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "path")
	_, err := NewStore[UserState](dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("directory should be created")
	}
}

func TestStore_LockBehavior(t *testing.T) {
	tests := []struct {
		name string
		test func(t *testing.T, dir string)
	}{
		{
			name: "lock is idempotent",
			test: func(t *testing.T, dir string) {
				store, err := NewStore[UserState](dir)
				if err != nil {
					t.Fatalf("failed to create store: %v", err)
				}

				// First lock should succeed
				if err := store.Lock(); err != nil {
					t.Fatalf("first lock failed: %v", err)
				}

				// Second lock should be idempotent (no-op, no error)
				if err := store.Lock(); err != nil {
					t.Fatalf("second lock should be idempotent: %v", err)
				}

				// Unlock should succeed
				if err := store.Unlock(); err != nil {
					t.Fatalf("unlock failed: %v", err)
				}

				// Second unlock should be idempotent (no-op, no error)
				if err := store.Unlock(); err != nil {
					t.Fatalf("second unlock should be idempotent: %v", err)
				}
			},
		},
		{
			name: "lock contention",
			test: func(t *testing.T, dir string) {
				// First store acquires lock
				store1, err := NewStore[UserState](dir)
				if err != nil {
					t.Fatalf("failed to create store1: %v", err)
				}
				if err := store1.Lock(); err != nil {
					t.Fatalf("store1 lock failed: %v", err)
				}
				defer func() { _ = store1.Unlock() }()

				// Second store should fail to acquire lock
				store2, err := NewStore[UserState](dir)
				if err != nil {
					t.Fatalf("failed to create store2: %v", err)
				}

				err = store2.Lock()
				if err == nil {
					t.Error("store2 lock should fail when store1 holds lock")
					_ = store2.Unlock()
				}
			},
		},
		{
			name: "lock file contains PID",
			test: func(t *testing.T, dir string) {
				store, err := NewStore[UserState](dir)
				if err != nil {
					t.Fatalf("failed to create store: %v", err)
				}

				if err := store.Lock(); err != nil {
					t.Fatalf("lock failed: %v", err)
				}
				defer func() { _ = store.Unlock() }()

				// Read lock file and verify PID is not empty
				data, err := os.ReadFile(store.LockPath())
				if err != nil {
					t.Fatalf("failed to read lock file: %v", err)
				}

				if len(data) == 0 {
					t.Error("lock file should contain PID")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.test(t, dir)
		})
	}
}

func TestStore_PathAccessors(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore[UserState](dir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{
			name:     "state path",
			got:      store.StatePath(),
			expected: filepath.Join(dir, "state.json"),
		},
		{
			name:     "lock path",
			got:      store.LockPath(),
			expected: filepath.Join(dir, "state.lock"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.got)
			}
		})
	}
}

func TestStore_LoadErrors(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(store *Store[UserState]) error
		wantError bool
	}{
		{
			name: "corrupted JSON",
			setup: func(store *Store[UserState]) error {
				return os.WriteFile(store.StatePath(), []byte("invalid json{"), 0644)
			},
			wantError: true,
		},
		{
			name: "empty file",
			setup: func(store *Store[UserState]) error {
				return os.WriteFile(store.StatePath(), []byte(""), 0644)
			},
			wantError: true,
		},
		{
			name: "valid JSON",
			setup: func(store *Store[UserState]) error {
				return os.WriteFile(store.StatePath(), []byte(`{"version":"1"}`), 0644)
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := NewStore[UserState](dir)
			if err != nil {
				t.Fatalf("failed to create store: %v", err)
			}

			if err := tt.setup(store); err != nil {
				t.Fatalf("setup failed: %v", err)
			}

			if err := store.Lock(); err != nil {
				t.Fatalf("lock failed: %v", err)
			}
			defer func() { _ = store.Unlock() }()

			_, err = store.Load()
			if tt.wantError && err == nil {
				t.Error("expected error but got none")
			}
			if !tt.wantError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestStore_SaveAndLoadPreservesAllFields(t *testing.T) {
	now := time.Now().Truncate(time.Second) // Truncate for JSON round-trip

	tests := []struct {
		name  string
		state *UserState
		check func(t *testing.T, loaded *UserState)
	}{
		{
			name: "all fields",
			state: &UserState{
				Version: Version,
				Registry: &RegistryState{
					Aqua: &AquaRegistryState{
						Ref:       "v4.465.0",
						UpdatedAt: now,
					},
				},
				Installers: map[string]*resource.InstallerState{
					"aqua": {
						Version:   "1.0.0",
						UpdatedAt: now,
					},
				},
				Runtimes: map[string]*resource.RuntimeState{
					"go": {
						Type:        resource.InstallTypeDownload,
						Version:     "1.25.5",
						InstallPath: "/home/user/.local/share/tomei/runtimes/go/1.25.5",
						Binaries:    []string{"go", "gofmt"},
						ToolBinPath: "/home/user/go/bin",
						UpdatedAt:   now,
					},
				},
				Tools: map[string]*resource.ToolState{
					"gh": {
						InstallerRef: "aqua",
						Version:      "2.86.0",
						Package:      &resource.Package{Owner: "cli", Repo: "cli"},
						InstallPath:  "/home/user/.local/share/tomei/tools/gh/2.86.0",
						BinPath:      "/home/user/.local/bin/gh",
						Digest:       "sha256:abc123",
						UpdatedAt:    now,
					},
				},
			},
			check: func(t *testing.T, loaded *UserState) {
				if loaded.Version != Version {
					t.Errorf("version mismatch: %s", loaded.Version)
				}
				if loaded.Registry == nil || loaded.Registry.Aqua == nil {
					t.Fatal("registry should be preserved")
				}
				if loaded.Registry.Aqua.Ref != "v4.465.0" {
					t.Errorf("registry ref mismatch: %s", loaded.Registry.Aqua.Ref)
				}
				if len(loaded.Installers) != 1 || loaded.Installers["aqua"].Version != "1.0.0" {
					t.Error("installers mismatch")
				}
				if len(loaded.Runtimes) != 1 || loaded.Runtimes["go"].Version != "1.25.5" {
					t.Error("runtimes mismatch")
				}
				if len(loaded.Runtimes["go"].Binaries) != 2 {
					t.Errorf("runtime binaries mismatch: %v", loaded.Runtimes["go"].Binaries)
				}
				if len(loaded.Tools) != 1 || loaded.Tools["gh"].Digest != "sha256:abc123" {
					t.Error("tools mismatch")
				}
			},
		},
		{
			name: "empty state",
			state: &UserState{
				Version: Version,
			},
			check: func(t *testing.T, loaded *UserState) {
				if loaded.Version != Version {
					t.Errorf("version mismatch: %s", loaded.Version)
				}
				if loaded.Registry != nil {
					t.Error("registry should be nil")
				}
				if loaded.Installers != nil {
					t.Error("installers should be nil")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := NewStore[UserState](dir)
			if err != nil {
				t.Fatalf("failed to create store: %v", err)
			}

			if err := store.Lock(); err != nil {
				t.Fatalf("lock failed: %v", err)
			}
			defer func() { _ = store.Unlock() }()

			if err := store.Save(tt.state); err != nil {
				t.Fatalf("save failed: %v", err)
			}

			loaded, err := store.Load()
			if err != nil {
				t.Fatalf("load failed: %v", err)
			}

			tt.check(t, loaded)
		})
	}
}

func TestStore_RegistryState(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name  string
		state *UserState
		check func(t *testing.T, loaded *UserState)
	}{
		{
			name: "registry only",
			state: &UserState{
				Version: Version,
				Registry: &RegistryState{
					Aqua: &AquaRegistryState{
						Ref:       "v4.465.0",
						UpdatedAt: now,
					},
				},
			},
			check: func(t *testing.T, loaded *UserState) {
				if loaded.Registry == nil {
					t.Fatal("registry should not be nil")
				}
				if loaded.Registry.Aqua == nil {
					t.Fatal("registry.aqua should not be nil")
				}
				if loaded.Registry.Aqua.Ref != "v4.465.0" {
					t.Errorf("expected ref v4.465.0, got %s", loaded.Registry.Aqua.Ref)
				}
			},
		},
		{
			name: "registry with tools",
			state: &UserState{
				Version: Version,
				Registry: &RegistryState{
					Aqua: &AquaRegistryState{
						Ref:       "v4.465.0",
						UpdatedAt: now,
					},
				},
				Tools: map[string]*resource.ToolState{
					"gh": {
						InstallerRef: "aqua",
						Version:      "2.86.0",
						Package:      &resource.Package{Owner: "cli", Repo: "cli"},
						InstallPath:  "/home/user/.local/share/tomei/tools/gh/2.86.0",
						BinPath:      "/home/user/.local/bin/gh",
						UpdatedAt:    now,
					},
				},
			},
			check: func(t *testing.T, loaded *UserState) {
				// Verify registry
				if loaded.Registry == nil || loaded.Registry.Aqua == nil {
					t.Fatal("registry should be loaded")
				}
				if loaded.Registry.Aqua.Ref != "v4.465.0" {
					t.Errorf("expected ref v4.465.0, got %s", loaded.Registry.Aqua.Ref)
				}
				// Verify tool
				if len(loaded.Tools) != 1 {
					t.Fatalf("expected 1 tool, got %d", len(loaded.Tools))
				}
				tool := loaded.Tools["gh"]
				if tool == nil {
					t.Fatal("tool 'gh' should exist")
				}
				if tool.Package == nil || tool.Package.String() != "cli/cli" {
					t.Errorf("expected package cli/cli, got %v", tool.Package)
				}
				if tool.InstallerRef != "aqua" {
					t.Errorf("expected installerRef aqua, got %s", tool.InstallerRef)
				}
			},
		},
		{
			name: "nil registry (backward compatibility)",
			state: &UserState{
				Version: Version,
				Tools: map[string]*resource.ToolState{
					"ripgrep": {
						InstallerRef: "aqua",
						Version:      "14.0.0",
						UpdatedAt:    now,
					},
				},
			},
			check: func(t *testing.T, loaded *UserState) {
				// Registry should be nil (omitempty)
				if loaded.Registry != nil {
					t.Error("registry should be nil when not set")
				}
				// Tools should still be loaded
				if len(loaded.Tools) != 1 {
					t.Errorf("expected 1 tool, got %d", len(loaded.Tools))
				}
			},
		},
		{
			name: "multiple tools with registry",
			state: &UserState{
				Version: Version,
				Registry: &RegistryState{
					Aqua: &AquaRegistryState{
						Ref:       "v4.500.0",
						UpdatedAt: now,
					},
				},
				Tools: map[string]*resource.ToolState{
					"gh": {
						InstallerRef: "aqua",
						Version:      "2.86.0",
						Package:      &resource.Package{Owner: "cli", Repo: "cli"},
						UpdatedAt:    now,
					},
					"rg": {
						InstallerRef: "aqua",
						Version:      "14.0.0",
						Package:      &resource.Package{Owner: "BurntSushi", Repo: "ripgrep"},
						UpdatedAt:    now,
					},
				},
			},
			check: func(t *testing.T, loaded *UserState) {
				if loaded.Registry == nil || loaded.Registry.Aqua == nil {
					t.Fatal("registry should be loaded")
				}
				if loaded.Registry.Aqua.Ref != "v4.500.0" {
					t.Errorf("expected ref v4.500.0, got %s", loaded.Registry.Aqua.Ref)
				}
				if len(loaded.Tools) != 2 {
					t.Fatalf("expected 2 tools, got %d", len(loaded.Tools))
				}
				if loaded.Tools["gh"].Package == nil || loaded.Tools["gh"].Package.String() != "cli/cli" {
					t.Errorf("expected gh package cli/cli, got %v", loaded.Tools["gh"].Package)
				}
				if loaded.Tools["rg"].Package == nil || loaded.Tools["rg"].Package.String() != "BurntSushi/ripgrep" {
					t.Errorf("expected rg package BurntSushi/ripgrep, got %v", loaded.Tools["rg"].Package)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := NewStore[UserState](dir)
			if err != nil {
				t.Fatalf("failed to create store: %v", err)
			}

			if err := store.Lock(); err != nil {
				t.Fatalf("failed to lock: %v", err)
			}
			defer func() { _ = store.Unlock() }()

			if err := store.Save(tt.state); err != nil {
				t.Fatalf("failed to save: %v", err)
			}

			loaded, err := store.Load()
			if err != nil {
				t.Fatalf("failed to load: %v", err)
			}

			tt.check(t, loaded)
		})
	}
}

func TestStore_SetQuiet(t *testing.T) {
	// Create state with empty version fields to trigger warnings
	stateJSON := `{
		"version": "1",
		"tools": {
			"cargo-binstall": {
				"installerRef": "rust",
				"version": "",
				"updatedAt": "2025-01-01T00:00:00Z"
			}
		},
		"runtimes": {
			"rust": {
				"version": "",
				"installPath": "",
				"updatedAt": "2025-01-01T00:00:00Z"
			}
		}
	}`

	t.Run("quiet suppresses warnings", func(t *testing.T) {
		dir := t.TempDir()
		store, err := NewStore[UserState](dir)
		if err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		if err := os.WriteFile(store.StatePath(), []byte(stateJSON), 0644); err != nil {
			t.Fatalf("failed to write state: %v", err)
		}

		// Capture slog output
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(handler))
		defer slog.SetDefault(oldLogger)

		store.SetQuiet(true)

		if err := store.Lock(); err != nil {
			t.Fatalf("lock failed: %v", err)
		}
		defer func() { _ = store.Unlock() }()

		_, err = store.Load()
		if err != nil {
			t.Fatalf("load failed: %v", err)
		}

		if buf.Len() > 0 {
			t.Errorf("expected no warnings with quiet=true, got: %s", buf.String())
		}
	})

	t.Run("non-quiet emits warnings", func(t *testing.T) {
		dir := t.TempDir()
		store, err := NewStore[UserState](dir)
		if err != nil {
			t.Fatalf("failed to create store: %v", err)
		}

		if err := os.WriteFile(store.StatePath(), []byte(stateJSON), 0644); err != nil {
			t.Fatalf("failed to write state: %v", err)
		}

		// Capture slog output
		var buf bytes.Buffer
		handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
		oldLogger := slog.Default()
		slog.SetDefault(slog.New(handler))
		defer slog.SetDefault(oldLogger)

		if err := store.Lock(); err != nil {
			t.Fatalf("lock failed: %v", err)
		}
		defer func() { _ = store.Unlock() }()

		_, err = store.Load()
		if err != nil {
			t.Fatalf("load failed: %v", err)
		}

		if buf.Len() == 0 {
			t.Error("expected warnings with quiet=false, got none")
		}
	})
}
