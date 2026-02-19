package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/download"
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			_, _ = w.Write([]byte(archiveHash + "  myruntime.tar.gz\n"))
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			_, _ = w.Write(tarGzContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Run("successful install with BinDir", func(t *testing.T) {
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		downloader := download.NewDownloader()
		installer := NewInstaller(downloader, runtimesDir)

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
					URL: server.URL + "/myruntime-1.0.0.tar.gz",
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
		assert.Equal(t, archiveHash, state.Digest)
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

		downloader := download.NewDownloader()
		installer := NewInstaller(downloader, runtimesDir)

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
					URL: server.URL + "/myruntime-1.0.0.tar.gz",
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

		downloader := download.NewDownloader()
		installer := NewInstaller(downloader, runtimesDir)

		runtime := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.0.0",
				Source: &resource.DownloadSource{
					URL: server.URL + "/myruntime-1.0.0.tar.gz",
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

		downloader := download.NewDownloader()
		installer := NewInstaller(downloader, runtimesDir)

		// Install (downgrade) to 1.0.0 — directory already exists
		runtime := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.0.0",
				Source: &resource.DownloadSource{
					URL: server.URL + "/myruntime-1.0.0.tar.gz",
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
	t.Run("single directory", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		subDir := filepath.Join(tmpDir, "myruntime")
		require.NoError(t, os.MkdirAll(subDir, 0755))

		root, err := findExtractedRoot(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, subDir, root)
	})

	t.Run("multiple entries", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "dir1"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "dir2"), 0755))

		root, err := findExtractedRoot(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, tmpDir, root)
	})

	t.Run("hidden files ignored", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		subDir := filepath.Join(tmpDir, "myruntime")
		require.NoError(t, os.MkdirAll(subDir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".hidden"), []byte{}, 0644))

		root, err := findExtractedRoot(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, subDir, root)
	})
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

