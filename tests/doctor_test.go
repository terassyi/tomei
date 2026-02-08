//go:build integration

package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/doctor"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

func TestDoctor_Integration_UnmanagedTools(t *testing.T) {
	// Setup temp directories
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	binDir := filepath.Join(tmpDir, "bin")
	goBinDir := filepath.Join(tmpDir, "go", "bin")
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.MkdirAll(binDir, 0755))
	require.NoError(t, os.MkdirAll(goBinDir, 0755))

	// Create paths
	paths, err := path.New(
		path.WithUserDataDir(dataDir),
		path.WithUserBinDir(binDir),
	)
	require.NoError(t, err)

	// Create and save state with a runtime
	store, err := state.NewStore[state.UserState](dataDir)
	require.NoError(t, err)

	require.NoError(t, store.Lock())
	defer store.Unlock()

	userState := state.UserState{
		Version: "1",
		Runtimes: map[string]*resource.RuntimeState{
			"go": {
				Version:     "1.25.0",
				InstallPath: filepath.Join(tmpDir, "runtimes", "go", "1.25.0"),
				ToolBinPath: goBinDir,
				Binaries:    []string{"go", "gofmt"},
			},
		},
		Tools: map[string]*resource.ToolState{
			"managed-tool": {
				Version:    "1.0.0",
				RuntimeRef: "", // download pattern
				BinPath:    filepath.Join(binDir, "managed-tool"),
			},
			"gopls": {
				Version:    "0.21.0",
				RuntimeRef: "go", // delegation pattern
				BinPath:    filepath.Join(goBinDir, "gopls"),
			},
		},
	}
	require.NoError(t, store.Save(&userState))

	// Create managed tool in binDir
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "managed-tool"), []byte("#!/bin/bash"), 0755))

	// Create managed tool in goBinDir (gopls)
	require.NoError(t, os.WriteFile(filepath.Join(goBinDir, "gopls"), []byte("#!/bin/bash"), 0755))

	// Create unmanaged tool in goBinDir
	require.NoError(t, os.WriteFile(filepath.Join(goBinDir, "goimports"), []byte("#!/bin/bash"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(goBinDir, "staticcheck"), []byte("#!/bin/bash"), 0755))

	// Run doctor
	doc, err := doctor.New(paths, &userState)
	require.NoError(t, err)
	result, err := doc.Check(context.Background())
	require.NoError(t, err)

	// Verify unmanaged tools detected
	assert.True(t, result.HasIssues())
	assert.Len(t, result.UnmanagedTools["go"], 2)

	// Check tool names
	var names []string
	for _, tool := range result.UnmanagedTools["go"] {
		names = append(names, tool.Name)
	}
	assert.Contains(t, names, "goimports")
	assert.Contains(t, names, "staticcheck")

	// managed-tool should not be in unmanaged
	assert.Empty(t, result.UnmanagedTools["tomei"])
}

func TestDoctor_Integration_Conflicts(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	binDir := filepath.Join(tmpDir, "bin")
	goBinDir := filepath.Join(tmpDir, "go", "bin")
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.MkdirAll(binDir, 0755))
	require.NoError(t, os.MkdirAll(goBinDir, 0755))

	paths, err := path.New(
		path.WithUserDataDir(dataDir),
		path.WithUserBinDir(binDir),
	)
	require.NoError(t, err)

	// Create same tool in both locations
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "conflicting-tool"), []byte("#!/bin/bash"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(goBinDir, "conflicting-tool"), []byte("#!/bin/bash"), 0755))

	userState := &state.UserState{
		Version: "1",
		Runtimes: map[string]*resource.RuntimeState{
			"go": {
				Version:     "1.25.0",
				ToolBinPath: goBinDir,
			},
		},
	}

	doc, err := doctor.New(paths, userState)
	require.NoError(t, err)
	result, err := doc.Check(context.Background())
	require.NoError(t, err)

	assert.Len(t, result.Conflicts, 1)
	assert.Equal(t, "conflicting-tool", result.Conflicts[0].Name)
	assert.Len(t, result.Conflicts[0].Locations, 2)
}

func TestDoctor_Integration_StateIntegrity(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.MkdirAll(binDir, 0755))

	paths, err := path.New(
		path.WithUserDataDir(dataDir),
		path.WithUserBinDir(binDir),
	)
	require.NoError(t, err)

	// Create state that references non-existent files
	userState := &state.UserState{
		Version: "1",
		Tools: map[string]*resource.ToolState{
			"missing-tool": {
				Version:     "1.0.0",
				BinPath:     filepath.Join(binDir, "missing-tool"),
				InstallPath: filepath.Join(dataDir, "tools", "missing-tool", "1.0.0"),
			},
		},
	}

	doc, err := doctor.New(paths, userState)
	require.NoError(t, err)
	result, err := doc.Check(context.Background())
	require.NoError(t, err)

	assert.True(t, result.HasIssues())
	assert.NotEmpty(t, result.StateIssues)

	// Check for missing binary issue
	var hasMissingBinary bool
	for _, issue := range result.StateIssues {
		if issue.Kind == doctor.StateIssueMissingBinary && issue.Name == "missing-tool" {
			hasMissingBinary = true
			break
		}
	}
	assert.True(t, hasMissingBinary)
}

func TestDoctor_Integration_NoIssues(t *testing.T) {
	tmpDir := t.TempDir()
	dataDir := filepath.Join(tmpDir, "data")
	binDir := filepath.Join(tmpDir, "bin")
	toolDir := filepath.Join(dataDir, "tools", "mytool", "1.0.0")
	require.NoError(t, os.MkdirAll(dataDir, 0755))
	require.NoError(t, os.MkdirAll(binDir, 0755))
	require.NoError(t, os.MkdirAll(toolDir, 0755))

	paths, err := path.New(
		path.WithUserDataDir(dataDir),
		path.WithUserBinDir(binDir),
	)
	require.NoError(t, err)

	// Create actual binary
	binaryPath := filepath.Join(toolDir, "mytool")
	require.NoError(t, os.WriteFile(binaryPath, []byte("#!/bin/bash"), 0755))

	// Create symlink
	symlinkPath := filepath.Join(binDir, "mytool")
	require.NoError(t, os.Symlink(binaryPath, symlinkPath))

	userState := &state.UserState{
		Version: "1",
		Tools: map[string]*resource.ToolState{
			"mytool": {
				Version:     "1.0.0",
				BinPath:     symlinkPath,
				InstallPath: toolDir,
				RuntimeRef:  "", // download pattern
			},
		},
	}

	doc, err := doctor.New(paths, userState)
	require.NoError(t, err)
	result, err := doc.Check(context.Background())
	require.NoError(t, err)

	assert.False(t, result.HasIssues())
}
