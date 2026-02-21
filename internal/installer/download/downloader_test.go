package download

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tomeiErrors "github.com/terassyi/tomei/internal/errors"
	"github.com/terassyi/tomei/internal/resource"
)

// roundTripFunc is a helper for mocking http.RoundTripper in tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewDownloader(t *testing.T) {
	t.Parallel()
	d := NewDownloader()
	assert.NotNil(t, d)

	hd, ok := d.(*httpDownloader)
	require.True(t, ok)

	// Verify transport-level timeouts are set (not http.Client.Timeout,
	// which would limit body read time for large downloads).
	tr, ok := hd.client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, defaultResponseHeaderTimeout, tr.ResponseHeaderTimeout)
	assert.Equal(t, defaultDialTimeout, tr.TLSHandshakeTimeout)
	assert.Zero(t, hd.client.Timeout, "Client.Timeout must be zero to allow large downloads")
}

func TestNewDownloaderWithClient_NilFallback(t *testing.T) {
	t.Parallel()
	d := NewDownloaderWithClient(nil)

	hd, ok := d.(*httpDownloader)
	require.True(t, ok)

	tr, ok := hd.client.Transport.(*http.Transport)
	require.True(t, ok)
	assert.Equal(t, defaultResponseHeaderTimeout, tr.ResponseHeaderTimeout)
	assert.Zero(t, hd.client.Timeout, "Client.Timeout must be zero to allow large downloads")
}

func TestDownloader_Download(t *testing.T) {
	t.Parallel()
	testContent := []byte("hello world")

	tests := []struct {
		name       string
		transport  roundTripFunc
		wantErr    bool
		errContain string
		wantCode   tomeiErrors.Code
	}{
		{
			name: "successful download",
			transport: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode:    http.StatusOK,
					Body:          io.NopCloser(bytes.NewReader(testContent)),
					ContentLength: int64(len(testContent)),
				}, nil
			},
			wantErr: false,
		},
		{
			name: "404 not found",
			transport: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(bytes.NewReader(nil)),
				}, nil
			},
			wantErr:    true,
			errContain: "404",
			wantCode:   tomeiErrors.CodeHTTPError,
		},
		{
			name: "500 server error",
			transport: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusInternalServerError,
					Body:       io.NopCloser(bytes.NewReader(nil)),
				}, nil
			},
			wantErr:    true,
			errContain: "500",
			wantCode:   tomeiErrors.CodeHTTPError,
		},
		{
			name: "network error",
			transport: func(_ *http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("connection refused")
			},
			wantErr:    true,
			errContain: "connection refused",
			wantCode:   tomeiErrors.CodeNetworkFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tmpDir := t.TempDir()
			destPath := filepath.Join(tmpDir, "downloaded")

			d := NewDownloaderWithClient(&http.Client{Transport: tt.transport})
			path, err := d.Download(context.Background(), "https://example.com/test", destPath)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				if tt.wantCode != "" {
					var tErr *tomeiErrors.Error
					require.ErrorAs(t, err, &tErr)
					assert.Equal(t, tt.wantCode, tErr.Code)
					assert.Equal(t, tomeiErrors.CategoryNetwork, tErr.Category)
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
	t.Parallel()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "downloaded")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := NewDownloaderWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return nil, req.Context().Err()
		}),
	})
	path, err := d.Download(ctx, "https://example.com/test", destPath)

	require.Error(t, err)
	assert.Empty(t, path)
}

func TestDownloader_Verify_NilChecksum(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "testfile")
	err := os.WriteFile(filePath, []byte("hello world"), 0644)
	require.NoError(t, err)

	d := NewDownloader()
	err = d.Verify(context.Background(), filePath, nil)

	require.NoError(t, err)
}

