package aqua

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRoundTripper is a mock http.RoundTripper for testing without network.
type mockRoundTripper struct {
	handler func(*http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.handler(req)
}

// newMockResponse creates a mock HTTP response.
func newMockResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Header:     make(http.Header),
	}
}

func TestFetcher_Fetch_CacheHit(t *testing.T) {
	// Setup: create a cache directory with a cached registry.yaml
	cacheDir := t.TempDir()
	ref := "v4.465.0"
	pkg := "cli/cli"

	cacheFile := filepath.Join(cacheDir, ref, "pkgs", pkg, "registry.yaml")
	err := os.MkdirAll(filepath.Dir(cacheFile), 0o755)
	require.NoError(t, err)

	cachedYAML := `packages:
  - type: github_release
    repo_owner: cli
    repo_name: cli
    asset: gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
`
	err = os.WriteFile(cacheFile, []byte(cachedYAML), 0o644)
	require.NoError(t, err)

	// Setup: mock HTTP client (should NOT be called)
	httpCalled := false
	mockClient := &http.Client{
		Transport: &mockRoundTripper{
			handler: func(req *http.Request) (*http.Response, error) {
				httpCalled = true
				return newMockResponse(http.StatusOK, ""), nil
			},
		},
	}

	// Test
	f := newFetcher(cacheDir).withHTTPClient(mockClient)
	info, err := f.fetch(context.Background(), ref, pkg)

	// Assert
	require.NoError(t, err)
	assert.False(t, httpCalled, "HTTP client should not be called on cache hit")
	assert.Equal(t, "github_release", info.Type)
	assert.Equal(t, "cli", info.RepoOwner)
	assert.Equal(t, "cli", info.RepoName)
}

func TestFetcher_Fetch_CacheMiss(t *testing.T) {
	cacheDir := t.TempDir()
	ref := "v4.465.0"
	pkg := "cli/cli"

	remoteYAML := `packages:
  - type: github_release
    repo_owner: cli
    repo_name: cli
    asset: gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
`
	// Setup: mock HTTP client
	mockClient := &http.Client{
		Transport: &mockRoundTripper{
			handler: func(req *http.Request) (*http.Response, error) {
				expectedPath := "/" + ref + "/pkgs/" + pkg + "/registry.yaml"
				assert.Equal(t, expectedPath, req.URL.Path)
				return newMockResponse(http.StatusOK, remoteYAML), nil
			},
		},
	}

	// Test
	f := newFetcher(cacheDir).
		withHTTPClient(mockClient).
		withBaseURL("https://example.com")
	info, err := f.fetch(context.Background(), ref, pkg)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "github_release", info.Type)
	assert.Equal(t, "cli", info.RepoOwner)
	assert.Equal(t, "cli", info.RepoName)

	// Verify cache was written
	cacheFile := filepath.Join(cacheDir, ref, "pkgs", pkg, "registry.yaml")
	_, err = os.Stat(cacheFile)
	assert.NoError(t, err, "cache file should be created")
}

func TestFetcher_Fetch_NotFound(t *testing.T) {
	cacheDir := t.TempDir()
	ref := "v4.465.0"
	pkg := "nonexistent/package"

	// Setup: mock HTTP client returning 404
	mockClient := &http.Client{
		Transport: &mockRoundTripper{
			handler: func(req *http.Request) (*http.Response, error) {
				return newMockResponse(http.StatusNotFound, ""), nil
			},
		},
	}

	// Test
	f := newFetcher(cacheDir).
		withHTTPClient(mockClient).
		withBaseURL("https://example.com")
	_, err := f.fetch(context.Background(), ref, pkg)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "package not found")
}

func TestFetcher_Fetch_ServerError(t *testing.T) {
	cacheDir := t.TempDir()
	ref := "v4.465.0"
	pkg := "cli/cli"

	// Setup: mock HTTP client returning 500
	mockClient := &http.Client{
		Transport: &mockRoundTripper{
			handler: func(req *http.Request) (*http.Response, error) {
				return newMockResponse(http.StatusInternalServerError, ""), nil
			},
		},
	}

	// Test
	f := newFetcher(cacheDir).
		withHTTPClient(mockClient).
		withBaseURL("https://example.com")
	_, err := f.fetch(context.Background(), ref, pkg)

	// Assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status code")
}

func TestFetcher_cachePath(t *testing.T) {
	f := newFetcher("/home/user/.cache/toto/registry/aqua")

	tests := []struct {
		ref      string
		pkg      string
		expected string
	}{
		{
			ref:      "v4.465.0",
			pkg:      "cli/cli",
			expected: "/home/user/.cache/toto/registry/aqua/v4.465.0/pkgs/cli/cli/registry.yaml",
		},
		{
			ref:      "v4.500.0",
			pkg:      "BurntSushi/ripgrep",
			expected: "/home/user/.cache/toto/registry/aqua/v4.500.0/pkgs/BurntSushi/ripgrep/registry.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.pkg, func(t *testing.T) {
			path, err := f.cachePath(tt.ref, tt.pkg)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, path)
		})
	}
}

func TestFetcher_writeCache_AtomicWrite(t *testing.T) {
	cacheDir := t.TempDir()
	f := newFetcher(cacheDir)

	path := filepath.Join(cacheDir, "test", "registry.yaml")
	data := []byte("test data")

	// Write
	err := f.writeCache(path, data)
	require.NoError(t, err)

	// Verify
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, data, content)

	// Verify no temp file left behind
	tmpFile := path + ".tmp"
	_, err = os.Stat(tmpFile)
	assert.True(t, os.IsNotExist(err), "temp file should be removed")
}

func TestFetcher_cachePath_PathTraversal(t *testing.T) {
	f := newFetcher("/home/user/.cache/toto/registry/aqua")

	tests := []struct {
		name    string
		ref     string
		pkg     string
		wantErr bool
	}{
		{
			name:    "valid",
			ref:     "v4.465.0",
			pkg:     "cli/cli",
			wantErr: false,
		},
		{
			name:    "ref with path traversal",
			ref:     "../../../etc",
			pkg:     "cli/cli",
			wantErr: true,
		},
		{
			name:    "pkg with path traversal",
			ref:     "v4.465.0",
			pkg:     "../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "ref with absolute path",
			ref:     "/etc/passwd",
			pkg:     "cli/cli",
			wantErr: true,
		},
		{
			name:    "invalid pkg format (single part)",
			ref:     "v4.465.0",
			pkg:     "cli",
			wantErr: true,
		},
		{
			name:    "invalid pkg format (three parts)",
			ref:     "v4.465.0",
			pkg:     "org/repo/extra",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.cachePath(tt.ref, tt.pkg)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
