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

	"github.com/terassyi/tomei/internal/checksum"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/runtime"
	"github.com/terassyi/tomei/internal/resource"
)

// TestRuntimeInstaller_Install_HTTP tests runtime download+install via real HTTP.
func TestRuntimeInstaller_Install_HTTP(t *testing.T) {
	t.Parallel()

	binContent := []byte("#!/bin/sh\necho 'mock runtime'\n")
	tarGzContent := createRuntimeTestTarGz(t, "myruntime", binContent)
	archiveHash := runtimeSha256Hash(tarGzContent)

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

		dl := download.NewDownloader()
		installer := runtime.NewInstaller(dl, runtimesDir)

		rt := &resource.Runtime{
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
				Binaries:    []string{"mybin"},
				BinDir:      binDir,
				ToolBinPath: "~/myruntime/bin",
			},
		}

		state, err := installer.Install(context.Background(), rt, "myruntime")
		require.NoError(t, err)

		assert.Equal(t, resource.InstallTypeDownload, state.Type)
		assert.Equal(t, "1.0.0", state.Version)
		assert.Equal(t, checksum.Digest(archiveHash), state.Digest)
		assert.DirExists(t, state.InstallPath)
		assert.FileExists(t, filepath.Join(state.InstallPath, "bin", "mybin"))

		// Verify symlink
		link, err := os.Readlink(filepath.Join(binDir, "mybin"))
		require.NoError(t, err)
		assert.Contains(t, link, "mybin")
	})

	t.Run("download with resolveVersion via github-release", func(t *testing.T) {
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

		ghClient := &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				req.URL.Scheme = "http"
				req.URL.Host = ghServer.Listener.Addr().String()
				return http.DefaultTransport.RoundTrip(req)
			}),
		}

		dl := download.NewDownloader()
		installer := runtime.NewInstaller(dl, runtimesDir)
		installer.SetHTTPClient(ghClient)

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
}

func createRuntimeTestTarGz(t *testing.T, name string, binContent []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for _, dir := range []string{name + "/", name + "/bin/"} {
		require.NoError(t, tw.WriteHeader(&tar.Header{
			Name:     dir,
			Mode:     0755,
			Typeflag: tar.TypeDir,
		}))
	}

	require.NoError(t, tw.WriteHeader(&tar.Header{
		Name: name + "/bin/mybin",
		Mode: 0755,
		Size: int64(len(binContent)),
	}))
	_, err := tw.Write(binContent)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return buf.Bytes()
}

func runtimeSha256Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