func TestInstaller_Download_ResolveVersion(t *testing.T) {
	t.Parallel()
	binContent := []byte("#!/bin/sh\necho 'mock runtime'\n")
	tarGzContent := createRuntimeTarGz(t, "myruntime", []mockBinary{
		{name: "mybin", content: binContent},
	})
	archiveHash := sha256sum(tarGzContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			_, _ = w.Write([]byte(archiveHash + "  myruntime.tar.gz\n"))
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			_, _ = w.Write(tarGzContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	t.Run("download with resolveVersion success", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		runner := &mockCommandRunner{
			captureResult: "1.25.6",
		}
		downloader := download.NewDownloader()
		installer := NewInstallerWithRunner(downloader, runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest",
				Source: &resource.DownloadSource{
					URL: server.URL + "/myruntime-1.25.6.tar.gz",
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
					URL: server.URL + "/myruntime-1.0.0.tar.gz",
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
					URL: server.URL + "/myruntime-1.0.0.tar.gz",
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
					URL: server.URL + "/myruntime-1.25.6.tar.gz",
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
		downloader := download.NewDownloader()
		installer := NewInstallerWithRunner(downloader, runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest",
				Source: &resource.DownloadSource{
					URL: server.URL + "/myruntime-1.25.6.tar.gz",
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

		// Mock GitHub API server
		ghServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/repos/oven-sh/bun/releases/latest" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"tag_name":"bun-v1.2.3"}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer ghServer.Close()

		// Create HTTP client that redirects GitHub API to mock server
		ghClient := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = ghServer.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		}

		downloader := download.NewDownloader()
		installer := NewInstallerWithRunner(downloader, runtimesDir, &mockCommandRunner{})
		installer.httpClient = ghClient

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest",
				Source: &resource.DownloadSource{
					URL: server.URL + "/myruntime-1.2.3.tar.gz",
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

		downloader := download.NewDownloader()
		installer := NewInstaller(downloader, runtimesDir)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.0.0",
				Source: &resource.DownloadSource{
					URL: server.URL + "/myruntime-1.0.0.tar.gz",
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

func TestInstaller_ResolveHTTPText(t *testing.T) {
	t.Parallel()

	t.Run("success with capture group", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("go1.26.0\ngo1.25.6\n"))
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		cmd := fmt.Sprintf("http-text:%s:^go(.+)", server.URL)
		version, err := installer.resolveHTTPText(context.Background(), cmd)
		require.NoError(t, err)
		assert.Equal(t, "1.26.0", version)
	})

	t.Run("success without capture group", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("v2.1.4\n"))
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		cmd := fmt.Sprintf("http-text:%s:v[0-9.]+", server.URL)
		version, err := installer.resolveHTTPText(context.Background(), cmd)
		require.NoError(t, err)
		assert.Equal(t, "v2.1.4", version)
	})

	t.Run("match on non-first line", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// Header lines before the version
			_, _ = w.Write([]byte("# Latest release\nDate: 2026-02-19\nversion=3.14.0\n"))
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		cmd := fmt.Sprintf("http-text:%s:^version=(.+)", server.URL)
		version, err := installer.resolveHTTPText(context.Background(), cmd)
		require.NoError(t, err)
		assert.Equal(t, "3.14.0", version)
	})

	t.Run("URL with query parameters", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify the query parameter arrived
			assert.Equal(t, "text", r.URL.Query().Get("m"))
			_, _ = w.Write([]byte("go1.26.0\n"))
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		// Real-world pattern: go.dev/VERSION?m=text
		cmd := fmt.Sprintf("http-text:%s/VERSION?m=text:^go(.+)", server.URL)
		version, err := installer.resolveHTTPText(context.Background(), cmd)
		require.NoError(t, err)
		assert.Equal(t, "1.26.0", version)
	})

	t.Run("first match wins among multiple matches", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("release-1.0.0\nrelease-2.0.0\nrelease-3.0.0\n"))
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		cmd := fmt.Sprintf("http-text:%s:^release-(.+)", server.URL)
		version, err := installer.resolveHTTPText(context.Background(), cmd)
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", version, "should return first match, not last")
	})

	t.Run("empty body", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			// 200 OK with empty body
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		cmd := fmt.Sprintf("http-text:%s:^go(.+)", server.URL)
		_, err := installer.resolveHTTPText(context.Background(), cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no match")
	})

	t.Run("no match", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("no version here\n"))
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		cmd := fmt.Sprintf("http-text:%s:^go(.+)", server.URL)
		_, err := installer.resolveHTTPText(context.Background(), cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no match")
	})

	t.Run("invalid regex", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		cmd := "http-text:https://example.com:[invalid"
		_, err := installer.resolveHTTPText(context.Background(), cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid http-text regex")
	})

	t.Run("missing scheme separator", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		cmd := "http-text:not-a-url"
		_, err := installer.resolveHTTPText(context.Background(), cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing ://")
	})

	t.Run("URL without regex part", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		// Valid URL but no colon after the path to separate regex
		cmd := "http-text:https://example.com/version"
		_, err := installer.resolveHTTPText(context.Background(), cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected http-text:<URL>:<regex>")
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		cmd := fmt.Sprintf("http-text:%s:^go(.+)", server.URL)
		_, err := installer.resolveHTTPText(context.Background(), cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 500")
	})

	t.Run("404 not found", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		cmd := fmt.Sprintf("http-text:%s:^v(.+)", server.URL)
		_, err := installer.resolveHTTPText(context.Background(), cmd)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "status 404")
	})

	t.Run("uses custom httpClient when set", func(t *testing.T) {
		t.Parallel()
		var gotRequest bool
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			gotRequest = true
			_, _ = w.Write([]byte("v1.0.0\n"))
		}))
		defer server.Close()

		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)
		installer.httpClient = &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				// Redirect to mock server
				req.URL.Scheme = "http"
				req.URL.Host = server.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		}

		cmd := "http-text:http://redirected.example.com/version:^v(.+)"
		version, err := installer.resolveHTTPText(context.Background(), cmd)
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", version)
		assert.True(t, gotRequest)
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

	t.Run("http-text dispatch for alias version", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("go1.26.0\n"))
		}))
		defer server.Close()

		installer := NewInstallerWithRunner(download.NewDownloader(), t.TempDir(), &mockCommandRunner{})

		spec := &resource.RuntimeSpec{
			Version:        "latest",
			ResolveVersion: []string{fmt.Sprintf("http-text:%s:^go(.+)", server.URL)},
		}
		version, kind, err := installer.resolveVersionValue(context.Background(), spec)
		require.NoError(t, err)
		assert.Equal(t, "1.26.0", version)
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

	t.Run("exact version skips resolution even with resolveVersion configured", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		binContent := []byte("#!/bin/sh\necho 'mock'\n")
		tarGzContent := createRuntimeTarGz(t, "myruntime", []mockBinary{
			{name: "mybin", content: binContent},
		})
		archiveHash := sha256sum(tarGzContent)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, ".tar.gz"):
				_, _ = w.Write(tarGzContent)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		// Runner should NOT be called for version resolution
		runner := &mockCommandRunner{
			captureResult: "should-not-be-called",
		}
		downloader := download.NewDownloader()
		installer := NewInstallerWithRunner(downloader, runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.26.0", // exact version
				Source: &resource.DownloadSource{
					URL: server.URL + "/myruntime-{{.Version}}.tar.gz",
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

		binContent := []byte("#!/bin/sh\necho 'mock'\n")
		tarGzContent := createRuntimeTarGz(t, "exact-vrt", []mockBinary{
			{name: "mybin", content: binContent},
		})
		archiveHash := sha256sum(tarGzContent)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, ".tar.gz") {
				_, _ = w.Write(tarGzContent)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		runner := &mockCommandRunner{captureResult: "should-not-be-called"}
		installer := NewInstallerWithRunner(download.NewDownloader(), runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "v2.1.4", // v-prefixed exact
				Source: &resource.DownloadSource{
					URL: server.URL + "/rt-{{.Version}}.tar.gz",
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

		binContent := []byte("#!/bin/sh\necho 'mock'\n")
		tarGzContent := createRuntimeTarGz(t, "envrt", []mockBinary{
			{name: "mybin", content: binContent},
		})
		archiveHash := sha256sum(tarGzContent)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, ".tar.gz") {
				_, _ = w.Write(tarGzContent)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()

		runner := &mockCommandRunner{}
		installer := NewInstallerWithRunner(download.NewDownloader(), runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.26.0", // exact
				Source: &resource.DownloadSource{
					URL: server.URL + "/rt-{{.Version}}.tar.gz",
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

		binContent := []byte("#!/bin/sh\necho 'mock'\n")
		tarGzContent := createRuntimeTarGz(t, "myruntime", []mockBinary{
			{name: "mybin", content: binContent},
		})
		archiveHash := sha256sum(tarGzContent)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, ".tar.gz"):
				_, _ = w.Write(tarGzContent)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}))
		defer server.Close()

		runner := &mockCommandRunner{
			captureResult: "1.26.0",
		}
		downloader := download.NewDownloader()
		installer := NewInstallerWithRunner(downloader, runtimesDir, runner)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest", // alias version
				Source: &resource.DownloadSource{
					URL: server.URL + "/myruntime-1.26.0.tar.gz",
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

func TestInstaller_Download_HTTPTextResolver(t *testing.T) {
	t.Parallel()

	binContent := []byte("#!/bin/sh\necho 'mock runtime'\n")
	tarGzContent := createRuntimeTarGz(t, "httptext-rt", []mockBinary{
		{name: "mybin", content: binContent},
	})
	archiveHash := sha256sum(tarGzContent)

	t.Run("http-text resolver integration", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		runtimesDir := filepath.Join(tmpDir, "runtimes")
		binDir := filepath.Join(tmpDir, "bin")

		// Version endpoint
		versionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("go1.26.0\ngo1.25.6\n"))
		}))
		defer versionServer.Close()

		// Archive endpoint
		archiveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, ".tar.gz") {
				_, _ = w.Write(tarGzContent)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}))
		defer archiveServer.Close()

		downloader := download.NewDownloader()
		installer := NewInstaller(downloader, runtimesDir)

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "latest",
				Source: &resource.DownloadSource{
					URL: archiveServer.URL + "/myruntime-1.26.0.tar.gz",
					Checksum: &resource.Checksum{
						Value: "sha256:" + archiveHash,
					},
				},
				Binaries:       []string{"mybin"},
				BinDir:         binDir,
				ToolBinPath:    "~/myruntime/bin",
				ResolveVersion: []string{fmt.Sprintf("http-text:%s:^go(.+)", versionServer.URL)},
			},
		}

		state, err := installer.Install(context.Background(), rt, "myruntime")
		require.NoError(t, err)

		assert.Equal(t, "1.26.0", state.Version)
		assert.Equal(t, resource.VersionAlias, state.VersionKind)
		assert.Equal(t, "latest", state.SpecVersion)
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
