//go:build integration

package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/installer/extract"
	"github.com/terassyi/tomei/internal/registry/aqua"
)

// TestAquaResolverIntegration tests the full resolver flow with a mock HTTP server.
// This covers: Fetcher → Version Override → OS Override → Template Rendering
func TestAquaResolverIntegration(t *testing.T) {
	// Setup mock HTTP server serving registry.yaml
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4.465.0/pkgs/cli/cli/registry.yaml":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`packages:
  - type: github_release
    repo_owner: cli
    repo_name: cli
    asset: gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    replacements:
      amd64: amd64
      arm64: arm64
      darwin: macOS
      linux: linux
    checksum:
      type: github_release
      asset: gh_{{trimV .Version}}_checksums.txt
      algorithm: sha256
    files:
      - name: gh
        src: gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}/bin/gh
`))
		case "/v4.465.0/pkgs/BurntSushi/ripgrep/registry.yaml":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`packages:
  - type: github_release
    repo_owner: BurntSushi
    repo_name: ripgrep
    asset: ripgrep-{{.Version}}-{{.Arch}}-unknown-{{.OS}}-gnu.tar.gz
    format: tar.gz
    replacements:
      amd64: x86_64
      arm64: aarch64
      darwin: apple-darwin
      linux: linux
    version_overrides:
      - version_constraint: semver(">= 14.0.0")
        asset: ripgrep-{{.Version}}-{{.Arch}}-unknown-{{.OS}}-gnu.tar.gz
        replacements:
          amd64: x86_64
          arm64: aarch64
          darwin: apple-darwin
          linux: linux
      - version_constraint: semver("< 14.0.0")
        asset: ripgrep-{{.Version}}-{{.Arch}}-unknown-{{.OS}}-musl.tar.gz
        replacements:
          amd64: x86_64
          arm64: aarch64
    checksum:
      type: github_release
      asset: "{{.Asset}}.sha256"
      algorithm: sha256
    files:
      - name: rg
`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create resolver with mock server
	cacheDir := t.TempDir()
	resolver := aqua.NewResolverWithBaseURL(cacheDir, server.URL)

	ctx := context.Background()

	t.Run("resolve gh for linux/arm64", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "cli/cli", "v2.86.0", "linux", "arm64")
		require.NoError(t, err)

		assert.Equal(t, "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_linux_arm64.tar.gz", resolved.URL)
		assert.Equal(t, "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_checksums.txt", resolved.ChecksumURL)
		assert.Equal(t, extract.ArchiveTypeTarGz, resolved.Format)
	})

	t.Run("resolve gh for darwin/arm64", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "cli/cli", "v2.86.0", "darwin", "arm64")
		require.NoError(t, err)

		assert.Equal(t, "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_macOS_arm64.tar.gz", resolved.URL)
	})

	t.Run("resolve ripgrep for linux/arm64 with version >= 14.0.0", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "BurntSushi/ripgrep", "15.1.0", "linux", "arm64")
		require.NoError(t, err)

		assert.Equal(t, "https://github.com/BurntSushi/ripgrep/releases/download/15.1.0/ripgrep-15.1.0-aarch64-unknown-linux-gnu.tar.gz", resolved.URL)
		assert.Contains(t, resolved.ChecksumURL, "sha256")
	})

	t.Run("resolve ripgrep for linux/amd64 with version < 14.0.0", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "BurntSushi/ripgrep", "13.0.0", "linux", "amd64")
		require.NoError(t, err)

		// Version < 14.0.0 should use musl variant
		assert.Equal(t, "https://github.com/BurntSushi/ripgrep/releases/download/13.0.0/ripgrep-13.0.0-x86_64-unknown-linux-musl.tar.gz", resolved.URL)
	})
}

// TestAquaComplexOverrides tests complex override scenarios.
func TestAquaComplexOverrides(t *testing.T) {
	// Setup mock HTTP server with complex override structure
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4.465.0/pkgs/complex/tool/registry.yaml":
			w.WriteHeader(http.StatusOK)
			// Complex override: version_overrides + OS overrides + replacements merge
			_, _ = w.Write([]byte(`packages:
  - type: github_release
    repo_owner: complex
    repo_name: tool
    asset: tool_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    replacements:
      darwin: macos
      linux: linux
      amd64: x86_64
      arm64: aarch64
    version_overrides:
      - version_constraint: semver(">= 2.0.0")
        asset: tool-v2_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz
        replacements:
          darwin: Darwin
          linux: Linux
      - version_constraint: semver("< 2.0.0")
        asset: tool-legacy_{{.Version}}_{{.OS}}_{{.Arch}}.zip
        format: zip
        overrides:
          - goos: windows
            asset: tool-legacy_{{.Version}}_windows_{{.Arch}}.exe
            format: raw
    overrides:
      - goos: darwin
        goarch: arm64
        asset: tool_{{.Version}}_macos_apple_silicon.tar.gz
    files:
      - name: tool
