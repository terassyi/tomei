package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/checksum"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/executor"
	"github.com/terassyi/tomei/internal/resource"
)

func TestNewInstaller(t *testing.T) {
	t.Parallel()
	downloader := download.NewDownloader()
	installer := NewInstaller(downloader, "/runtimes")

	assert.NotNil(t, installer)
	assert.Equal(t, "/runtimes", installer.runtimesDir)
}

func TestInstaller_Install(t *testing.T) {
	t.Parallel()
	// Create a mock runtime tarball with a top-level directory
	binContent := []byte("#!/bin/sh\necho 'mock runtime'\n")
	tarGzContent := createRuntimeTarGz(t, "myruntime", []mockBinary{
		{name: "mybin", content: binContent},
		{name: "mybin2", content: binContent},
	})
	archiveHash := sha256sum(tarGzContent)

	t.Run("successful install with BinDir", func(t *testing.T) {
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		dl := &mockRuntimeDownloader{archiveData: tarGzContent}
		installer := NewInstaller(dl, runtimesDir)

		runtime := &resource.Runtime{
			BaseResource: resource.BaseResource{
				APIVersion:   resource.GroupVersion,
				ResourceKind: resource.KindRuntime,
				Metadata:     resource.Metadata{Name: "myruntime"},
			},
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.0.0",
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.0.0.tar.gz",
					Checksum: &resource.Checksum{
						Value: "sha256:" + archiveHash,
					},
				},
				Binaries:    []string{"mybin", "mybin2"},
				BinDir:      binDir, // Explicit BinDir
				ToolBinPath: "~/myruntime/bin",
				Env: map[string]string{
					"MY_RUNTIME_HOME": "~/.local/share/tomei/runtimes/myruntime/1.0.0",
				},
			},
		}

		state, err := installer.Install(context.Background(), runtime, "myruntime")
		require.NoError(t, err)

		// Verify state
		assert.Equal(t, resource.InstallTypeDownload, state.Type)
		assert.Equal(t, "1.0.0", state.Version)
		assert.Equal(t, checksum.Digest(archiveHash), state.Digest)
		assert.Contains(t, state.InstallPath, "myruntime/1.0.0")
		assert.Equal(t, []string{"mybin", "mybin2"}, state.Binaries)
		assert.Equal(t, binDir, state.BinDir)
		// ToolBinPath and Env values should have ~ expanded
		home, _ := os.UserHomeDir()
		assert.Equal(t, filepath.Join(home, "myruntime/bin"), state.ToolBinPath)
		assert.Equal(t, filepath.Join(home, ".local/share/tomei/runtimes/myruntime/1.0.0"), state.Env["MY_RUNTIME_HOME"])

		// Verify install directory exists
		assert.DirExists(t, state.InstallPath)

		// Verify binaries exist
		assert.FileExists(t, filepath.Join(state.InstallPath, "bin", "mybin"))
		assert.FileExists(t, filepath.Join(state.InstallPath, "bin", "mybin2"))

		// Verify symlinks in BinDir
		link1, err := os.Readlink(filepath.Join(binDir, "mybin"))
		require.NoError(t, err)
		assert.Contains(t, link1, "mybin")

		link2, err := os.Readlink(filepath.Join(binDir, "mybin2"))
		require.NoError(t, err)
		assert.Contains(t, link2, "mybin2")
	})

	t.Run("successful install with ToolBinPath as default BinDir", func(t *testing.T) {
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		toolBinDir := filepath.Join(tmpDir, "toolbin")

		dl := &mockRuntimeDownloader{archiveData: tarGzContent}
		installer := NewInstaller(dl, runtimesDir)

		runtime := &resource.Runtime{
			BaseResource: resource.BaseResource{
				APIVersion:   resource.GroupVersion,
				ResourceKind: resource.KindRuntime,
				Metadata:     resource.Metadata{Name: "myruntime"},
			},
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.0.0",
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.0.0.tar.gz",
					Checksum: &resource.Checksum{
						Value: "sha256:" + archiveHash,
					},
				},
				Binaries:    []string{"mybin", "mybin2"},
				ToolBinPath: toolBinDir, // No BinDir, should use ToolBinPath
			},
		}

		state, err := installer.Install(context.Background(), runtime, "myruntime")
		require.NoError(t, err)

		// BinDir should be ToolBinPath
		assert.Equal(t, toolBinDir, state.BinDir)

		// Verify symlinks in ToolBinPath
		link1, err := os.Readlink(filepath.Join(toolBinDir, "mybin"))
		require.NoError(t, err)
		assert.Contains(t, link1, "mybin")
	})

	t.Run("already installed", func(t *testing.T) {
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")

		// Pre-create the install directory with a fake binary
		installPath := filepath.Join(runtimesDir, "myruntime", "1.0.0")
		require.NoError(t, os.MkdirAll(filepath.Join(installPath, "bin"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(installPath, "bin", "mybin"), []byte("v1.0.0"), 0755))

		binDir := filepath.Join(tmpDir, "bin")

		installer := NewInstaller(download.NewDownloader(), runtimesDir)

		runtime := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.0.0",
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.0.0.tar.gz",
				},
				Binaries: []string{"mybin"},
				BinDir:   binDir,
			},
		}

		state, err := installer.Install(context.Background(), runtime, "myruntime")
		require.NoError(t, err)
		assert.Equal(t, installPath, state.InstallPath)

		// Verify symlink was created even though download was skipped
		linkPath := filepath.Join(binDir, "mybin")
		target, err := os.Readlink(linkPath)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(installPath, "bin", "mybin"), target)
	})

	t.Run("already installed rebuilds symlinks on version switch", func(t *testing.T) {
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		// Pre-create two version directories with fake binaries
		for _, ver := range []string{"1.0.0", "2.0.0"} {
			installPath := filepath.Join(runtimesDir, "myruntime", ver)
			require.NoError(t, os.MkdirAll(filepath.Join(installPath, "bin"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(installPath, "bin", "mybin"), []byte("v"+ver), 0755))
		}

		// Create initial symlink pointing to 2.0.0
		require.NoError(t, os.MkdirAll(binDir, 0755))
		require.NoError(t, os.Symlink(
			filepath.Join(runtimesDir, "myruntime", "2.0.0", "bin", "mybin"),
			filepath.Join(binDir, "mybin"),
		))

		installer := NewInstaller(download.NewDownloader(), runtimesDir)

		// Install (downgrade) to 1.0.0 — directory already exists
		runtime := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.0.0",
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.0.0.tar.gz",
				},
				Binaries: []string{"mybin"},
				BinDir:   binDir,
			},
		}

		state, err := installer.Install(context.Background(), runtime, "myruntime")
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", state.Version)

		// Verify symlink now points to 1.0.0
		linkPath := filepath.Join(binDir, "mybin")
		target, err := os.Readlink(linkPath)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(runtimesDir, "myruntime", "1.0.0", "bin", "mybin"), target)
	})

	t.Run("delegation basic", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")

		runner := &mockCommandRunner{
			checkResult: true,
		}
		installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDelegation,
				Version:     "1.0.0",
				ToolBinPath: binDir,
				Bootstrap: &resource.RuntimeBootstrapSpec{
					CommandSet: resource.CommandSet{
						Install: []string{"install-cmd {{.Version}}"},
						Check:   []string{"check-cmd"},
						Remove:  []string{"remove-cmd"},
					},
				},
				Commands: &resource.CommandsSpec{
					Install: []string{"tool-install {{.Name}}"},
				},
			},
		}

		state, err := installer.Install(context.Background(), rt, "mock")
		require.NoError(t, err)

		assert.Equal(t, resource.InstallTypeDelegation, state.Type)
		assert.Equal(t, "1.0.0", state.Version)
		assert.Equal(t, resource.VersionExact, state.VersionKind)
		assert.Equal(t, "1.0.0", state.SpecVersion)
		assert.Equal(t, binDir, state.ToolBinPath)
		assert.Equal(t, []string{"remove-cmd"}, state.RemoveCommand)
		assert.NotNil(t, state.Commands)

		// Verify install command was called
		require.Len(t, runner.executeWithEnvCalls, 1)
		assert.Equal(t, []string{"install-cmd {{.Version}}"}, runner.executeWithEnvCalls[0].cmds)
		assert.Equal(t, "1.0.0", runner.executeWithEnvCalls[0].vars.Version)

		// Verify check command was called
		require.Len(t, runner.checkCalls, 1)
		assert.Equal(t, []string{"check-cmd"}, runner.checkCalls[0].cmds)
	})

	t.Run("delegation with ResolveVersion", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		binDir := filepath.Join(tmpDir, "bin")

		runner := &mockCommandRunner{
			captureResult: "1.83.0",
			checkResult:   true,
		}
		installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDelegation,
				Version:     "stable",
				ToolBinPath: binDir,
				Bootstrap: &resource.RuntimeBootstrapSpec{
					CommandSet: resource.CommandSet{
						Install: []string{"install-cmd {{.Version}}"},
						Check:   []string{"check-cmd"},
					},
					ResolveVersion: []string{"resolve-cmd"},
				},
			},
		}

		state, err := installer.Install(context.Background(), rt, "mock")
		require.NoError(t, err)

		assert.Equal(t, "1.83.0", state.Version)
		assert.Equal(t, resource.VersionAlias, state.VersionKind)
		assert.Equal(t, "stable", state.SpecVersion)

		// Verify resolve was called
		require.Len(t, runner.captureCalls, 1)
		assert.Equal(t, []string{"resolve-cmd"}, runner.captureCalls[0].cmds)

		// Verify install was called with resolved version
		require.Len(t, runner.executeWithEnvCalls, 1)
		assert.Equal(t, "1.83.0", runner.executeWithEnvCalls[0].vars.Version)
	})

	t.Run("delegation check fails", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		runner := &mockCommandRunner{
			checkResult: false,
		}
		installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDelegation,
				Version:     "1.0.0",
				ToolBinPath: filepath.Join(tmpDir, "bin"),
				Bootstrap: &resource.RuntimeBootstrapSpec{
					CommandSet: resource.CommandSet{
						Install: []string{"install-cmd"},
						Check:   []string{"check-cmd"},
					},
				},
			},
		}

		_, err := installer.Install(context.Background(), rt, "mock")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bootstrap check failed")
	})

	t.Run("delegation ResolveVersion fails", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		runner := &mockCommandRunner{
			captureErr: fmt.Errorf("command failed: exit 1"),
		}
		installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDelegation,
				Version:     "stable",
				ToolBinPath: filepath.Join(tmpDir, "bin"),
				Bootstrap: &resource.RuntimeBootstrapSpec{
					CommandSet: resource.CommandSet{
						Install: []string{"install-cmd"},
						Check:   []string{"check-cmd"},
					},
					ResolveVersion: []string{"resolve-cmd"},
				},
			},
		}

		_, err := installer.Install(context.Background(), rt, "mock")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to resolve version")
	})

	t.Run("delegation install command fails", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		runner := &mockCommandRunner{
			executeErr: fmt.Errorf("command failed: install error"),
		}
		installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDelegation,
				Version:     "1.0.0",
				ToolBinPath: filepath.Join(tmpDir, "bin"),
				Bootstrap: &resource.RuntimeBootstrapSpec{
					CommandSet: resource.CommandSet{
						Install: []string{"install-cmd"},
						Check:   []string{"check-cmd"},
					},
				},
			},
		}

		_, err := installer.Install(context.Background(), rt, "mock")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bootstrap install failed")
	})

	t.Run("delegation missing bootstrap", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDelegation,
				Version:     "1.0.0",
				ToolBinPath: filepath.Join(tmpDir, "bin"),
			},
		}

		_, err := installer.Install(context.Background(), rt, "mock")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bootstrap is required")
	})

	t.Run("missing source URL", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		runtime := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDownload,
				Version:     "1.0.0",
				Source:      &resource.DownloadSource{},
				ToolBinPath: "~/bin",
			},
		}

		_, err := installer.Install(context.Background(), runtime, "myruntime")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "source.url is required")
	})
}

