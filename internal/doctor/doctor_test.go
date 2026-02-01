package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/toto/internal/path"
	"github.com/terassyi/toto/internal/resource"
	"github.com/terassyi/toto/internal/state"
)

func TestNew(t *testing.T) {
	paths, err := path.New()
	require.NoError(t, err)

	userState := &state.UserState{}
	doc, err := New(paths, userState)
	require.NoError(t, err)

	assert.NotNil(t, doc)
	assert.Equal(t, paths, doc.paths)
	assert.Equal(t, userState, doc.state)
	assert.NotNil(t, doc.scanPaths)
}

func TestDoctor_ScanForUnmanaged(t *testing.T) {
	t.Run("detects unmanaged tools", func(t *testing.T) {
		// Setup temp directories
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))

		// Create unmanaged executable
		unmanagedTool := filepath.Join(binDir, "unmanaged-tool")
		require.NoError(t, os.WriteFile(unmanagedTool, []byte("#!/bin/bash\necho hello"), 0755))

		// Create paths with custom bin dir
		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		// Empty state (no managed tools)
		userState := &state.UserState{
			Tools: make(map[string]*resource.ToolState),
		}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		unmanaged, err := doc.scanForUnmanaged()
		require.NoError(t, err)

		assert.Len(t, unmanaged["toto"], 1)
		assert.Equal(t, "unmanaged-tool", unmanaged["toto"][0].Name)
	})

	t.Run("does not detect managed tools", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))

		// Create managed tool (download pattern, no runtimeRef)
		managedTool := filepath.Join(binDir, "managed-tool")
		require.NoError(t, os.WriteFile(managedTool, []byte("#!/bin/bash\necho hello"), 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		// State with managed tool
		userState := &state.UserState{
			Tools: map[string]*resource.ToolState{
				"managed-tool": {
					Version:    "1.0.0",
					RuntimeRef: "", // download pattern
				},
			},
		}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		unmanaged, err := doc.scanForUnmanaged()
		require.NoError(t, err)

		assert.Empty(t, unmanaged["toto"])
	})

	t.Run("empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		userState := &state.UserState{}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		unmanaged, err := doc.scanForUnmanaged()
		require.NoError(t, err)

		assert.Empty(t, unmanaged)
	})

	t.Run("skips hidden files", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))

		// Create hidden file
		hiddenFile := filepath.Join(binDir, ".hidden")
		require.NoError(t, os.WriteFile(hiddenFile, []byte("#!/bin/bash"), 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		userState := &state.UserState{}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		unmanaged, err := doc.scanForUnmanaged()
		require.NoError(t, err)

		assert.Empty(t, unmanaged)
	})

	t.Run("scans runtime toolBinPath", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		goBinDir := filepath.Join(tmpDir, "go", "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))
		require.NoError(t, os.MkdirAll(goBinDir, 0755))

		// Create unmanaged tool in go bin
		unmanagedTool := filepath.Join(goBinDir, "goimports")
		require.NoError(t, os.WriteFile(unmanagedTool, []byte("#!/bin/bash"), 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		// State with go runtime
		userState := &state.UserState{
			Runtimes: map[string]*resource.RuntimeState{
				"go": {
					Version:     "1.25.0",
					ToolBinPath: goBinDir,
				},
			},
			Tools: make(map[string]*resource.ToolState),
		}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		unmanaged, err := doc.scanForUnmanaged()
		require.NoError(t, err)

		assert.Len(t, unmanaged["go"], 1)
		assert.Equal(t, "goimports", unmanaged["go"][0].Name)
	})

	t.Run("does not detect runtime delegation tools", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		goBinDir := filepath.Join(tmpDir, "go", "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))
		require.NoError(t, os.MkdirAll(goBinDir, 0755))

		// Create tool in go bin that is managed via delegation
		managedTool := filepath.Join(goBinDir, "gopls")
		require.NoError(t, os.WriteFile(managedTool, []byte("#!/bin/bash"), 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		userState := &state.UserState{
			Runtimes: map[string]*resource.RuntimeState{
				"go": {
					Version:     "1.25.0",
					ToolBinPath: goBinDir,
				},
			},
			Tools: map[string]*resource.ToolState{
				"gopls": {
					Version:    "0.21.0",
					RuntimeRef: "go", // delegation pattern
				},
			},
		}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		unmanaged, err := doc.scanForUnmanaged()
		require.NoError(t, err)

		assert.Empty(t, unmanaged["go"])
	})
}

func TestDoctor_DetectConflicts(t *testing.T) {
	t.Run("detects conflicts", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		goBinDir := filepath.Join(tmpDir, "go", "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))
		require.NoError(t, os.MkdirAll(goBinDir, 0755))

		// Create same tool in both locations
		require.NoError(t, os.WriteFile(filepath.Join(binDir, "mytool"), []byte("#!/bin/bash"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(goBinDir, "mytool"), []byte("#!/bin/bash"), 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		userState := &state.UserState{
			Runtimes: map[string]*resource.RuntimeState{
				"go": {
					Version:     "1.25.0",
					ToolBinPath: goBinDir,
				},
			},
		}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		conflicts, err := doc.detectConflicts()
		require.NoError(t, err)

		assert.Len(t, conflicts, 1)
		assert.Equal(t, "mytool", conflicts[0].Name)
		assert.Len(t, conflicts[0].Locations, 2)
	})

	t.Run("no conflicts when unique", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		goBinDir := filepath.Join(tmpDir, "go", "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))
		require.NoError(t, os.MkdirAll(goBinDir, 0755))

		// Create different tools
		require.NoError(t, os.WriteFile(filepath.Join(binDir, "tool1"), []byte("#!/bin/bash"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(goBinDir, "tool2"), []byte("#!/bin/bash"), 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		userState := &state.UserState{
			Runtimes: map[string]*resource.RuntimeState{
				"go": {
					Version:     "1.25.0",
					ToolBinPath: goBinDir,
				},
			},
		}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		conflicts, err := doc.detectConflicts()
		require.NoError(t, err)

		assert.Empty(t, conflicts)
	})
}

func TestDoctor_CheckStateIntegrity(t *testing.T) {
	t.Run("detects missing binary", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		// State references a tool that doesn't exist
		userState := &state.UserState{
			Tools: map[string]*resource.ToolState{
				"missing-tool": {
					Version: "1.0.0",
					BinPath: filepath.Join(binDir, "missing-tool"),
				},
			},
		}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		issues, err := doc.checkStateIntegrity()
		require.NoError(t, err)

		assert.Len(t, issues, 1)
		assert.Equal(t, StateIssueMissingBinary, issues[0].Kind)
		assert.Equal(t, "missing-tool", issues[0].Name)
	})

	t.Run("detects broken symlink", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))

		// Create broken symlink
		symlink := filepath.Join(binDir, "broken-tool")
		require.NoError(t, os.Symlink("/nonexistent/target", symlink))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		userState := &state.UserState{
			Tools: map[string]*resource.ToolState{
				"broken-tool": {
					Version: "1.0.0",
					BinPath: symlink,
				},
			},
		}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		issues, err := doc.checkStateIntegrity()
		require.NoError(t, err)

		assert.Len(t, issues, 1)
		assert.Equal(t, StateIssueBrokenSymlink, issues[0].Kind)
		assert.Equal(t, "broken-tool", issues[0].Name)
	})

	t.Run("no issues when healthy", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		toolDir := filepath.Join(tmpDir, "tools", "mytool", "1.0.0")
		require.NoError(t, os.MkdirAll(binDir, 0755))
		require.NoError(t, os.MkdirAll(toolDir, 0755))

		// Create actual binary
		binary := filepath.Join(toolDir, "mytool")
		require.NoError(t, os.WriteFile(binary, []byte("#!/bin/bash"), 0755))

		// Create symlink to it
		symlink := filepath.Join(binDir, "mytool")
		require.NoError(t, os.Symlink(binary, symlink))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		userState := &state.UserState{
			Tools: map[string]*resource.ToolState{
				"mytool": {
					Version:     "1.0.0",
					BinPath:     symlink,
					InstallPath: toolDir,
				},
			},
		}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		issues, err := doc.checkStateIntegrity()
		require.NoError(t, err)

		assert.Empty(t, issues)
	})
}

func TestDoctor_Check(t *testing.T) {
	t.Run("full check with no issues", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		userState := &state.UserState{}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		result, err := doc.Check(context.Background())
		require.NoError(t, err)

		assert.False(t, result.HasIssues())
	})
}

func TestResult_HasIssues(t *testing.T) {
	t.Run("no issues", func(t *testing.T) {
		result := &Result{
			UnmanagedTools: make(map[string][]UnmanagedTool),
		}
		assert.False(t, result.HasIssues())
	})

	t.Run("has unmanaged tools", func(t *testing.T) {
		result := &Result{
			UnmanagedTools: map[string][]UnmanagedTool{
				"toto": {{Name: "tool", Path: "/path"}},
			},
		}
		assert.True(t, result.HasIssues())
	})

	t.Run("has conflicts", func(t *testing.T) {
		result := &Result{
			UnmanagedTools: make(map[string][]UnmanagedTool),
			Conflicts:      []Conflict{{Name: "tool"}},
		}
		assert.True(t, result.HasIssues())
	})

	t.Run("has state issues", func(t *testing.T) {
		result := &Result{
			UnmanagedTools: make(map[string][]UnmanagedTool),
			StateIssues:    []StateIssue{{Kind: StateIssueMissingBinary}},
		}
		assert.True(t, result.HasIssues())
	})
}

func TestResult_UnmanagedToolNames(t *testing.T) {
	result := &Result{
		UnmanagedTools: map[string][]UnmanagedTool{
			"toto": {{Name: "tool1", Path: "/path1"}},
			"go":   {{Name: "tool2", Path: "/path2"}, {Name: "tool1", Path: "/path3"}}, // tool1 duplicate
		},
	}

	names := result.UnmanagedToolNames()
	assert.Len(t, names, 2) // tool1 should be deduplicated
	assert.Contains(t, names, "tool1")
	assert.Contains(t, names, "tool2")
}

func TestDoctor_IsRuntimeBinary(t *testing.T) {
	t.Run("returns true for runtime binaries of specific runtime", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		userState := &state.UserState{
			Runtimes: map[string]*resource.RuntimeState{
				"go": {
					Version:  "1.25.0",
					Binaries: []string{"go", "gofmt"},
				},
			},
		}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		assert.True(t, doc.isRuntimeBinary("go", "go"))
		assert.True(t, doc.isRuntimeBinary("gofmt", "go"))
		assert.False(t, doc.isRuntimeBinary("other", "go"))
		assert.False(t, doc.isRuntimeBinary("go", "rust")) // different runtime
	})

	t.Run("returns false for nil state", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		doc, err := New(paths, nil)
		require.NoError(t, err)
		assert.False(t, doc.isRuntimeBinary("go", "go"))
	})

	t.Run("does not report runtime binaries as unmanaged in runtime BinDir", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		goBinDir := filepath.Join(tmpDir, "go", "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))
		require.NoError(t, os.MkdirAll(goBinDir, 0755))

		// Create runtime binaries in go bin dir (new behavior - BinDir)
		require.NoError(t, os.WriteFile(filepath.Join(goBinDir, "go"), []byte("#!/bin/bash"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(goBinDir, "gofmt"), []byte("#!/bin/bash"), 0755))

		paths, err := path.New(path.WithUserBinDir(binDir))
		require.NoError(t, err)

		// State shows these binaries are from the go runtime with BinDir
		userState := &state.UserState{
			Runtimes: map[string]*resource.RuntimeState{
				"go": {
					Version:     "1.25.0",
					Binaries:    []string{"go", "gofmt"},
					BinDir:      goBinDir,
					ToolBinPath: goBinDir,
				},
			},
			Tools: make(map[string]*resource.ToolState),
		}

		doc, err := New(paths, userState)
		require.NoError(t, err)
		unmanaged, err := doc.scanForUnmanaged()
		require.NoError(t, err)

		// go and gofmt should not be reported as unmanaged in go category
		assert.Empty(t, unmanaged["go"])
	})
}
