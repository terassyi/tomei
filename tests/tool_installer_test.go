//go:build integration

package tests

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

	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/executor"
	"github.com/terassyi/tomei/internal/installer/place"
	"github.com/terassyi/tomei/internal/registry/aqua"
	"github.com/terassyi/tomei/internal/resource"

	toolpkg "github.com/terassyi/tomei/internal/installer/tool"
)

// TestToolInstaller_Install_HTTP tests tool installation with real HTTP download.
func TestToolInstaller_Install_HTTP(t *testing.T) {
	t.Parallel()
	binaryContent := []byte("#!/bin/sh\necho hello")
	tarGzContent := createToolTestTarGz(t, "ripgrep", binaryContent)
	archiveHash := toolSha256Hash(tarGzContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(archiveHash + "  ripgrep.tar.gz\n"))
			return
		}
		_, _ = w.Write(tarGzContent)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	binDir := filepath.Join(tmpDir, "bin")

	dl := download.NewDownloader()
	placer := place.NewPlacer(toolsDir, binDir)
	installer := toolpkg.NewInstaller(dl, placer)

	tool := &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "ripgrep"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "download",
			Version:      "14.1.1",
			Source: &resource.DownloadSource{
				URL: server.URL + "/ripgrep.tar.gz",
				Checksum: &resource.Checksum{
					Value: "sha256:" + archiveHash,
				},
				ArchiveType: "tar.gz",
			},
		},
	}

	state, err := installer.Install(context.Background(), tool, "ripgrep")
	require.NoError(t, err)
	assert.NotNil(t, state)
	assert.Equal(t, "download", state.InstallerRef)
	assert.Equal(t, "14.1.1", state.Version)
	assert.NotEmpty(t, state.InstallPath)
	assert.NotEmpty(t, state.BinPath)
	assert.NotEmpty(t, state.Digest)

	// Verify binary exists
	_, err = os.Stat(state.InstallPath)
	require.NoError(t, err)

	// Verify symlink exists
	_, err = os.Lstat(state.BinPath)
	require.NoError(t, err)
}

// TestToolInstaller_InstallFromRegistry_HTTP tests registry-based tool installation
// with real HTTP communication.
func TestToolInstaller_InstallFromRegistry_HTTP(t *testing.T) {
	t.Parallel()
	binaryContent := []byte("#!/bin/sh\necho hello from registry")
	tarGzContent := createToolTestTarGz(t, "mytool", binaryContent)

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/pkgs/test/mytool/registry.yaml"):
			registryYAML := `packages:
  - type: http
    repo_owner: test
    repo_name: mytool
    url: "` + serverURL + `/releases/mytool_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz"
    format: tar.gz
`
			_, _ = w.Write([]byte(registryYAML))
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			_, _ = w.Write(tarGzContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	binDir := filepath.Join(tmpDir, "bin")
	cacheDir := filepath.Join(tmpDir, "cache")

	dl := download.NewDownloader()
	placer := place.NewPlacer(toolsDir, binDir)
	installer := toolpkg.NewInstaller(dl, placer)

	resolver := aqua.NewResolverWithBaseURL(cacheDir, server.URL)
	installer.SetResolver(resolver, "v4.465.0")

	tool := &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "mytool"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "1.0.0",
			Package: &resource.Package{
				Owner: "test",
				Repo:  "mytool",
			},
		},
	}

	state, err := installer.Install(context.Background(), tool, "mytool")
	require.NoError(t, err)
	require.NotNil(t, state)

	assert.Equal(t, "aqua", state.InstallerRef)
	assert.Equal(t, "1.0.0", state.Version)
	assert.NotEmpty(t, state.InstallPath)
	assert.NotEmpty(t, state.BinPath)
	assert.Equal(t, "test", state.Package.Owner)
	assert.Equal(t, "mytool", state.Package.Repo)

	_, err = os.Stat(state.InstallPath)
	require.NoError(t, err)
}

// TestToolInstaller_Install_HTTP_BinaryName tests that binaryName override works
// for the download pattern: symlink and state use the overridden name.
func TestToolInstaller_Install_HTTP_BinaryName(t *testing.T) {
	t.Parallel()
	binaryContent := []byte("#!/bin/sh\necho kubectl-krew")
	// Archive contains binary named "krew" (default name), but we want symlink "kubectl-krew"
	tarGzContent := createToolTestTarGz(t, "krew", binaryContent)
	archiveHash := toolSha256Hash(tarGzContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(archiveHash + "  krew.tar.gz\n"))
			return
		}
		_, _ = w.Write(tarGzContent)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	binDir := filepath.Join(tmpDir, "bin")

	dl := download.NewDownloader()
	placer := place.NewPlacer(toolsDir, binDir)
	installer := toolpkg.NewInstaller(dl, placer)

	tool := &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "krew"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "download",
			Version:      "0.4.4",
			BinaryName:   "kubectl-krew",
			Source: &resource.DownloadSource{
				URL: server.URL + "/krew.tar.gz",
				Checksum: &resource.Checksum{
					Value: "sha256:" + archiveHash,
				},
				ArchiveType: "tar.gz",
			},
		},
	}

	state, err := installer.Install(context.Background(), tool, "krew")
	require.NoError(t, err)
	require.NotNil(t, state)

	// BinPath should use binaryName
	assert.Equal(t, filepath.Join(binDir, "kubectl-krew"), state.BinPath)

	// InstallPath should use binaryName
	assert.Equal(t, filepath.Join(toolsDir, "krew", "0.4.4", "kubectl-krew"), state.InstallPath)

	// Symlink should exist at binDir/kubectl-krew
	_, err = os.Lstat(state.BinPath)
	require.NoError(t, err)
}

