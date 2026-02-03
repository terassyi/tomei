package runtime

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/installer/download"
	"github.com/terassyi/toto/internal/resource"
)

func TestNewInstaller(t *testing.T) {
	downloader := download.NewDownloader()
	installer := NewInstaller(downloader, "/runtimes")

	assert.NotNil(t, installer)
	assert.Equal(t, "/runtimes", installer.runtimesDir)
}

func TestInstaller_Install(t *testing.T) {
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
					"MY_RUNTIME_HOME": "~/.local/share/toto/runtimes/myruntime/1.0.0",
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
		assert.Equal(t, filepath.Join(home, ".local/share/toto/runtimes/myruntime/1.0.0"), state.Env["MY_RUNTIME_HOME"])

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

		// Pre-create the install directory
		installPath := filepath.Join(runtimesDir, "myruntime", "1.0.0")
		require.NoError(t, os.MkdirAll(installPath, 0755))

		downloader := download.NewDownloader()
		installer := NewInstaller(downloader, runtimesDir)

		runtime := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:    resource.InstallTypeDownload,
				Version: "1.0.0",
				Source: &resource.DownloadSource{
					URL: server.URL + "/myruntime-1.0.0.tar.gz",
				},
				Binaries:    []string{"mybin"},
				ToolBinPath: "~/bin",
			},
		}

		state, err := installer.Install(context.Background(), runtime, "myruntime")
		require.NoError(t, err)
		assert.Equal(t, installPath, state.InstallPath)
	})

	t.Run("delegation pattern not supported yet", func(t *testing.T) {
		tmpDir := t.TempDir()
		installer := NewInstaller(download.NewDownloader(), tmpDir)

		runtime := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:        resource.InstallTypeDelegation,
				Version:     "stable",
				ToolBinPath: "~/.cargo/bin",
				Bootstrap: &resource.RuntimeBootstrapSpec{
					CommandSet: resource.CommandSet{
						Install: "curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y",
						Check:   "rustc --version",
					},
				},
			},
		}

		_, err := installer.Install(context.Background(), runtime, "rust")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})

	t.Run("missing source URL", func(t *testing.T) {
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
	t.Run("successful remove with BinDir", func(t *testing.T) {
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

	t.Run("successful remove without BinDir", func(t *testing.T) {
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
	t.Run("single directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		subDir := filepath.Join(tmpDir, "myruntime")
		require.NoError(t, os.MkdirAll(subDir, 0755))

		root, err := findExtractedRoot(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, subDir, root)
	})

	t.Run("multiple entries", func(t *testing.T) {
		tmpDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "dir1"), 0755))
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "dir2"), 0755))

		root, err := findExtractedRoot(tmpDir)
		require.NoError(t, err)
		assert.Equal(t, tmpDir, root)
	})

	t.Run("hidden files ignored", func(t *testing.T) {
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
	tmpDir := t.TempDir()

	// Create bin/mybin
	binDir := filepath.Join(tmpDir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "mybin"), []byte("binary"), 0755))

	// Create root-level binary
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "rootbin"), []byte("binary"), 0755))

	t.Run("find in bin directory", func(t *testing.T) {
		path := findBinary(tmpDir, "mybin")
		assert.Equal(t, filepath.Join(tmpDir, "bin", "mybin"), path)
	})

	t.Run("find in root directory", func(t *testing.T) {
		path := findBinary(tmpDir, "rootbin")
		assert.Equal(t, filepath.Join(tmpDir, "rootbin"), path)
	})

	t.Run("not found", func(t *testing.T) {
		path := findBinary(tmpDir, "notexist")
		assert.Empty(t, path)
	})
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