func TestInstaller_DelegationUpdateCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		update   []string
		action   resource.ActionType
		wantCmds []string
	}{
		{
			name:     "ActionInstall uses install even with update configured",
			update:   []string{"update-cmd {{.Version}}"},
			action:   resource.ActionInstall,
			wantCmds: []string{"install-cmd {{.Version}}"},
		},
		{
			name:     "ActionUpgrade uses update when configured",
			update:   []string{"update-cmd {{.Version}}"},
			action:   resource.ActionUpgrade,
			wantCmds: []string{"update-cmd {{.Version}}"},
		},
		{
			name:     "ActionReinstall uses update when configured",
			update:   []string{"update-cmd {{.Version}}"},
			action:   resource.ActionReinstall,
			wantCmds: []string{"update-cmd {{.Version}}"},
		},
		{
			name:     "ActionUpgrade falls back to install without update",
			update:   nil,
			action:   resource.ActionUpgrade,
			wantCmds: []string{"install-cmd {{.Version}}"},
		},
		{
			name:     "zero-value action defaults to install",
			update:   []string{"update-cmd {{.Version}}"},
			action:   "",
			wantCmds: []string{"install-cmd {{.Version}}"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			binDir := filepath.Join(tmpDir, "bin")

			runner := &mockCommandRunner{
				checkResult: true,
			}
			installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

			rt := &resource.Runtime{
				RuntimeSpec: &resource.RuntimeSpec{
					Type:        resource.InstallTypeDelegation,
					Version:     "1.0.0",
					ToolBinPath: binDir,
					Bootstrap: &resource.RuntimeBootstrapSpec{
						CommandSet: resource.CommandSet{
							Install: []string{"install-cmd {{.Version}}"},
							Check:   []string{"check-cmd"},
						},
						Update: tt.update,
					},
				},
			}

			ctx := context.Background()
			if tt.action != "" {
				ctx = executor.WithAction(ctx, tt.action)
			}

			state, err := installer.Install(ctx, rt, "mock")
			require.NoError(t, err)

			assert.Equal(t, resource.InstallTypeDelegation, state.Type)
			assert.Equal(t, "1.0.0", state.Version)

			// Verify the correct command was executed
			require.Len(t, runner.executeWithEnvCalls, 1)
			assert.Equal(t, tt.wantCmds, runner.executeWithEnvCalls[0].cmds)
		})
	}
}

