//go:build integration

package tests

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/runtime"
	"github.com/terassyi/tomei/internal/resource"
)

// TestRuntimeDelegation_Install tests the real runtime Installer with the delegation pattern
// using echo-based mock bootstrap commands.
func TestRuntimeDelegation_Install(t *testing.T) {
	t.Run("basic delegation install and state", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")
		require.NoError(t, os.MkdirAll(binDir, 0755))

		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		rt := &resource.Runtime{
			BaseResource: resource.BaseResource{
				APIVersion:   resource.GroupVersion,
				ResourceKind: resource.KindRuntime,
				Metadata:     resource.Metadata{Name: "mock"},
			},
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDelegation,
				Version:     "1.0.0",
				ToolBinPath: binDir,
				Bootstrap: &resource.RuntimeBootstrapSpec{
					CommandSet: resource.CommandSet{
						Install: "echo installing version {{.Version}}",
						Check:   "true",
						Remove:  "echo removing",
					},
				},
				Commands: &resource.CommandsSpec{
					Install: "echo installing tool {{.Package}}@{{.Version}}",
					Remove:  "echo removing tool {{.Name}}",
				},
				Env: map[string]string{
					"MOCK_HOME": filepath.Join(tmpDir, "mock-home"),
				},
			},
		}

		ctx := context.Background()
		state, err := installer.Install(ctx, rt, "mock")
		require.NoError(t, err)

		// Verify state fields
		assert.Equal(t, resource.InstallTypeDelegation, state.Type)
		assert.Equal(t, "1.0.0", state.Version)
		assert.Equal(t, resource.VersionExact, state.VersionKind)
		assert.Equal(t, "1.0.0", state.SpecVersion)
		assert.Equal(t, binDir, state.ToolBinPath)
		assert.Equal(t, binDir, state.BinDir) // defaults to ToolBinPath
		assert.Equal(t, "echo removing", state.RemoveCommand)
		assert.NotNil(t, state.Commands)
		assert.Equal(t, "echo installing tool {{.Package}}@{{.Version}}", state.Commands.Install)
		assert.Equal(t, filepath.Join(tmpDir, "mock-home"), state.Env["MOCK_HOME"])
		assert.Empty(t, state.InstallPath) // delegation doesn't set installPath
		assert.Empty(t, state.Digest)      // no download digest
		assert.False(t, state.UpdatedAt.IsZero())
	})

	t.Run("delegation with ResolveVersion", func(t *testing.T) {
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")

		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDelegation,
				Version:     "stable",
				ToolBinPath: binDir,
				Bootstrap: &resource.RuntimeBootstrapSpec{
					CommandSet: resource.CommandSet{
						Install: "echo installing version {{.Version}}",
						Check:   "true",
					},
					ResolveVersion: "echo 1.83.0",
				},
			},
		}

		ctx := context.Background()
		state, err := installer.Install(ctx, rt, "mock")
		require.NoError(t, err)

		assert.Equal(t, "1.83.0", state.Version)
		assert.Equal(t, resource.VersionAlias, state.VersionKind)
		assert.Equal(t, "stable", state.SpecVersion)
	})

	t.Run("delegation bootstrap check fails", func(t *testing.T) {
		tmpDir := t.TempDir()

		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDelegation,
				Version:     "1.0.0",
				ToolBinPath: filepath.Join(tmpDir, "bin"),
				Bootstrap: &resource.RuntimeBootstrapSpec{
					CommandSet: resource.CommandSet{
						Install: "echo installing",
						Check:   "false", // always fails
					},
				},
			},
		}

		_, err := installer.Install(context.Background(), rt, "mock")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bootstrap check failed")
	})

	t.Run("delegation install command fails", func(t *testing.T) {
		tmpDir := t.TempDir()

		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDelegation,
				Version:     "1.0.0",
				ToolBinPath: filepath.Join(tmpDir, "bin"),
				Bootstrap: &resource.RuntimeBootstrapSpec{
					CommandSet: resource.CommandSet{
						Install: "exit 1",
						Check:   "true",
					},
				},
			},
		}

		_, err := installer.Install(context.Background(), rt, "mock")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bootstrap install failed")
	})
}

// TestRuntimeDelegation_Remove tests the real runtime Installer.Remove with the delegation pattern.
func TestRuntimeDelegation_Remove(t *testing.T) {
	t.Run("delegation remove with command", func(t *testing.T) {
		tmpDir := t.TempDir()
		markerFile := filepath.Join(tmpDir, "removed")

		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		st := &resource.RuntimeState{
			Type:          resource.InstallTypeDelegation,
			Version:       "1.0.0",
			RemoveCommand: "touch " + markerFile,
		}

		err := installer.Remove(context.Background(), st, "mock")
		require.NoError(t, err)

		// Verify the remove command was actually executed
		assert.FileExists(t, markerFile)
	})

	t.Run("delegation remove without command is no-op", func(t *testing.T) {
		tmpDir := t.TempDir()

		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		st := &resource.RuntimeState{
			Type:    resource.InstallTypeDelegation,
			Version: "1.0.0",
		}

		err := installer.Remove(context.Background(), st, "mock")
		require.NoError(t, err)
	})

	t.Run("delegation remove command fails", func(t *testing.T) {
		tmpDir := t.TempDir()

		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		st := &resource.RuntimeState{
			Type:          resource.InstallTypeDelegation,
			Version:       "1.0.0",
			RemoveCommand: "exit 1",
		}

		err := installer.Remove(context.Background(), st, "mock")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bootstrap remove failed")
	})
}

// TestRuntimeDelegation_InstallThenRemove tests the full lifecycle: install → state → remove.
func TestRuntimeDelegation_InstallThenRemove(t *testing.T) {
	tmpDir := t.TempDir()
	binDir := filepath.Join(tmpDir, "bin")
	mockHome := filepath.Join(tmpDir, "mock-home")

	installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

	rt := &resource.Runtime{
		RuntimeSpec: &resource.RuntimeSpec{
			Type:        resource.InstallTypeDelegation,
			Version:     "1.0.0",
			ToolBinPath: binDir,
			Bootstrap: &resource.RuntimeBootstrapSpec{
				CommandSet: resource.CommandSet{
					Install: "mkdir -p " + mockHome,
					Check:   "test -d " + mockHome,
					Remove:  "rm -rf " + mockHome,
				},
			},
		},
	}

	ctx := context.Background()

	// Install
	state, err := installer.Install(ctx, rt, "mock")
	require.NoError(t, err)
	assert.Equal(t, resource.InstallTypeDelegation, state.Type)
	assert.DirExists(t, mockHome)

	// Remove using state's RemoveCommand
	err = installer.Remove(ctx, state, "mock")
	require.NoError(t, err)
	assert.NoDirExists(t, mockHome)
}
