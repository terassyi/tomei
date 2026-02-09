package log

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func TestLogStore_RecordAndFailedResources(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	// Start two resources
	store.RecordStart(resource.KindTool, "ripgrep", "14.0.0", "install", "download")
	store.RecordStart(resource.KindTool, "gopls", "0.16.0", "install", "go install")

	// Add output to both
	store.RecordOutput(resource.KindTool, "ripgrep", "downloading...")
	store.RecordOutput(resource.KindTool, "ripgrep", "verifying checksum...")

	store.RecordOutput(resource.KindTool, "gopls", "go: downloading golang.org/x/tools")
	store.RecordOutput(resource.KindTool, "gopls", "compile error: something broke")

	// gopls fails, ripgrep succeeds
	store.RecordError(resource.KindTool, "gopls", errors.New("command failed: exit status 1"))
	store.RecordComplete(resource.KindTool, "ripgrep")

	// Check failed resources
	failed := store.FailedResources()
	require.Len(t, failed, 1)

	assert.Equal(t, resource.KindTool, failed[0].Kind)
	assert.Equal(t, "gopls", failed[0].Name)
	assert.Equal(t, "0.16.0", failed[0].Version)
	assert.Equal(t, "install", failed[0].Action)
	assert.Equal(t, "go install", failed[0].Method)
	require.EqualError(t, failed[0].Error, "command failed: exit status 1")
	assert.Contains(t, failed[0].Output, "go: downloading golang.org/x/tools\n")
	assert.Contains(t, failed[0].Output, "compile error: something broke\n")
}

func TestLogStore_RecordComplete_DiscardsFile(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	store.RecordStart(resource.KindTool, "foo", "1.0.0", "install", "download")
	store.RecordOutput(resource.KindTool, "foo", "some output")
	store.RecordComplete(resource.KindTool, "foo")

	failed := store.FailedResources()
	assert.Empty(t, failed)

	// Writer should be cleaned up
	store.mu.Lock()
	_, writerExists := store.writers[resourceKey(resource.KindTool, "foo")]
	_, metaExists := store.metadata[resourceKey(resource.KindTool, "foo")]
	store.mu.Unlock()

	assert.False(t, writerExists)
	assert.False(t, metaExists)

	// Temporary file should be removed
	tmpPath := filepath.Join(store.SessionDir(), tmpFilename(resource.KindTool, "foo"))
	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err))
}

func TestLogStore_Flush(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	store.RecordStart(resource.KindTool, "gopls", "0.16.0", "install", "go install")
	store.RecordOutput(resource.KindTool, "gopls", "go: downloading something")
	store.RecordOutput(resource.KindTool, "gopls", "error: build failed")
	store.RecordError(resource.KindTool, "gopls", errors.New("exit status 1"))

	store.RecordStart(resource.KindRuntime, "rust", "stable", "install", "rustup")
	store.RecordOutput(resource.KindRuntime, "rust", "info: installing component")
	store.RecordError(resource.KindRuntime, "rust", errors.New("network error"))

	err = store.Flush()
	require.NoError(t, err)

	// Check files exist
	goplsLog := filepath.Join(store.SessionDir(), "Tool_gopls.log")
	rustLog := filepath.Join(store.SessionDir(), "Runtime_rust.log")

	goplsContent, err := os.ReadFile(goplsLog)
	require.NoError(t, err)
	assert.Contains(t, string(goplsContent), "# Resource: Tool/gopls")
	assert.Contains(t, string(goplsContent), "# Version: 0.16.0")
	assert.Contains(t, string(goplsContent), "# Action: install")
	assert.Contains(t, string(goplsContent), "# Method: go install")
	assert.Contains(t, string(goplsContent), "# Error: exit status 1")
	assert.Contains(t, string(goplsContent), "go: downloading something")
	assert.Contains(t, string(goplsContent), "error: build failed")

	rustContent, err := os.ReadFile(rustLog)
	require.NoError(t, err)
	assert.Contains(t, string(rustContent), "# Resource: Runtime/rust")
	assert.Contains(t, string(rustContent), "info: installing component")

	// Temporary files should be cleaned up after Flush
	tmpFiles, _ := filepath.Glob(filepath.Join(store.SessionDir(), ".tmp_*"))
	assert.Empty(t, tmpFiles)
}

func TestLogStore_Flush_NoFailures(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	require.NoError(t, err)

	store.RecordStart(resource.KindTool, "foo", "1.0.0", "install", "download")
	store.RecordComplete(resource.KindTool, "foo")

	err = store.Flush()
	require.NoError(t, err)

	// Close should clean up the empty session directory
	store.Close()

	_, err = os.Stat(store.SessionDir())
	assert.True(t, os.IsNotExist(err))
}

func TestLogStore_Cleanup(t *testing.T) {
	tmpDir := t.TempDir()

	// Create 7 fake session directories
	sessions := []string{
		"20260201T100000",
		"20260202T100000",
		"20260203T100000",
		"20260204T100000",
		"20260205T100000",
		"20260206T100000",
		"20260207T100000",
	}
	for _, s := range sessions {
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, s), 0755))
	}

	store, err := NewStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	err = store.Cleanup(3)
	require.NoError(t, err)

	// Should keep the 3 most recent
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)

	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}

	assert.Len(t, dirs, 3)
	assert.Contains(t, dirs, "20260205T100000")
	assert.Contains(t, dirs, "20260206T100000")
	assert.Contains(t, dirs, "20260207T100000")
}

func TestLogStore_Cleanup_FewSessions(t *testing.T) {
	tmpDir := t.TempDir()

	// Only 2 sessions, keep 5
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "20260201T100000"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "20260202T100000"), 0755))

	store, err := NewStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	err = store.Cleanup(5)
	require.NoError(t, err)

	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestLogStore_MultipleFailures_Sorted(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	store.RecordStart(resource.KindTool, "zebra", "1.0.0", "install", "download")
	store.RecordStart(resource.KindRuntime, "go", "1.25.0", "install", "download")
	store.RecordStart(resource.KindTool, "alpha", "2.0.0", "install", "cargo install")

	store.RecordError(resource.KindTool, "zebra", errors.New("err1"))
	store.RecordError(resource.KindRuntime, "go", errors.New("err2"))
	store.RecordError(resource.KindTool, "alpha", errors.New("err3"))

	failed := store.FailedResources()
	require.Len(t, failed, 3)

	// Should be sorted: Runtime/go, Tool/alpha, Tool/zebra
	assert.Equal(t, resource.KindRuntime, failed[0].Kind)
	assert.Equal(t, "go", failed[0].Name)
	assert.Equal(t, resource.KindTool, failed[1].Kind)
	assert.Equal(t, "alpha", failed[1].Name)
	assert.Equal(t, resource.KindTool, failed[2].Kind)
	assert.Equal(t, "zebra", failed[2].Name)
}

func TestLogStore_Close_CleansUpTmpFiles(t *testing.T) {
	tmpDir := t.TempDir()

	store, err := NewStore(tmpDir)
	require.NoError(t, err)

	store.RecordStart(resource.KindTool, "foo", "1.0.0", "install", "download")
	store.RecordOutput(resource.KindTool, "foo", "some output")
	// Neither Complete nor Error — simulate abrupt Close

	store.Close()

	// Temporary file should be removed
	tmpFiles, _ := filepath.Glob(filepath.Join(store.SessionDir(), ".tmp_*"))
	assert.Empty(t, tmpFiles)

	// Empty session directory should be removed
	_, err = os.Stat(store.SessionDir())
	assert.True(t, os.IsNotExist(err))
}