func TestInstaller_DelegationUpdateCommand_ErrorMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		update    []string
		action    resource.ActionType
		wantError string
	}{
		{
			name:      "install error says bootstrap install",
			action:    resource.ActionInstall,
			wantError: "bootstrap install failed",
		},
		{
			name:      "update error says bootstrap update",
			update:    []string{"update-cmd"},
			action:    resource.ActionUpgrade,
			wantError: "bootstrap update failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()

			runner := &mockCommandRunner{
				executeErr: fmt.Errorf("command failed"),
			}
			installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

			rt := &resource.Runtime{
				RuntimeSpec: &resource.RuntimeSpec{
					Type:        resource.InstallTypeDelegation,
					Version:     "1.0.0",
					ToolBinPath: filepath.Join(tmpDir, "bin"),
					Bootstrap: &resource.RuntimeBootstrapSpec{
						CommandSet: resource.CommandSet{
							Install: []string{"install-cmd"},
							Check:   []string{"check-cmd"},
						},
						Update: tt.update,
					},
				},
			}

			ctx := executor.WithAction(context.Background(), tt.action)
			_, err := installer.Install(ctx, rt, "mock")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
		})
	}
}

func TestInstaller_Remove(t *testing.T) {
	t.Parallel()
	t.Run("successful remove with BinDir", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		// Create mock runtime installation
		installPath := filepath.Join(runtimesDir, "myruntime", "1.0.0")
		require.NoError(t, os.MkdirAll(filepath.Join(installPath, "bin"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(installPath, "bin", "mybin"), []byte("binary"), 0755))

		// Create symlink
		require.NoError(t, os.MkdirAll(binDir, 0755))
		require.NoError(t, os.Symlink(filepath.Join(installPath, "bin", "mybin"), filepath.Join(binDir, "mybin")))

		installer := NewInstaller(download.NewDownloader(), runtimesDir)

		state := &resource.RuntimeState{
			Version:     "1.0.0",
			InstallPath: installPath,
			Binaries:    []string{"mybin"},
			BinDir:      binDir,
		}

		err := installer.Remove(context.Background(), state, "myruntime")
		require.NoError(t, err)

		// Verify removal
		assert.NoDirExists(t, installPath)
		assert.NoFileExists(t, filepath.Join(binDir, "mybin"))
	})

	t.Run("delegation remove with command", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		runner := &mockCommandRunner{}
		installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

		st := &resource.RuntimeState{
			Type:          resource.InstallTypeDelegation,
			Version:       "1.0.0",
			RemoveCommand: []string{"remove-cmd"},
			Env:           map[string]string{"KEY": "val"},
		}

		err := installer.Remove(context.Background(), st, "mock")
		require.NoError(t, err)

		require.Len(t, runner.executeWithEnvCalls, 1)
		assert.Equal(t, []string{"remove-cmd"}, runner.executeWithEnvCalls[0].cmds)
		assert.Equal(t, map[string]string{"KEY": "val"}, runner.executeWithEnvCalls[0].env)
	})

	t.Run("delegation remove without command", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		runner := &mockCommandRunner{}
		installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

		st := &resource.RuntimeState{
			Type:    resource.InstallTypeDelegation,
			Version: "1.0.0",
		}

		err := installer.Remove(context.Background(), st, "mock")
		require.NoError(t, err)
		assert.Empty(t, runner.executeWithEnvCalls) // No command executed
	})

	t.Run("delegation remove command fails", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		runner := &mockCommandRunner{
			executeErr: fmt.Errorf("remove failed"),
		}
		installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

		st := &resource.RuntimeState{
			Type:          resource.InstallTypeDelegation,
			Version:       "1.0.0",
			RemoveCommand: []string{"remove-cmd"},
		}

		err := installer.Remove(context.Background(), st, "mock")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "bootstrap remove failed")
	})

	t.Run("successful remove without BinDir", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")

		// Create mock runtime installation
		installPath := filepath.Join(runtimesDir, "myruntime", "1.0.0")
		require.NoError(t, os.MkdirAll(filepath.Join(installPath, "bin"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(installPath, "bin", "mybin"), []byte("binary"), 0755))

		installer := NewInstaller(download.NewDownloader(), runtimesDir)

		state := &resource.RuntimeState{
			Version:     "1.0.0",
			InstallPath: installPath,
			Binaries:    []string{"mybin"},
			BinDir:      "", // No symlinks were created
		}

		err := installer.Remove(context.Background(), state, "myruntime")
		require.NoError(t, err)

		// Verify removal of install path
		assert.NoDirExists(t, installPath)
	})
}

