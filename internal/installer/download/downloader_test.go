package download

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/resource"
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

func TestDownloader_Verify_NilChecksum(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testfile")
	err := os.WriteFile(filePath, []byte("hello world"), 0644)
	require.NoError(t, err)

	d := NewDownloader()
	err = d.Verify(context.Background(), filePath, nil)

	require.NoError(t, err)
}

func TestDownloader_Verify_DirectValue(t *testing.T) {
	testContent := []byte("hello world")
	sha256sum := fmt.Sprintf("%x", sha256.Sum256(testContent))
	sha512sum := fmt.Sprintf("%x", sha512.Sum512(testContent))

	tests := []struct {
		name       string
		checksum   *resource.Checksum
		wantErr    bool
		errContain string
	}{
		{
			name: "valid sha256 checksum",
			checksum: &resource.Checksum{
				Value: "sha256:" + sha256sum,
			},
			wantErr: false,
		},
		{
			name: "valid sha512 checksum",
			checksum: &resource.Checksum{
				Value: "sha512:" + sha512sum,
			},
			wantErr: false,
		},
		{
			name: "invalid format - missing algorithm",
			checksum: &resource.Checksum{
				Value: sha256sum,
			},
			wantErr:    true,
			errContain: "invalid checksum format",
		},
		{
			name: "unsupported algorithm",
			checksum: &resource.Checksum{
				Value: "md5:abc123",
			},
			wantErr:    true,
			errContain: "unsupported hash algorithm",
		},
		{
			name: "checksum mismatch",
			checksum: &resource.Checksum{
				Value: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
			},
			wantErr:    true,
			errContain: "checksum mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "testfile")
			err := os.WriteFile(filePath, testContent, 0644)
			require.NoError(t, err)

			d := NewDownloader()
			err = d.Verify(context.Background(), filePath, tt.checksum)

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

func TestDownloader_Verify_URLChecksum(t *testing.T) {
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
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				// Format: hash  filename
				w.Write([]byte(fmt.Sprintf("%s  testfile.tar.gz\n", sha256sum)))
			},
			wantErr: false,
		},
		{
			name: "BSD style format with asterisk",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				// Format: hash *filename
				w.Write([]byte(fmt.Sprintf("%s *testfile.tar.gz\n", sha256sum)))
			},
			wantErr: false,
		},
		{
			name: "multiple files in checksum file",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(fmt.Sprintf(
					"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  other.tar.gz\n"+
						"%s  testfile.tar.gz\n"+
						"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  another.tar.gz\n",
					sha256sum,
				)))
			},
			wantErr: false,
		},
		{
			name: "file not found in checksum file",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  other.tar.gz\n"))
			},
			wantErr:    true,
			errContain: "not found in checksums file",
		},
		{
			name: "checksum file fetch error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			wantErr:    true,
			errContain: "failed to fetch checksum file",
		},
		{
			name: "custom file pattern",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(fmt.Sprintf("%s  custom-name.tar.gz\n", sha256sum)))
			},
			filePattern: "custom-name.tar.gz",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			d := NewDownloader()
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

func TestDownloader_Verify_EmptyChecksum(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testfile")
	err := os.WriteFile(filePath, []byte("hello world"), 0644)
	require.NoError(t, err)

	// Empty checksum struct (no value, no URL)
	checksum := &resource.Checksum{}

	d := NewDownloader()
	err = d.Verify(context.Background(), filePath, checksum)

	require.NoError(t, err)
}

func TestDownloader_Verify_FileNotFound(t *testing.T) {
	checksum := &resource.Checksum{
		Value: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}

	d := NewDownloader()
	err := d.Verify(context.Background(), "/nonexistent/file", checksum)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open file")
}
