package tool

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
	"github.com/terassyi/toto/internal/installer/place"
	"github.com/terassyi/toto/internal/resource"
)

func TestNewToolInstaller(t *testing.T) {
	downloader := download.NewDownloader()
	placer := place.NewPlacer("/tools", "/bin")

	installer := NewToolInstaller(downloader, placer)
	assert.NotNil(t, installer)
}

func TestToolInstaller_Install(t *testing.T) {
	// Create test server
	binaryContent := []byte("#!/bin/sh\necho hello")
	tarGzContent := createTarGzContent(t, "ripgrep", binaryContent)
	archiveHash := sha256Hash(tarGzContent) // Hash of archive, not binary

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			// Return checksum file
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(archiveHash + "  ripgrep.tar.gz\n"))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(tarGzContent)
	}))
	defer server.Close()

	tests := []struct {
		name       string
		tool       *resource.Tool
		wantErr    bool
		errContain string
	}{
		{
			name: "successful install",
			tool: &resource.Tool{
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
			},
			wantErr: false,
		},
		{
			name: "missing source",
			tool: &resource.Tool{
				BaseResource: resource.BaseResource{
					Metadata: resource.Metadata{Name: "mytool"},
				},
				ToolSpec: &resource.ToolSpec{
					InstallerRef: "download",
					Version:      "1.0.0",
					Source:       nil,
				},
			},
			wantErr:    true,
			errContain: "source is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			toolsDir := filepath.Join(tmpDir, "tools")
			binDir := filepath.Join(tmpDir, "bin")

			downloader := download.NewDownloader()
			placer := place.NewPlacer(toolsDir, binDir)

			installer := NewToolInstaller(downloader, placer)

			state, err := installer.Install(context.Background(), tt.tool, tt.tool.Name())

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, state)
			assert.Equal(t, tt.tool.ToolSpec.InstallerRef, state.InstallerRef)
			assert.Equal(t, tt.tool.ToolSpec.Version, state.Version)
			assert.NotEmpty(t, state.InstallPath)
			assert.NotEmpty(t, state.BinPath)
			assert.NotEmpty(t, state.Digest)

			// Verify binary exists
			_, err = os.Stat(state.InstallPath)
			require.NoError(t, err)

			// Verify symlink exists
			_, err = os.Lstat(state.BinPath)
			require.NoError(t, err)
		})
	}
}

func TestToolInstaller_Install_Skip(t *testing.T) {
	binaryContent := []byte("#!/bin/sh\necho hello")

	tmpDir := t.TempDir()
	toolsDir := filepath.Join(tmpDir, "tools")
	binDir := filepath.Join(tmpDir, "bin")

	// Pre-install the binary
	installDir := filepath.Join(toolsDir, "ripgrep", "14.1.1")
	err := os.MkdirAll(installDir, 0755)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(installDir, "ripgrep"), binaryContent, 0755)
	require.NoError(t, err)

	downloader := download.NewDownloader()
	placer := place.NewPlacer(toolsDir, binDir)
	inst := NewToolInstaller(downloader, placer)

	// No checksum - will skip if binary exists
	tool := &resource.Tool{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "ripgrep"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "download",
			Version:      "14.1.1",
			Source: &resource.DownloadSource{
				URL:         "https://example.com/ripgrep.tar.gz",
				Checksum:    nil, // No checksum = existence check only
				ArchiveType: "tar.gz",
			},
		},
	}

	state, err := inst.Install(context.Background(), tool, "ripgrep")

	require.NoError(t, err)
	assert.NotNil(t, state)
	// Should still return valid state even if skipped
	assert.Equal(t, tool.ToolSpec.Version, state.Version)
}

// Helper functions

func createTarGzContent(t *testing.T, binaryName string, content []byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name: binaryName,
		Mode: 0755,
		Size: int64(len(content)),
	}
	err := tw.WriteHeader(hdr)
	require.NoError(t, err)
	_, err = tw.Write(content)
	require.NoError(t, err)

	require.NoError(t, tw.Close())
	require.NoError(t, gw.Close())

	return buf.Bytes()
}

func sha256Hash(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