func TestFindExtractedRoot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dirs      []string // directories to create
		files     []string // files to create
		wantIsDir bool     // true = expect a subdirectory, false = expect extractDir
		wantName  string   // expected subdirectory name (when wantIsDir is true)
	}{
		{
			name:      "single directory (Go pattern)",
			dirs:      []string{"go"},
			wantIsDir: true,
			wantName:  "go",
		},
		{
			name:  "flat binary (Deno pattern)",
			files: []string{"deno"},
		},
		{
			name:      "macOS ZIP (Bun pattern)",
			dirs:      []string{"bun-darwin-aarch64", "__MACOSX"},
			wantIsDir: true,
			wantName:  "bun-darwin-aarch64",
		},
		{
			name:      "sharkdp-style (bat pattern)",
			dirs:      []string{"bat-v0.26.0-x86_64"},
			files:     []string{"LICENSE", "README"},
			wantIsDir: true,
			wantName:  "bat-v0.26.0-x86_64",
		},
		{
			name:      "macOS ZIP + files",
			dirs:      []string{"myruntime", "__MACOSX"},
			files:     []string{"LICENSE"},
			wantIsDir: true,
			wantName:  "myruntime",
		},
		{
			name: "multiple real dirs",
			dirs: []string{"bin", "lib"},
		},
		{
			name:  "multiple dirs + files",
			dirs:  []string{"bin", "lib"},
			files: []string{"LICENSE"},
		},
		{
			name: "empty directory",
		},
		{
			name:      "hidden dir + real dir",
			dirs:      []string{".git", "myruntime"},
			wantIsDir: true,
			wantName:  "myruntime",
		},
		{
			name:  "files only",
			files: []string{"binary", "LICENSE"},
		},
		{
			name: "__MACOSX only",
			dirs: []string{"__MACOSX"},
		},
		{
			name:      "hidden file + real dir (existing regression)",
			dirs:      []string{"myruntime"},
			files:     []string{".hidden"},
			wantIsDir: true,
			wantName:  "myruntime",
		},
		{
			name: "multiple dirs (existing regression)",
			dirs: []string{"dir1", "dir2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()

			for _, d := range tt.dirs {
				require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, d), 0755))
			}
			for _, f := range tt.files {
				require.NoError(t, os.WriteFile(filepath.Join(tmpDir, f), []byte{}, 0644))
			}

			root, err := findExtractedRoot(tmpDir)
			require.NoError(t, err)

			if tt.wantIsDir {
				assert.Equal(t, filepath.Join(tmpDir, tt.wantName), root)
			} else {
				assert.Equal(t, tmpDir, root)
			}
		})
	}
}

func TestIsOSMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{name: "__MACOSX", input: "__MACOSX", want: true},
		{name: "regular name", input: "myruntime", want: false},
		{name: "lowercase __macosx", input: "__macosx", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isOSMetadata(tt.input))
		})
	}
}

func TestFindBinary(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	// Create bin/mybin
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "mybin"), []byte("binary"), 0755))

	// Create root-level binary
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "rootbin"), []byte("binary"), 0755))

	t.Run("find in bin directory", func(t *testing.T) {
		t.Parallel()
		path := findBinary(tmpDir, "mybin")
		assert.Equal(t, filepath.Join(tmpDir, "bin", "mybin"), path)
	})

	t.Run("find in root directory", func(t *testing.T) {
		t.Parallel()
		path := findBinary(tmpDir, "rootbin")
		assert.Equal(t, filepath.Join(tmpDir, "rootbin"), path)
	})

	t.Run("not found", func(t *testing.T) {
		t.Parallel()
		path := findBinary(tmpDir, "notexist")
		assert.Empty(t, path)
	})
}