func TestDownloader_Verify_DirectValue(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
	testContent := []byte("hello world")
	sha256sum := fmt.Sprintf("%x", sha256.Sum256(testContent))

	tests := []struct {
		name        string
		respBody    string
		respStatus  int
		filePattern string
		wantErr     bool
		errContain  string
		wantCode    tomeiErrors.Code
	}{
		{
			name:     "single hash format",
			respBody: fmt.Sprintf("%s  testfile.tar.gz\n", sha256sum),
			wantErr:  false,
		},
		{
			name:     "BSD style format with asterisk",
			respBody: fmt.Sprintf("%s *testfile.tar.gz\n", sha256sum),
			wantErr:  false,
		},
		{
			name: "multiple files in checksum file",
			respBody: fmt.Sprintf(
				"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  other.tar.gz\n"+
					"%s  testfile.tar.gz\n"+
					"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb  another.tar.gz\n",
				sha256sum,
			),
			wantErr: false,
		},
		{
			name:       "file not found in checksum file",
			respBody:   "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  other.tar.gz\n",
			wantErr:    true,
			errContain: "not found in GNU checksums file",
		},
		{
			name:       "checksum file fetch error",
			respStatus: http.StatusNotFound,
			wantErr:    true,
			errContain: "failed to fetch checksum file",
			wantCode:   tomeiErrors.CodeHTTPError,
		},
		{
			name:        "custom file pattern",
			respBody:    fmt.Sprintf("%s  custom-name.tar.gz\n", sha256sum),
			filePattern: "custom-name.tar.gz",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			status := tt.respStatus
			if status == 0 {
				status = http.StatusOK
			}

			d := NewDownloaderWithClient(&http.Client{
				Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: status,
						Body:       io.NopCloser(bytes.NewBufferString(tt.respBody)),
					}, nil
				}),
			})

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, "testfile.tar.gz")
			err := os.WriteFile(filePath, testContent, 0644)
			require.NoError(t, err)

			checksum := &resource.Checksum{
				URL:         "https://example.com/checksums.txt",
				FilePattern: tt.filePattern,
			}

			err = d.Verify(context.Background(), filePath, checksum)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContain != "" {
					assert.Contains(t, err.Error(), tt.errContain)
				}
				if tt.wantCode != "" {
					var tErr *tomeiErrors.Error
					require.ErrorAs(t, err, &tErr)
					assert.Equal(t, tt.wantCode, tErr.Code)
				}
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestDownloader_Verify_GoJSONChecksum(t *testing.T) {
	t.Parallel()
	testContent := []byte("hello world")
	sha256sum := fmt.Sprintf("%x", sha256.Sum256(testContent))

	tests := []struct {
		name       string
		respBody   string
		filename   string
		wantErr    bool
		errContain string
	}{
		{
			name: "valid Go JSON format",
			respBody: fmt.Sprintf(`[
				{
					"version": "go1.23.5",
					"stable": true,
					"files": [
						{
							"filename": "go1.23.5.linux-amd64.tar.gz",
							"os": "linux",
							"arch": "amd64",
							"sha256": "%s",
							"size": 12345,
							"kind": "archive"
						}
					]
				}
			]`, sha256sum),
			filename: "go1.23.5.linux-amd64.tar.gz",
			wantErr:  false,
		},
		{
			name: "multiple versions in Go JSON",
			respBody: fmt.Sprintf(`[
				{
					"version": "go1.24.0",
					"stable": true,
					"files": [
						{"filename": "go1.24.0.linux-amd64.tar.gz", "sha256": "aaaa", "kind": "archive"}
					]
				},
				{
					"version": "go1.23.5",
					"stable": true,
					"files": [
						{"filename": "go1.23.5.linux-amd64.tar.gz", "sha256": "%s", "kind": "archive"},
						{"filename": "go1.23.5.darwin-arm64.tar.gz", "sha256": "bbbb", "kind": "archive"}
					]
				}
			]`, sha256sum),
			filename: "go1.23.5.linux-amd64.tar.gz",
			wantErr:  false,
		},
		{
			name: "file not found in Go JSON",
			respBody: `[
				{
					"version": "go1.23.5",
					"stable": true,
					"files": [
						{"filename": "go1.23.5.darwin-arm64.tar.gz", "sha256": "aaaa", "kind": "archive"}
					]
				}
			]`,
			filename:   "go1.23.5.linux-amd64.tar.gz",
			wantErr:    true,
			errContain: "not found in Go JSON checksums",
		},
		{
			name:       "invalid JSON falls back to unknown format",
			respBody:   `[invalid json`,
			filename:   "go1.23.5.linux-amd64.tar.gz",
			wantErr:    true,
			errContain: "unknown or unsupported checksum file format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d := NewDownloaderWithClient(&http.Client{
				Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(bytes.NewBufferString(tt.respBody)),
					}, nil
				}),
			})

			tmpDir := t.TempDir()
			filePath := filepath.Join(tmpDir, tt.filename)
			err := os.WriteFile(filePath, testContent, 0644)
			require.NoError(t, err)

			checksum := &resource.Checksum{
				URL: "https://example.com/checksums.json",
			}

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
	t.Parallel()
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
	t.Parallel()
	checksum := &resource.Checksum{
		Value: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}

	d := NewDownloader()
	err := d.Verify(context.Background(), "/nonexistent/file", checksum)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open file")
}
