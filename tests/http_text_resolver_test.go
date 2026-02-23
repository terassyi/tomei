//go:build integration

package tests

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
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/runtime"
	"github.com/terassyi/tomei/internal/resource"
)

// NOTE: These tests exercise the shared resolve.Resolver indirectly through
// runtime.Installer. Direct unit tests are in internal/installer/resolve/resolve_test.go.
// The tool commands pattern (installByCommands) also uses the shared resolver
// but is tested at the unit level in internal/installer/tool/installer_test.go.

// TestHTTPTextResolver_ResolveAndInstall tests the http-text version resolver
// via the full Install path.  These tests use httptest.NewServer to verify
// actual HTTP communication behavior.
func TestHTTPTextResolver_ResolveAndInstall(t *testing.T) {
	t.Parallel()

	// Shared archive for successful-install tests.
	binContent := []byte("#!/bin/sh\necho 'mock runtime'\n")
	archiveBytes := createHTTPTextTestArchive(t, "httptext-rt", binContent)
	archiveHash := sha256hex(archiveBytes)

	t.Run("success with capture group", func(t *testing.T) {
		t.Parallel()

		versionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "go1.26.0\ngo1.25.6\n")
		}))
		defer versionServer.Close()

		archiveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write(archiveBytes) //nolint:errcheck
		}))
		defer archiveServer.Close()

		tmpDir := t.TempDir()
		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:           resource.InstallTypeDownload,
				Version:        "latest",
				ResolveVersion: []string{fmt.Sprintf("http-text:%s:^go(.+)", versionServer.URL)},
				Source: &resource.DownloadSource{
					URL:      archiveServer.URL + "/httptext-rt.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:" + archiveHash},
				},
				Binaries:    []string{"mybin"},
				BinDir:      filepath.Join(tmpDir, "bin"),
				ToolBinPath: "~/httptext-rt/bin",
			},
		}

		st, err := installer.Install(context.Background(), rt, "httptext-rt")
		require.NoError(t, err)
		assert.Equal(t, "1.26.0", st.Version)
		assert.Equal(t, resource.VersionAlias, st.VersionKind)
		assert.Equal(t, "latest", st.SpecVersion)
	})

	t.Run("success without capture group", func(t *testing.T) {
		t.Parallel()

		versionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "v2.1.4\n")
		}))
		defer versionServer.Close()

		archiveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write(archiveBytes) //nolint:errcheck
		}))
		defer archiveServer.Close()

		tmpDir := t.TempDir()
		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:           resource.InstallTypeDownload,
				Version:        "latest",
				ResolveVersion: []string{fmt.Sprintf("http-text:%s:v[0-9.]+", versionServer.URL)},
				Source: &resource.DownloadSource{
					URL:      archiveServer.URL + "/rt.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:" + archiveHash},
				},
				Binaries:    []string{"mybin"},
				BinDir:      filepath.Join(tmpDir, "bin"),
				ToolBinPath: "~/rt/bin",
			},
		}

		st, err := installer.Install(context.Background(), rt, "rt")
		require.NoError(t, err)
		assert.Equal(t, "v2.1.4", st.Version)
		assert.Equal(t, resource.VersionAlias, st.VersionKind)
	})

	t.Run("match on non-first line", func(t *testing.T) {
		t.Parallel()

		versionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "# Latest release\nDate: 2026-02-19\nversion=3.14.0\n")
		}))
		defer versionServer.Close()

		archiveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write(archiveBytes) //nolint:errcheck
		}))
		defer archiveServer.Close()

		tmpDir := t.TempDir()
		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:           resource.InstallTypeDownload,
				Version:        "latest",
				ResolveVersion: []string{fmt.Sprintf("http-text:%s:^version=(.+)", versionServer.URL)},
				Source: &resource.DownloadSource{
					URL:      archiveServer.URL + "/rt.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:" + archiveHash},
				},
				Binaries:    []string{"mybin"},
				BinDir:      filepath.Join(tmpDir, "bin"),
				ToolBinPath: "~/rt/bin",
			},
		}

		st, err := installer.Install(context.Background(), rt, "rt")
		require.NoError(t, err)
		assert.Equal(t, "3.14.0", st.Version)
	})

	t.Run("URL with query parameters", func(t *testing.T) {
		t.Parallel()

		versionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "text", r.URL.Query().Get("m"))
			fmt.Fprint(w, "go1.26.0\n")
		}))
		defer versionServer.Close()

		archiveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write(archiveBytes) //nolint:errcheck
		}))
		defer archiveServer.Close()

		tmpDir := t.TempDir()
		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:           resource.InstallTypeDownload,
				Version:        "latest",
				ResolveVersion: []string{fmt.Sprintf("http-text:%s/VERSION?m=text:^go(.+)", versionServer.URL)},
				Source: &resource.DownloadSource{
					URL:      archiveServer.URL + "/rt.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:" + archiveHash},
				},
				Binaries:    []string{"mybin"},
				BinDir:      filepath.Join(tmpDir, "bin"),
				ToolBinPath: "~/rt/bin",
			},
		}

		st, err := installer.Install(context.Background(), rt, "rt")
		require.NoError(t, err)
		assert.Equal(t, "1.26.0", st.Version)
	})

	t.Run("first match wins among multiple matches", func(t *testing.T) {
		t.Parallel()

		versionServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "release-1.0.0\nrelease-2.0.0\nrelease-3.0.0\n")
		}))
		defer versionServer.Close()

		archiveServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write(archiveBytes) //nolint:errcheck
		}))
		defer archiveServer.Close()

		tmpDir := t.TempDir()
		installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

		rt := &resource.Runtime{
			RuntimeSpec: &resource.RuntimeSpec{
				Type:           resource.InstallTypeDownload,
				Version:        "latest",
				ResolveVersion: []string{fmt.Sprintf("http-text:%s:^release-(.+)", versionServer.URL)},
				Source: &resource.DownloadSource{
					URL:      archiveServer.URL + "/rt.tar.gz",
					Checksum: &resource.Checksum{Value: "sha256:" + archiveHash},
				},
				Binaries:    []string{"mybin"},
				BinDir:      filepath.Join(tmpDir, "bin"),
				ToolBinPath: "~/rt/bin",
			},
		}

		st, err := installer.Install(context.Background(), rt, "rt")
		require.NoError(t, err)
		assert.Equal(t, "1.0.0", st.Version, "should return first match, not last")
	})
}