// TestToolInstaller_InstallFromRegistry_HTTP_BinaryName tests that binaryName override
// takes priority over aqua registry files[].name.
func TestToolInstaller_InstallFromRegistry_HTTP_BinaryName(t *testing.T) {
	t.Parallel()
	binaryContent := []byte("#!/bin/sh\necho hello from registry")
	tarGzContent := createToolTestTarGz(t, "original-name", binaryContent)

	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/pkgs/test/mytool/registry.yaml"):
			registryYAML := `packages:
  - type: http
    repo_owner: test
    repo_name: mytool
    url: "` + serverURL + `/releases/mytool_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz"
    format: tar.gz
    files:
      - name: original-name
`
			_, _ = w.Write([]byte(registryYAML))
		case strings.HasSuffix(r.URL.Path, ".tar.gz"):
			_, _ = w.Write(tarGzContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	binDir := filepath.Join(tmpDir, "bin")
	cacheDir := filepath.Join(tmpDir, "cache")

	dl := download.NewDownloader()
	placer := place.NewPlacer(toolsDir, binDir)
	installer := toolpkg.NewInstaller(dl, placer)

	resolver := aqua.NewResolverWithBaseURL(cacheDir, server.URL)
	installer.SetResolver(resolver, "v4.465.0")

	tool := &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "mytool"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "1.0.0",
			BinaryName:   "override-name",
			Package: &resource.Package{
				Owner: "test",
				Repo:  "mytool",
			},
		},
	}

	state, err := installer.Install(context.Background(), tool, "mytool")
	require.NoError(t, err)
	require.NotNil(t, state)

	// BinPath should use spec.BinaryName (override-name), not registry files[].name (original-name)
	assert.Equal(t, filepath.Join(binDir, "override-name"), state.BinPath)
}

// TestToolInstaller_Install_HTTP_BinaryName_OldSymlinkCleanup tests that the old symlink
// is removed when binaryName changes on upgrade.
func TestToolInstaller_Install_HTTP_BinaryName_OldSymlinkCleanup(t *testing.T) {
	t.Parallel()
	binaryContent := []byte("#!/bin/sh\necho krew")
	tarGzContent := createToolTestTarGz(t, "krew", binaryContent)
	archiveHash := toolSha256Hash(tarGzContent)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			_, _ = w.Write([]byte(archiveHash + "  krew.tar.gz\n"))
			return
		}
		_, _ = w.Write(tarGzContent)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	binDir := filepath.Join(tmpDir, "bin")

	dl := download.NewDownloader()
	placer := place.NewPlacer(toolsDir, binDir)
	installer := toolpkg.NewInstaller(dl, placer)

	// First install: no binaryName override → symlink at binDir/krew
	tool1 := &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "krew"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "download",
			Version:      "0.4.4",
			Source: &resource.DownloadSource{
				URL: server.URL + "/krew.tar.gz",
				Checksum: &resource.Checksum{
					Value: "sha256:" + archiveHash,
				},
				ArchiveType: "tar.gz",
			},
		},
	}

	state1, err := installer.Install(context.Background(), tool1, "krew")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(binDir, "krew"), state1.BinPath)

	// Verify old symlink exists
	_, err = os.Lstat(filepath.Join(binDir, "krew"))
	require.NoError(t, err)

	// Second install: binaryName override → symlink at binDir/kubectl-krew
	// Pass old BinPath via context to simulate upgrade
	tool2 := &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "krew"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "download",
			Version:      "0.4.4",
			BinaryName:   "kubectl-krew",
			Source: &resource.DownloadSource{
				URL: server.URL + "/krew.tar.gz",
				Checksum: &resource.Checksum{
					Value: "sha256:" + archiveHash,
				},
				ArchiveType: "tar.gz",
			},
		},
	}

	ctx := executor.WithOldBinPath(context.Background(), state1.BinPath)
	state2, err := installer.Install(ctx, tool2, "krew")
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(binDir, "kubectl-krew"), state2.BinPath)

	// New symlink should exist
	_, err = os.Lstat(filepath.Join(binDir, "kubectl-krew"))
	require.NoError(t, err)

	// Old symlink should be removed
	_, err = os.Lstat(filepath.Join(binDir, "krew"))
	assert.True(t, os.IsNotExist(err), "old symlink should be removed")
}

func createToolTestTarGz(t *testing.T, binaryName string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: binaryName,
		Mode: 0755,
		Size: int64(len(content)),
	}
	require.NoError(t, tw.WriteHeader(hdr))
	_, err := tw.Write(content)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return buf.Bytes()
}

func toolSha256Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
