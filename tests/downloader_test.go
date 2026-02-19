//go:build integration

package tests

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/resource"
)

func TestDownloader_Download_HTTP(t *testing.T) {
	t.Parallel()
	testContent := []byte("hello world")

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		errContain string
	}{
		{
			name: "successful download",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(testContent)
			},
			wantErr: false,
		},
		{
			name: "404 not found",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			errContain: "404",
		},
		{
			name: "500 server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:    true,
			errContain: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			tmpDir := t.TempDir()
			destPath := filepath.Join(tmpDir, "downloaded")

			d := download.NewDownloader()
			path, err := d.Download(context.Background(), server.URL, destPath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				assert.Empty(t, path)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, destPath, path)

			content, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, testContent, content)
		})
	}
}

func TestDownloader_Verify_URLChecksum_HTTP(t *testing.T) {
	t.Parallel()
	testContent := []byte("hello world")
	sha256sum := fmt.Sprintf("%x", sha256.Sum256(testContent))

	tests := []struct {
		name        string
		handler     http.HandlerFunc
		filePattern string
		wantErr     bool
		errContain  string
	}{
		{
			name: "single hash format",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(fmt.Appendf(nil, "%s  testfile.tar.gz\n", sha256sum))
			},
			wantErr: false,
		},
		{
			name: "multiple files in checksum file",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(fmt.Appendf(nil,
					"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  other.tar.gz\n"+
						"%s  testfile.tar.gz\n",
					sha256sum,
				))
			},
			wantErr: false,
		},
		{
			name: "checksum file fetch error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			errContain: "failed to fetch checksum file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "testfile.tar.gz")
			err := os.WriteFile(filePath, testContent, 0644)
			require.NoError(t, err)

			checksum := &resource.Checksum{
				URL:         server.URL + "/checksums.txt",
				FilePattern: tt.filePattern,
			}

			d := download.NewDownloader()
			err = d.Verify(context.Background(), filePath, checksum)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestDownloader_Verify_GoJSONChecksum_HTTP(t *testing.T) {
	t.Parallel()
	testContent := []byte("hello world")
	sha256sum := fmt.Sprintf("%x", sha256.Sum256(testContent))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(fmt.Appendf(nil, `[
			{
				"version": "go1.23.5",
				"stable": true,
				"files": [
					{
						"filename": "go1.23.5.linux-amd64.tar.gz",
						"sha256": "%s",
						"kind": "archive"
					}
				]
			}
		]`, sha256sum))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "go1.23.5.linux-amd64.tar.gz")
	err := os.WriteFile(filePath, testContent, 0644)
	require.NoError(t, err)

	checksum := &resource.Checksum{URL: server.URL}

	d := download.NewDownloader()
	err = d.Verify(context.Background(), filePath, checksum)
	require.NoError(t, err)
}