func TestInstaller_Download_ResolveVersion(t *testing.T) {
	t.Parallel()
	binContent := []byte("#!/bin/sh\necho 'mock runtime'\n")
	tarGzContent := createRuntimeTarGz(t, "myruntime", []mockBinary{
		{name: "mybin", content: binContent},
	})
	archiveHash := sha256sum(tarGzContent)

	t.Run("download with resolveVersion success", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		runner := &mockCommandRunner{
			captureResult: "1.25.6",
		}
		dl := &mockRuntimeDownloader{archiveData: tarGzContent}
		installer := NewInstallerWithRunner(dl, runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest",
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.25.6.tar.gz",
					Checksum: &resource.Checksum{
						Value: "sha256:" + archiveHash,
					},
				},
				Binaries:       []string{"mybin"},
				BinDir:         binDir,
				ToolBinPath:    "~/myruntime/bin",
				ResolveVersion: []string{"echo 1.25.6"},
			},
		}

		state, err := installer.Install(context.Background(), rt, "myruntime")
		require.NoError(t, err)

		assert.Equal(t, "1.25.6", state.Version)
		assert.Equal(t, resource.VersionAlias, state.VersionKind)
		assert.Equal(t, "latest", state.SpecVersion)
		assert.Contains(t, state.InstallPath, "myruntime/1.25.6")

		// Verify resolve was called
		require.Len(t, runner.captureCalls, 1)
		assert.Equal(t, []string{"echo 1.25.6"}, runner.captureCalls[0].cmds)
	})

	t.Run("download with resolveVersion failure", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		runner := &mockCommandRunner{
			captureErr: fmt.Errorf("command not found"),
		}
		installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest",
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.0.0.tar.gz",
				},
				ToolBinPath:    "~/bin",
				ResolveVersion: []string{"bad-cmd"},
			},
		}

		_, err := installer.Install(context.Background(), rt, "myruntime")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to resolve version")
	})

	t.Run("download with resolveVersion returns empty", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()

		runner := &mockCommandRunner{
			captureResult: "",
		}
		installer := NewInstallerWithRunner(download.NewDownloader(), tmpDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest",
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.0.0.tar.gz",
				},
				ToolBinPath:    "~/bin",
				ResolveVersion: []string{"echo ''"},
			},
		}

		_, err := installer.Install(context.Background(), rt, "myruntime")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty result")
	})

	t.Run("download with resolveVersion already installed", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		// Pre-create installed directory
		installPath := filepath.Join(runtimesDir, "myruntime", "1.25.6")
		require.NoError(t, os.MkdirAll(filepath.Join(installPath, "bin"), 0755))
		require.NoError(t, os.WriteFile(filepath.Join(installPath, "bin", "mybin"), binContent, 0755))

		runner := &mockCommandRunner{
			captureResult: "1.25.6",
		}
		installer := NewInstallerWithRunner(download.NewDownloader(), runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest",
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.25.6.tar.gz",
				},
				Binaries:       []string{"mybin"},
				BinDir:         binDir,
				ToolBinPath:    "~/myruntime/bin",
				ResolveVersion: []string{"echo 1.25.6"},
			},
		}

		state, err := installer.Install(context.Background(), rt, "myruntime")
		require.NoError(t, err)

		// Should skip download but still return correct state
		assert.Equal(t, "1.25.6", state.Version)
		assert.Equal(t, resource.VersionAlias, state.VersionKind)
		assert.Equal(t, installPath, state.InstallPath)

		// Verify symlink was created
		linkPath := filepath.Join(binDir, "mybin")
		target, err := os.Readlink(linkPath)
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(installPath, "bin", "mybin"), target)
	})

	t.Run("download with env template expansion", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		runner := &mockCommandRunner{
			captureResult: "1.25.6",
		}
		dl := &mockRuntimeDownloader{archiveData: tarGzContent}
		installer := NewInstallerWithRunner(dl, runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest",
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.25.6.tar.gz",
					Checksum: &resource.Checksum{
						Value: "sha256:" + archiveHash,
					},
				},
				Binaries:       []string{"mybin"},
				BinDir:         binDir,
				ToolBinPath:    "~/myruntime/bin",
				ResolveVersion: []string{"echo 1.25.6"},
				Env: map[string]string{
					"MY_RUNTIME_HOME": "~/.local/share/tomei/runtimes/myruntime/{{.Version}}",
				},
			},
		}

		state, err := installer.Install(context.Background(), rt, "myruntime")
		require.NoError(t, err)

		// Env should have {{.Version}} expanded to resolved version, then ~ expanded
		home, _ := os.UserHomeDir()
		assert.Equal(t, filepath.Join(home, ".local/share/tomei/runtimes/myruntime/1.25.6"), state.Env["MY_RUNTIME_HOME"])
	})

	t.Run("download with github-release resolver", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		// Mock GitHub API via pure roundTripFunc (no httptest server)
		ghClient := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				if req.URL.Path == "/repos/oven-sh/bun/releases/latest" {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header:     http.Header{"Content-Type": []string{"application/json"}},
						Body:       io.NopCloser(strings.NewReader(`{"tag_name":"bun-v1.2.3"}`)),
					}, nil
				}
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			}),
		}

		dl := &mockRuntimeDownloader{archiveData: tarGzContent}
		installer := NewInstallerWithRunner(dl, runtimesDir, &mockCommandRunner{})
		installer.httpClient = ghClient

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest",
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.2.3.tar.gz",
					Checksum: &resource.Checksum{
						Value: "sha256:" + archiveHash,
					},
				},
				Binaries:       []string{"mybin"},
				BinDir:         binDir,
				ToolBinPath:    "~/bun/bin",
				ResolveVersion: []string{"github-release:oven-sh/bun:bun-v"},
			},
		}

		state, err := installer.Install(context.Background(), rt, "bun")
		require.NoError(t, err)

		assert.Equal(t, "1.2.3", state.Version)
		assert.Equal(t, resource.VersionAlias, state.VersionKind)
		assert.Equal(t, "latest", state.SpecVersion)
	})

	t.Run("download without resolveVersion uses spec version directly", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		dl := &mockRuntimeDownloader{archiveData: tarGzContent}
		installer := NewInstaller(dl, runtimesDir)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.0.0",
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.0.0.tar.gz",
					Checksum: &resource.Checksum{
						Value: "sha256:" + archiveHash,
					},
				},
				Binaries:    []string{"mybin"},
				BinDir:      binDir,
				ToolBinPath: "~/myruntime/bin",
				// No ResolveVersion — existing behavior
			},
		}

		state, err := installer.Install(context.Background(), rt, "myruntime")
		require.NoError(t, err)

		assert.Equal(t, "1.0.0", state.Version)
		assert.Equal(t, resource.VersionExact, state.VersionKind)
		assert.Equal(t, "1.0.0", state.SpecVersion)
	})
}

