package download

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDownloader(t *testing.T) {
	d := NewDownloader()
	assert.NotNil(t, d)
}

func TestDownloader_Download(t *testing.T) {
	testContent := []byte("hello world")

	tests := []struct {
		name       string
		handler    http.HandlerFunc
		wantErr    bool
		errContain string
	}{
		{
			name: "successful download",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write(testContent)
			},
			wantErr: false,
		},
		{
			name: "404 not found",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			errContain: "404",
		},
		{
			name: "500 server error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			wantErr:    true,
			errContain: "500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			tmpDir := t.TempDir()
			destPath := filepath.Join(tmpDir, "downloaded")

			d := NewDownloader()
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

			// Verify file was downloaded
			content, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, testContent, content)
		})
	}
}

func TestDownloader_Download_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "downloaded")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := NewDownloader()
	path, err := d.Download(ctx, server.URL, destPath)

	require.Error(t, err)
	assert.Empty(t, path)
}

func TestDownloader_Verify(t *testing.T) {
	testContent := []byte("hello world")
	sha256sum := fmt.Sprintf("%x", sha256.Sum256(testContent))
	sha512sum := fmt.Sprintf("%x", sha512.Sum512(testContent))
	md5sum := fmt.Sprintf("%x", md5.Sum(testContent))

	tests := []struct {
		name       string
		content    []byte
		handler    http.HandlerFunc
		wantErr    bool
		errContain string
	}{
		{
			name:    "valid sha256 checksum",
			content: testContent,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".sha256") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(sha256sum))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: false,
		},
		{
			name:    "valid sha512 checksum",
			content: testContent,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".sha512") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(sha512sum))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: false,
		},
		{
			name:    "valid md5 checksum",
			content: testContent,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".md5") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(md5sum))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: false,
		},
		{
			name:    "valid checksum with filename format",
			content: testContent,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".sha256") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(fmt.Sprintf("%s  somefile.tar.gz", sha256sum)))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: false,
		},
		{
			name:    "checksum mismatch",
			content: testContent,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".sha256") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000"))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			errContain: "checksum mismatch",
		},
		{
			name:    "no checksum file found - skip verification",
			content: testContent,
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: false,
		},
		{
			name:    "prefers sha256 over md5",
			content: testContent,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".sha256") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(sha256sum))
					return
				}
				if strings.HasSuffix(r.URL.Path, ".md5") {
					w.WriteHeader(http.StatusOK)
					// Wrong hash to ensure sha256 is preferred
					w.Write([]byte("wronghash"))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: false,
		},
		{
			name:    "empty checksum file - skip to next algorithm",
			content: testContent,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".sha256") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(""))
					return
				}
				if strings.HasSuffix(r.URL.Path, ".sha512") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(sha512sum))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: false,
		},
		{
			name:    "whitespace only checksum file - skip to next algorithm",
			content: testContent,
			handler: func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, ".sha256") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte("   \n\t  "))
					return
				}
				if strings.HasSuffix(r.URL.Path, ".sha512") {
					w.WriteHeader(http.StatusOK)
					w.Write([]byte(sha512sum))
					return
				}
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "testfile")

			err := os.WriteFile(filePath, tt.content, 0644)
			require.NoError(t, err)

			d := NewDownloader()
			err = d.Verify(context.Background(), filePath, server.URL+"/file.tar.gz")

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

func TestDownloader_Verify_FileNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("abcd1234"))
	}))
	defer server.Close()

	d := NewDownloader()
	err := d.Verify(context.Background(), "/nonexistent/file", server.URL+"/file.tar.gz")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open file")
}