// TestHTTPTextResolver_Errors tests error paths in the http-text resolver
// via the full Install path.
func TestHTTPTextResolver_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantSubstr string
	}{
		{
			name: "server error 500",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantSubstr: "status 500",
		},
		{
			name: "404 not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantSubstr: "status 404",
		},
		{
			name: "empty body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				// 200 OK with empty body
			},
			wantSubstr: "no match",
		},
		{
			name: "no regex match",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprint(w, "no version here\n")
			},
			wantSubstr: "no match",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(tt.handler)
			defer server.Close()

			tmpDir := t.TempDir()
			installer := runtime.NewInstaller(download.NewDownloader(), filepath.Join(tmpDir, "runtimes"))

			rt := &resource.Runtime{
				RuntimeSpec: &resource.RuntimeSpec{
					Type:           resource.InstallTypeDownload,
					Version:        "latest",
					ResolveVersion: []string{fmt.Sprintf("http-text:%s:^go(.+)", server.URL)},
					Source: &resource.DownloadSource{
						URL: "https://example.com/should-not-be-reached.tar.gz",
					},
					Binaries:    []string{"mybin"},
					ToolBinPath: "~/rt/bin",
				},
			}

			_, err := installer.Install(context.Background(), rt, "rt")
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantSubstr)
		})
	}
}

// createHTTPTextTestArchive creates a tar.gz with a top-level directory containing a single binary.
func createHTTPTextTestArchive(t *testing.T, name string, binContent []byte) []byte {
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

func sha256hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