// TestInstaller_ResolveGitHubRelease tests GitHub release resolution through resolveVersionValue.
// The underlying logic is in the resolve package; this tests the integration path.
func TestInstaller_ResolveGitHubRelease(t *testing.T) {
	t.Parallel()

	t.Run("valid owner/repo with tagPrefix", func(t *testing.T) {
		t.Parallel()
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), &mockCommandRunner{})
		installer.httpClient = &http.Client{
			Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"tag_name": "bun-v1.2.3"}`)),
				}, nil
			}),
		}

		spec := &resource.RuntimeSpec{
			Version:        "latest",
			ResolveVersion: []string{"github-release:oven-sh/bun:bun-v"},
		}
		version, kind, err := installer.resolveVersionValue(context.Background(), spec)
		require.NoError(t, err)
		assert.Equal(t, "1.2.3", version)
		assert.Equal(t, resource.VersionAlias, kind)
	})

	t.Run("missing slash in owner/repo", func(t *testing.T) {
		t.Parallel()
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), &mockCommandRunner{})

		spec := &resource.RuntimeSpec{
			Version:        "latest",
			ResolveVersion: []string{"github-release:no-slash"},
		}
		_, _, err := installer.resolveVersionValue(context.Background(), spec)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid github-release format")
	})
}

// TestInstaller_ResolveHTTPText tests HTTP text resolution through resolveVersionValue.
func TestInstaller_ResolveHTTPText(t *testing.T) {
	t.Parallel()

	t.Run("missing scheme separator", func(t *testing.T) {
		t.Parallel()
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), &mockCommandRunner{})

		spec := &resource.RuntimeSpec{
			Version:        "latest",
			ResolveVersion: []string{"http-text:not-a-url"},
		}
		_, _, err := installer.resolveVersionValue(context.Background(), spec)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing ://")
	})

	t.Run("URL without regex part", func(t *testing.T) {
		t.Parallel()
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), &mockCommandRunner{})

		spec := &resource.RuntimeSpec{
			Version:        "latest",
			ResolveVersion: []string{"http-text:https://example.com/version"},
		}
		_, _, err := installer.resolveVersionValue(context.Background(), spec)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected http-text:<URL>:<regex>")
	})
}

// TestResolveVersionValue tests the resolveVersionValue dispatch logic directly.
func TestResolveVersionValue(t *testing.T) {
	t.Parallel()

	t.Run("no resolveVersion returns spec version as-is", func(t *testing.T) {
		t.Parallel()
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), &mockCommandRunner{})

		spec := &resource.RuntimeSpec{Version: "1.26.0"}
		version, kind, err := installer.resolveVersionValue(context.Background(), spec)
		require.NoError(t, err)
		assert.Equal(t, "1.26.0", version)
		assert.Equal(t, resource.VersionExact, kind)
	})

	t.Run("no resolveVersion with empty version returns VersionLatest", func(t *testing.T) {
		t.Parallel()
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), &mockCommandRunner{})

		spec := &resource.RuntimeSpec{Version: ""}
		version, kind, err := installer.resolveVersionValue(context.Background(), spec)
		require.NoError(t, err)
		assert.Empty(t, version)
		assert.Equal(t, resource.VersionLatest, kind)
	})

	t.Run("exact version skips shell resolver", func(t *testing.T) {
		t.Parallel()
		runner := &mockCommandRunner{captureResult: "should-not-be-called"}
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), runner)

		spec := &resource.RuntimeSpec{
			Version:        "1.26.0",
			ResolveVersion: []string{"echo should-not-run"},
		}
		version, kind, err := installer.resolveVersionValue(context.Background(), spec)
		require.NoError(t, err)
		assert.Equal(t, "1.26.0", version)
		assert.Equal(t, resource.VersionExact, kind)
		assert.Empty(t, runner.captureCalls, "shell command should not be called for exact version")
	})

	t.Run("v-prefixed exact version skips resolver", func(t *testing.T) {
		t.Parallel()
		runner := &mockCommandRunner{captureResult: "should-not-be-called"}
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), runner)

		spec := &resource.RuntimeSpec{
			Version:        "v2.1.4",
			ResolveVersion: []string{"echo should-not-run"},
		}
		version, kind, err := installer.resolveVersionValue(context.Background(), spec)
		require.NoError(t, err)
		assert.Equal(t, "v2.1.4", version)
		assert.Equal(t, resource.VersionExact, kind)
		assert.Empty(t, runner.captureCalls)
	})

	t.Run("exact version skips http-text resolver", func(t *testing.T) {
		t.Parallel()
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), &mockCommandRunner{})

		spec := &resource.RuntimeSpec{
			Version:        "2.6.10",
			ResolveVersion: []string{"http-text:https://example.com/version:^v(.+)"},
		}
		version, kind, err := installer.resolveVersionValue(context.Background(), spec)
		require.NoError(t, err)
		assert.Equal(t, "2.6.10", version)
		assert.Equal(t, resource.VersionExact, kind)
	})

	t.Run("exact version skips github-release resolver", func(t *testing.T) {
		t.Parallel()
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), &mockCommandRunner{})

		spec := &resource.RuntimeSpec{
			Version:        "1.2.21",
			ResolveVersion: []string{"github-release:oven-sh/bun:bun-v"},
		}
		version, kind, err := installer.resolveVersionValue(context.Background(), spec)
		require.NoError(t, err)
		assert.Equal(t, "1.2.21", version)
		assert.Equal(t, resource.VersionExact, kind)
	})

	t.Run("latest triggers shell resolver", func(t *testing.T) {
		t.Parallel()
		runner := &mockCommandRunner{captureResult: "1.26.0"}
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), runner)

		spec := &resource.RuntimeSpec{
			Version:        "latest",
			ResolveVersion: []string{"echo 1.26.0"},
		}
		version, kind, err := installer.resolveVersionValue(context.Background(), spec)
		require.NoError(t, err)
		assert.Equal(t, "1.26.0", version)
		assert.Equal(t, resource.VersionAlias, kind)
		require.Len(t, runner.captureCalls, 1)
	})

	t.Run("stable triggers shell resolver", func(t *testing.T) {
		t.Parallel()
		runner := &mockCommandRunner{captureResult: "1.83.0"}
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), runner)

		spec := &resource.RuntimeSpec{
			Version:        "stable",
			ResolveVersion: []string{"resolve-stable"},
		}
		version, kind, err := installer.resolveVersionValue(context.Background(), spec)
		require.NoError(t, err)
		assert.Equal(t, "1.83.0", version)
		assert.Equal(t, resource.VersionAlias, kind)
	})

	t.Run("shell resolver returns empty error", func(t *testing.T) {
		t.Parallel()
		runner := &mockCommandRunner{captureResult: ""}
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), runner)

		spec := &resource.RuntimeSpec{
			Version:        "latest",
			ResolveVersion: []string{"echo ''"},
		}
		_, _, err := installer.resolveVersionValue(context.Background(), spec)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty result")
	})

	t.Run("shell resolver command fails", func(t *testing.T) {
		t.Parallel()
		runner := &mockCommandRunner{captureErr: fmt.Errorf("command not found")}
		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), runner)

		spec := &resource.RuntimeSpec{
			Version:        "latest",
			ResolveVersion: []string{"bad-cmd"},
		}
		_, _, err := installer.resolveVersionValue(context.Background(), spec)
		require.Error(t, err)
	})
}

// TestInstaller_ResolveVersionValue_ExactSkip tests exact version skip via the full Install path.
func TestInstaller_ResolveVersionValue_ExactSkip(t *testing.T) {
	t.Parallel()

	binContent := []byte("#!/bin/sh\necho 'mock'\n")

	t.Run("exact version skips resolution even with resolveVersion configured", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		tarGzContent := createRuntimeTarGz(t, "myruntime", []mockBinary{
			{name: "mybin", content: binContent},
		})
		archiveHash := sha256sum(tarGzContent)

		// Runner should NOT be called for version resolution
		runner := &mockCommandRunner{
			captureResult: "should-not-be-called",
		}
		dl := &mockRuntimeDownloader{archiveData: tarGzContent}
		installer := NewInstallerWithRunner(dl, runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.26.0", // exact version
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-{{.Version}}.tar.gz",
					Checksum: &resource.Checksum{
						Value: "sha256:" + archiveHash,
					},
				},
				Binaries:       []string{"mybin"},
				BinDir:         binDir,
				ToolBinPath:    "~/myruntime/bin",
				ResolveVersion: []string{"echo should-not-run"},
			},
		}

		state, err := installer.Install(context.Background(), rt, "myruntime")
		require.NoError(t, err)

		// Version should be used as-is without resolution
		assert.Equal(t, "1.26.0", state.Version)
		assert.Equal(t, resource.VersionExact, state.VersionKind)
		assert.Equal(t, "1.26.0", state.SpecVersion)
		// Resolve command should NOT have been called
		assert.Empty(t, runner.captureCalls, "resolve command should not be called for exact versions")
	})

	t.Run("v-prefixed exact version skips resolution via Install", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		tarGzContent := createRuntimeTarGz(t, "exact-vrt", []mockBinary{
			{name: "mybin", content: binContent},
		})
		archiveHash := sha256sum(tarGzContent)

		runner := &mockCommandRunner{captureResult: "should-not-be-called"}
		dl := &mockRuntimeDownloader{archiveData: tarGzContent}
		installer := NewInstallerWithRunner(dl, runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "v2.1.4", // v-prefixed exact
				Source: &resource.DownloadSource{
					URL: "https://example.com/rt-{{.Version}}.tar.gz",
					Checksum: &resource.Checksum{
						Value: "sha256:" + archiveHash,
					},
				},
				Binaries:       []string{"mybin"},
				BinDir:         binDir,
				ToolBinPath:    "~/rt/bin",
				ResolveVersion: []string{"http-text:https://example.com/v:^v(.+)"},
			},
		}

		state, err := installer.Install(context.Background(), rt, "vrt")
		require.NoError(t, err)
		assert.Equal(t, "v2.1.4", state.Version)
		assert.Equal(t, resource.VersionExact, state.VersionKind)
		assert.Empty(t, runner.captureCalls)
	})

	t.Run("exact version with env template expansion", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		tarGzContent := createRuntimeTarGz(t, "envrt", []mockBinary{
			{name: "mybin", content: binContent},
		})
		archiveHash := sha256sum(tarGzContent)

		runner := &mockCommandRunner{}
		dl := &mockRuntimeDownloader{archiveData: tarGzContent}
		installer := NewInstallerWithRunner(dl, runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.26.0", // exact
				Source: &resource.DownloadSource{
					URL: "https://example.com/rt-{{.Version}}.tar.gz",
					Checksum: &resource.Checksum{
						Value: "sha256:" + archiveHash,
					},
				},
				Binaries:       []string{"mybin"},
				BinDir:         binDir,
				ToolBinPath:    "~/rt/bin",
				ResolveVersion: []string{"echo should-not-run"},
				Env: map[string]string{
					"MY_HOME": "~/.local/share/runtimes/{{.Version}}",
				},
			},
		}

		state, err := installer.Install(context.Background(), rt, "envrt")
		require.NoError(t, err)

		// Env {{.Version}} should expand to exact version
		home, _ := os.UserHomeDir()
		assert.Equal(t, filepath.Join(home, ".local/share/runtimes/1.26.0"), state.Env["MY_HOME"])
		assert.Empty(t, runner.captureCalls, "resolver should not be called")
	})

	t.Run("alias version triggers resolution", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		tarGzContent := createRuntimeTarGz(t, "myruntime", []mockBinary{
			{name: "mybin", content: binContent},
		})
		archiveHash := sha256sum(tarGzContent)

		runner := &mockCommandRunner{
			captureResult: "1.26.0",
		}
		dl := &mockRuntimeDownloader{archiveData: tarGzContent}
		installer := NewInstallerWithRunner(dl, runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest", // alias version
				Source: &resource.DownloadSource{
					URL: "https://example.com/myruntime-1.26.0.tar.gz",
					Checksum: &resource.Checksum{
						Value: "sha256:" + archiveHash,
					},
				},
				Binaries:       []string{"mybin"},
				BinDir:         binDir,
				ToolBinPath:    "~/myruntime/bin",
				ResolveVersion: []string{"echo 1.26.0"},
			},
		}

		state, err := installer.Install(context.Background(), rt, "myruntime")
		require.NoError(t, err)

		assert.Equal(t, "1.26.0", state.Version)
		assert.Equal(t, resource.VersionAlias, state.VersionKind)
		assert.Equal(t, "latest", state.SpecVersion)
		// Resolve command SHOULD have been called
		require.Len(t, runner.captureCalls, 1, "resolve command should be called for alias versions")
	})
}

func TestExpandVersionTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		tmpl    string
		version string
		want    string
		wantErr bool
	}{
		{
			name:    "no template markers",
			tmpl:    "https://example.com/download/v1.0.0.tar.gz",
			version: "2.0.0",
			want:    "https://example.com/download/v1.0.0.tar.gz",
		},
		{
			name:    "with Version template",
			tmpl:    "https://go.dev/dl/go{{.Version}}.linux-amd64.tar.gz",
			version: "1.25.6",
			want:    "https://go.dev/dl/go1.25.6.linux-amd64.tar.gz",
		},
		{
			name:    "multiple Version references",
			tmpl:    "https://example.com/{{.Version}}/download-{{.Version}}.tar.gz",
			version: "1.0.0",
			want:    "https://example.com/1.0.0/download-1.0.0.tar.gz",
		},
		{
			name:    "empty version",
			tmpl:    "https://example.com/{{.Version}}/file.tar.gz",
			version: "",
			want:    "https://example.com//file.tar.gz",
		},
		{
			name:    "env template",
			tmpl:    "~/.local/share/tomei/runtimes/go/{{.Version}}",
			version: "1.25.6",
			want:    "~/.local/share/tomei/runtimes/go/1.25.6",
		},
		{
			name:    "invalid template",
			tmpl:    "{{.Invalid",
			version: "1.0.0",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := expandVersionTemplate(tt.tmpl, tt.version)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// roundTripFunc is a helper for mocking http.RoundTripper in tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRuntimeInstaller_ProgressCallback_Priority(t *testing.T) {
	t.Parallel()
	binContent := []byte("#!/bin/sh\necho 'mock runtime'\n")
	archiveData := createRuntimeTarGz(t, "myruntime", []mockBinary{
		{name: "mybin", content: binContent},
	})

	makeRuntime := func() *resource.Runtime {
		return &resource.Runtime{
			BaseResource: resource.BaseResource{
				APIVersion:   resource.GroupVersion,
				ResourceKind: resource.KindRuntime,
				Metadata:     resource.Metadata{Name: "myruntime"},
			},
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.0.0",
				Source: &resource.DownloadSource{
					URL:      "https://example.com/myruntime-1.0.0.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:dummy"},
				},
				Binaries: []string{"mybin"},
			},
		}
	}

	tests := []struct {
		name          string
		fieldCallback bool
		ctxCallback   bool
		wantField     bool
		wantCtx       bool
	}{
		{
			name:          "context callback preferred over field",
			fieldCallback: true,
			ctxCallback:   true,
			wantField:     false,
			wantCtx:       true,
		},
		{
			name:          "field callback used when no context callback",
			fieldCallback: true,
			ctxCallback:   false,
			wantField:     true,
			wantCtx:       false,
		},
		{
			name:          "no callback - nil passed to downloader",
			fieldCallback: false,
			ctxCallback:   false,
			wantField:     false,
			wantCtx:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tmpDir := t.TempDir()
			runtimesDir := filepath.Join(tmpDir, "runtimes")

			dl := &mockRuntimeDownloader{archiveData: archiveData}
			installer := NewInstaller(dl, runtimesDir)

			var fieldCalled, ctxCalled bool

			if tt.fieldCallback {
				installer.SetProgressCallback(func(_, _ int64) { fieldCalled = true })
			}

			ctx := context.Background()
			if tt.ctxCallback {
				ctx = download.WithCallback(ctx, download.ProgressCallback(func(_, _ int64) { ctxCalled = true }))
			}

			_, err := installer.Install(ctx, makeRuntime(), "myruntime")
			require.NoError(t, err)

			assert.Equal(t, tt.wantField, fieldCalled, "field callback")
			assert.Equal(t, tt.wantCtx, ctxCalled, "context callback")

			if !tt.fieldCallback && !tt.ctxCallback {
				assert.Nil(t, dl.lastProgressCallback, "callback should be nil")
			}
		})
	}
}

// --- mockCommandRunner ---

type cmdCall struct {
	cmds []string
	vars command.Vars
	env  map[string]string
}

type mockCommandRunner struct {
	executeErr             error
	executeWithOutputErr   error
	captureResult          string
	captureErr             error
	checkResult            bool
	executeWithEnvCalls    []cmdCall
	executeWithOutputCalls []cmdCall
	captureCalls           []cmdCall
	checkCalls             []cmdCall
}

func (m *mockCommandRunner) ExecuteWithEnv(_ context.Context, cmds []string, vars command.Vars, env map[string]string) error {
	m.executeWithEnvCalls = append(m.executeWithEnvCalls, cmdCall{cmds: cmds, vars: vars, env: env})
	return m.executeErr
}

func (m *mockCommandRunner) ExecuteWithOutput(_ context.Context, cmds []string, vars command.Vars, env map[string]string, callback command.OutputCallback) error {
	m.executeWithOutputCalls = append(m.executeWithOutputCalls, cmdCall{cmds: cmds, vars: vars, env: env})
	if callback != nil {
		callback("mock output line")
	}
	if m.executeWithOutputErr != nil {
		return m.executeWithOutputErr
	}
	return m.executeErr
}

func (m *mockCommandRunner) ExecuteCapture(_ context.Context, cmds []string, vars command.Vars, env map[string]string) (string, error) {
	m.captureCalls = append(m.captureCalls, cmdCall{cmds: cmds, vars: vars, env: env})
	return m.captureResult, m.captureErr
}

func (m *mockCommandRunner) Check(_ context.Context, cmds []string, vars command.Vars, env map[string]string) bool {
	m.checkCalls = append(m.checkCalls, cmdCall{cmds: cmds, vars: vars, env: env})
	return m.checkResult
}

// Helper types and functions

type mockBinary struct {
	name    string
	content []byte
}

// createRuntimeTarGz creates a tar.gz archive with a top-level directory containing binaries.
func createRuntimeTarGz(t *testing.T, name string, binaries []mockBinary) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	// Create top-level directory
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     name + "/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}))

	// Create bin directory
	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name:     name + "/bin/",
		Mode:     0755,
		Typeflag: tar.TypeDir,
	}))

	// Create binaries
	for _, bin := range binaries {
		hdr := &tar.Header{
			Name: name + "/bin/" + bin.name,
			Mode: 0755,
			Size: int64(len(bin.content)),
		}
		require.NoError(t, tw.WriteHeader(hdr))
		_, err := tw.Write(bin.content)
		require.NoError(t, err)
	}

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return buf.Bytes()
}

func sha256sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// mockRuntimeDownloader records whether a progress callback was passed.
type mockRuntimeDownloader struct {
	archiveData          []byte
	lastProgressCallback download.ProgressCallback
}

func (m *mockRuntimeDownloader) Download(_ context.Context, _, destPath string) (string, error) {
	if err := os.WriteFile(destPath, m.archiveData, 0644); err != nil {
		return "", err
	}
	return destPath, nil
}

func (m *mockRuntimeDownloader) DownloadWithProgress(_ context.Context, _, destPath string, callback download.ProgressCallback) (string, error) {
	m.lastProgressCallback = callback
	if callback != nil {
		callback(100, 200)
	}
	if err := os.WriteFile(destPath, m.archiveData, 0644); err != nil {
		return "", err
	}
	return destPath, nil
}

func (m *mockRuntimeDownloader) Verify(_ context.Context, _ string, _ *resource.Checksum) error {
	return nil
}
