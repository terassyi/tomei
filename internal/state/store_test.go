package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/terassyi/toto/internal/resource"
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
			InstallPath: "/home/user/.local/share/toto/runtimes/go/1.25.1",
			Binaries:    []string{"go", "gofmt"},
			ToolBinPath: "/home/user/go/bin",
			UpdatedAt:   time.Now(),
		},
	}
	state.Tools = map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "aqua",
			Version:      "14.0.0",
			InstallPath:  "/home/user/.local/share/toto/tools/ripgrep/14.0.0",
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
