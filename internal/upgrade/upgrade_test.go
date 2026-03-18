package upgrade

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsDevBuild(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "dev string", version: "dev", want: true},
		{name: "empty string", version: "", want: true},
		{name: "valid semver", version: "0.1.0", want: false},
		{name: "zero version", version: "0.0.0", want: false},
		{name: "prerelease", version: "0.1.0-rc1", want: false},
		{name: "garbage", version: "not-a-version", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, isDevBuild(tt.version))
		})
	}
}

func TestArchiveName(t *testing.T) {
	tests := []struct {
		name    string
		version string
		goos    string
		goarch  string
		want    string
	}{
		{
			name:    "darwin arm64",
			version: "0.1.3",
			goos:    "darwin",
			goarch:  "arm64",
			want:    "tomei_v0.1.3_darwin_arm64.tar.gz",
		},
		{
			name:    "linux amd64",
			version: "0.2.0",
			goos:    "linux",
			goarch:  "amd64",
			want:    "tomei_v0.2.0_linux_amd64.tar.gz",
		},
		{
			name:    "linux arm64",
			version: "1.0.0",
			goos:    "linux",
			goarch:  "arm64",
			want:    "tomei_v1.0.0_linux_arm64.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, archiveName(tt.version, tt.goos, tt.goarch))
		})
	}
}

func TestCheck_NewVersionAvailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/repos/terassyi/tomei/releases/latest", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.2.0"})
	}))
	defer srv.Close()

	u := NewUpdater(srv.Client(), srv.Client(), "0.1.0", WithAPIBaseURL(srv.URL))
	result, err := u.Check(context.Background(), Config{})

	require.NoError(t, err)
	assert.Equal(t, "0.1.0", result.CurrentVersion)
	assert.Equal(t, "0.2.0", result.LatestVersion)
	assert.False(t, result.UpToDate)
}

func TestCheck_AlreadyUpToDate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.1.0"})
	}))
	defer srv.Close()

	u := NewUpdater(srv.Client(), srv.Client(), "0.1.0", WithAPIBaseURL(srv.URL))
	result, err := u.Check(context.Background(), Config{})

	require.NoError(t, err)
	assert.True(t, result.UpToDate)
}

func TestCheck_DevBuildBlocked(t *testing.T) {
	u := NewUpdater(http.DefaultClient, http.DefaultClient, "dev")
	_, err := u.Check(context.Background(), Config{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot upgrade from development build")
}

func TestCheck_DevBuildWithForce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.1.0"})
	}))
	defer srv.Close()

	u := NewUpdater(srv.Client(), srv.Client(), "dev", WithAPIBaseURL(srv.URL))
	result, err := u.Check(context.Background(), Config{Force: true})

	require.NoError(t, err)
	assert.Equal(t, "0.1.0", result.LatestVersion)
	// UpToDate should be false since dev build can't be compared
	assert.False(t, result.UpToDate)
}

func TestCheck_TargetVersion(t *testing.T) {
	// No API call should be made when --version is specified
	u := NewUpdater(http.DefaultClient, http.DefaultClient, "0.1.0")
	result, err := u.Check(context.Background(), Config{TargetVersion: "v0.1.3"})

	require.NoError(t, err)
	assert.Equal(t, "0.1.3", result.LatestVersion)
	assert.False(t, result.UpToDate) // version comparison skipped with explicit --version
}

func TestReplaceBinary(t *testing.T) {
	tmpDir := t.TempDir()

	// Create "current" binary
	currentPath := filepath.Join(tmpDir, "tomei")
	require.NoError(t, os.WriteFile(currentPath, []byte("old-binary"), 0755))

	// Create "new" binary
	newPath := filepath.Join(tmpDir, "tomei-new")
	require.NoError(t, os.WriteFile(newPath, []byte("new-binary"), 0755))

	// Replace
	err := replaceBinary(currentPath, newPath)
	require.NoError(t, err)

	// Verify content
	content, err := os.ReadFile(currentPath)
	require.NoError(t, err)
	assert.Equal(t, "new-binary", string(content))

	// Verify permissions
	info, err := os.Stat(currentPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())

	// Verify backup was cleaned up
	_, err = os.Stat(currentPath + ".bak")
	assert.True(t, os.IsNotExist(err))
}

func TestFindBinary(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, dir string)
		wantErr bool
	}{
		{
			name: "flat archive (root level)",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei"), []byte("bin"), 0755))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "LICENSE"), []byte("lic"), 0644))
			},
		},
		{
			name: "nested in subdirectory",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				sub := filepath.Join(dir, "tomei_v0.1.0_linux_amd64")
				require.NoError(t, os.MkdirAll(sub, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(sub, "tomei"), []byte("bin"), 0755))
			},
		},
		{
			name: "binary not found",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("readme"), 0644))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)

			path, err := findBinary(dir, "tomei")
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.FileExists(t, path)
		})
	}
}

func TestCheckWritable(t *testing.T) {
	t.Run("writable directory", func(t *testing.T) {
		dir := t.TempDir()
		assert.NoError(t, checkWritable(dir))
	})

	t.Run("non-existent directory", func(t *testing.T) {
		assert.Error(t, checkWritable("/non/existent/path"))
	})
}

func TestReleaseAssetURL(t *testing.T) {
	got := releaseAssetURL("https://github.com", "0.1.3", "tomei_v0.1.3_darwin_arm64.tar.gz")
	assert.Equal(t, "https://github.com/terassyi/tomei/releases/download/v0.1.3/tomei_v0.1.3_darwin_arm64.tar.gz", got)
}

func TestCheck_RateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	u := NewUpdater(srv.Client(), srv.Client(), "0.1.0", WithAPIBaseURL(srv.URL))
	_, err := u.Check(context.Background(), Config{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit")
}

func TestDownloadFile(t *testing.T) {
	content := "test-content-for-download"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, content)
	}))
	defer srv.Close()

	tmpDir := t.TempDir()
	destPath := filepath.Join(tmpDir, "downloaded")

	err := downloadFile(context.Background(), srv.Client(), srv.URL, destPath)
	require.NoError(t, err)

	got, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
}

func TestFetchBody(t *testing.T) {
	content := "checksums content here"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, content)
	}))
	defer srv.Close()

	got, err := fetchBody(context.Background(), srv.Client(), srv.URL)
	require.NoError(t, err)
	assert.Equal(t, content, string(got))
}

func TestCopyFile(t *testing.T) {
	tmpDir := t.TempDir()

	src := filepath.Join(tmpDir, "src")
	require.NoError(t, os.WriteFile(src, []byte("data"), 0755))

	dst := filepath.Join(tmpDir, "dst")
	require.NoError(t, copyFile(src, dst))

	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "data", string(got))

	info, err := os.Stat(dst)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}