`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	resolver := aqua.NewResolverWithBaseURL(cacheDir, server.URL)
	ctx := context.Background()

	t.Run("version >= 2.0.0 uses v2 asset with version override replacements", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "complex/tool", "2.5.0", "linux", "arm64")
		require.NoError(t, err)

		// version_overrides replacements completely replace base replacements (no merge)
		// darwin→Darwin, linux→Linux are applied, but arm64 has no replacement so stays as arm64
		assert.Equal(t, "https://github.com/complex/tool/releases/download/2.5.0/tool-v2_2.5.0_Linux_arm64.tar.gz", resolved.URL)
		assert.Equal(t, extract.ArchiveTypeTarGz, resolved.Format)
	})

	t.Run("version < 2.0.0 uses legacy asset with zip format", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "complex/tool", "1.5.0", "linux", "amd64")
		require.NoError(t, err)

		assert.Equal(t, "https://github.com/complex/tool/releases/download/1.5.0/tool-legacy_1.5.0_linux_x86_64.zip", resolved.URL)
		assert.Equal(t, extract.ArchiveTypeZip, resolved.Format)
	})

	t.Run("darwin/arm64 uses OS override asset", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "complex/tool", "3.0.0", "darwin", "arm64")
		require.NoError(t, err)

		// OS override should take precedence
		assert.Equal(t, "https://github.com/complex/tool/releases/download/3.0.0/tool_3.0.0_macos_apple_silicon.tar.gz", resolved.URL)
	})
}

// TestAquaChecksumFlow tests checksum URL generation and template rendering.
func TestAquaChecksumFlow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4.465.0/pkgs/checksum/test/registry.yaml":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`packages:
  - type: github_release
    repo_owner: checksum
    repo_name: test
    asset: test_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    checksum:
      type: github_release
      asset: "{{.Asset}}.sha256"
      algorithm: sha256
    files:
      - name: test
`))
		case "/v4.465.0/pkgs/checksum/test2/registry.yaml":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`packages:
  - type: github_release
    repo_owner: checksum
    repo_name: test2
    asset: test2_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    checksum:
      type: github_release
      asset: checksums.txt
      algorithm: sha256
    files:
      - name: test2
`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	resolver := aqua.NewResolverWithBaseURL(cacheDir, server.URL)
	ctx := context.Background()

	t.Run("checksum asset uses {{.Asset}} template variable", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "checksum/test", "1.0.0", "linux", "amd64")
		require.NoError(t, err)

		// {{.Asset}} should be replaced with the rendered asset name
		assert.Equal(t, "https://github.com/checksum/test/releases/download/1.0.0/test_1.0.0_linux_amd64.tar.gz.sha256", resolved.ChecksumURL)
	})

	t.Run("checksum asset with static filename", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "checksum/test2", "1.0.0", "linux", "amd64")
		require.NoError(t, err)

		assert.Equal(t, "https://github.com/checksum/test2/releases/download/1.0.0/checksums.txt", resolved.ChecksumURL)
	})
}

// TestAquaCacheConsistency tests that cache is correctly used across multiple resolves.
func TestAquaCacheConsistency(t *testing.T) {
	var requestCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`packages:
  - type: github_release
    repo_owner: cache
    repo_name: test
    asset: test_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
`))
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	resolver := aqua.NewResolverWithBaseURL(cacheDir, server.URL)
	ctx := context.Background()

	// First resolve - should hit the server
	_, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "cache/test", "1.0.0", "linux", "amd64")
	require.NoError(t, err)
	assert.Equal(t, int32(1), requestCount.Load(), "first resolve should hit the server")

	// Second resolve with same parameters - should use cache
	_, err = resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "cache/test", "2.0.0", "linux", "amd64")
	require.NoError(t, err)
	assert.Equal(t, int32(1), requestCount.Load(), "second resolve should use cache, no additional request")

	// Verify cache file exists
	cachePath := filepath.Join(cacheDir, "v4.465.0", "pkgs", "cache", "test", "registry.yaml")
	_, err = os.Stat(cachePath)
	assert.NoError(t, err, "cache file should exist")
}

// TestAquaErrorHandling tests error scenarios.
func TestAquaErrorHandling(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v4.465.0/pkgs/unsupported/tool/registry.yaml":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`packages:
  - type: github_release
    repo_owner: unsupported
    repo_name: tool
    asset: test.tar.gz
    supported_envs:
      - darwin
      - linux/amd64
`))
		case "/v4.465.0/pkgs/invalid/template/registry.yaml":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`packages:
  - type: github_release
    repo_owner: invalid
    repo_name: template
    asset: "test_{{.Invalid}}.tar.gz"
`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	resolver := aqua.NewResolverWithBaseURL(cacheDir, server.URL)
	ctx := context.Background()

	t.Run("package not found returns error", func(t *testing.T) {
		_, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "nonexistent/package", "1.0.0", "linux", "amd64")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("unsupported environment is reported in errors", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "unsupported/tool", "1.0.0", "linux", "arm64")
		require.NoError(t, err)

		// linux/arm64 is not in supported_envs (only darwin and linux/amd64)
		assert.NotEmpty(t, resolved.Errors, "should have errors for unsupported env")
	})
}

// TestAquaReplacementsMerge tests that replacements are correctly merged
// between base, version_overrides, and OS overrides.
func TestAquaReplacementsMerge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`packages:
  - type: github_release
    repo_owner: merge
    repo_name: test
    asset: test_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    replacements:
      darwin: macos
      linux: linux
      amd64: x86_64
      arm64: aarch64
    version_overrides:
      - version_constraint: semver(">= 2.0.0")
        replacements:
          darwin: Darwin
        overrides:
          - goos: linux
            replacements:
              linux: Linux
`))
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	resolver := aqua.NewResolverWithBaseURL(cacheDir, server.URL)
	ctx := context.Background()

	t.Run("version override replacements replace base (no merge)", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "merge/test", "2.5.0", "darwin", "arm64")
		require.NoError(t, err)

		// version_overrides.replacements completely replaces base replacements (no merge)
		// darwin→Darwin from version override is applied
		// arm64 has no replacement in version override, so stays as arm64
		assert.Equal(t, "https://github.com/merge/test/releases/download/2.5.0/test_2.5.0_Darwin_arm64.tar.gz", resolved.URL)
	})

	t.Run("OS override replacements merge with version override", func(t *testing.T) {
		resolved, err := resolver.ResolveWithOS(ctx, aqua.RegistryRef("v4.465.0"), "merge/test", "2.5.0", "linux", "amd64")
		require.NoError(t, err)

		// version_overrides.replacements: only darwin→Darwin (replaces base completely)
		// OS override (goos: linux): adds linux→Linux
		// applyOSOverrides merges: darwin→Darwin + linux→Linux
		// amd64 has no replacement anywhere, so stays as amd64
		assert.Equal(t, "https://github.com/merge/test/releases/download/2.5.0/test_2.5.0_Linux_amd64.tar.gz", resolved.URL)
	})
}
