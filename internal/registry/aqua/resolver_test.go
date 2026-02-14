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

func TestResolver_Resolve_GitHubRelease(t *testing.T) {
	t.Parallel()
	// Setup: create cache with package info
	cacheDir := t.TempDir()
	ref := RegistryRef("v4.465.0")
	pkg := "cli/cli"

	registryYAML := `packages:
  - type: github_release
    repo_owner: cli
    repo_name: cli
    asset: gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    replacements:
      darwin: macOS
    checksum:
      type: github_release
      asset: gh_{{trimV .Version}}_checksums.txt
      algorithm: sha256
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	resolver := NewResolver(cacheDir, nil)

	// Test
	result, err := resolver.ResolveWithOS(context.Background(), ref, pkg, "v2.86.0", "darwin", "arm64")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_macOS_arm64.tar.gz", result.URL)
	assert.Equal(t, "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_checksums.txt", result.ChecksumURL)
	assert.Equal(t, "sha256", result.Algorithm)
	assert.Equal(t, "tar.gz", result.Format)
	assert.Empty(t, result.Errors)
}

func TestResolver_Resolve_HTTP(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	ref := RegistryRef("v4.465.0")
	pkg := "example/tool"

	registryYAML := `packages:
  - type: http
    repo_owner: example
    repo_name: tool
    url: https://example.com/releases/{{trimV .Version}}/tool_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	resolver := NewResolver(cacheDir, nil)

	result, err := resolver.ResolveWithOS(context.Background(), ref, pkg, "v1.0.0", "linux", "amd64")

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/releases/1.0.0/tool_linux_amd64.tar.gz", result.URL)
}

func TestResolver_Resolve_WithVersionOverrides(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	ref := RegistryRef("v4.465.0")
	pkg := "cli/cli"

	registryYAML := `packages:
  - type: github_release
    repo_owner: cli
    repo_name: cli
    asset: gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    version_overrides:
      - version_constraint: semver("< 2.0.0")
        asset: gh_old_{{trimV .Version}}_{{.OS}}_{{.Arch}}.zip
        format: zip
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	resolver := NewResolver(cacheDir, nil)

	result, err := resolver.ResolveWithOS(context.Background(), ref, pkg, "v1.5.0", "linux", "amd64")

	require.NoError(t, err)
	assert.Equal(t, "https://github.com/cli/cli/releases/download/v1.5.0/gh_old_1.5.0_linux_amd64.zip", result.URL)
	assert.Equal(t, "zip", result.Format)
	assert.Contains(t, result.Warnings, "version v1.5.0 uses legacy asset format")
}

func TestResolver_Resolve_WithOSOverrides(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	ref := RegistryRef("v4.465.0")
	pkg := "example/tool"

	registryYAML := `packages:
  - type: github_release
    repo_owner: example
    repo_name: tool
    asset: tool_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    overrides:
      - goos: windows
        asset: tool_{{.OS}}_{{.Arch}}.zip
        format: zip
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	resolver := NewResolver(cacheDir, nil)

	result, err := resolver.ResolveWithOS(context.Background(), ref, pkg, "v1.0.0", "windows", "amd64")

	require.NoError(t, err)
	assert.Equal(t, "https://github.com/example/tool/releases/download/v1.0.0/tool_windows_amd64.zip", result.URL)
	assert.Equal(t, "zip", result.Format)
}

func TestResolver_Resolve_WithReplacements(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	ref := RegistryRef("v4.465.0")
	pkg := "BurntSushi/ripgrep"

	registryYAML := `packages:
  - type: github_release
    repo_owner: BurntSushi
    repo_name: ripgrep
    asset: ripgrep-{{trimV .Version}}-{{.Arch}}-{{.OS}}.tar.gz
    format: tar.gz
    replacements:
      amd64: x86_64
      linux: unknown-linux-musl
      darwin: apple-darwin
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	resolver := NewResolver(cacheDir, nil)

	result, err := resolver.ResolveWithOS(context.Background(), ref, pkg, "v14.0.0", "linux", "amd64")

	require.NoError(t, err)
	assert.Equal(t, "https://github.com/BurntSushi/ripgrep/releases/download/v14.0.0/ripgrep-14.0.0-x86_64-unknown-linux-musl.tar.gz", result.URL)
}

func TestResolver_Resolve_UnsupportedEnv(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	ref := RegistryRef("v4.465.0")
	pkg := "example/tool"

	registryYAML := `packages:
  - type: github_release
    repo_owner: example
    repo_name: tool
    asset: tool_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    supported_envs:
      - linux/amd64
      - darwin/arm64
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	resolver := NewResolver(cacheDir, nil)

	result, err := resolver.ResolveWithOS(context.Background(), ref, pkg, "v1.0.0", "windows", "amd64")

	require.NoError(t, err)
	assert.Empty(t, result.URL)
	assert.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0], "does not support windows/amd64")
}

func TestResolver_Resolve_SupportedEnv(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	ref := RegistryRef("v4.465.0")
	pkg := "example/tool"

	registryYAML := `packages:
  - type: github_release
    repo_owner: example
    repo_name: tool
    asset: tool_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    supported_envs:
      - linux
      - darwin
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	resolver := NewResolver(cacheDir, nil)

	result, err := resolver.ResolveWithOS(context.Background(), ref, pkg, "v1.0.0", "linux", "arm64")

	require.NoError(t, err)
	assert.NotEmpty(t, result.URL)
	assert.Empty(t, result.Errors)
}

func TestResolver_Resolve_PackageNotFound(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()

	mockClient := &http.Client{
		Transport: &mockRoundTripper{
			handler: func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNotFound,
					Body:       io.NopCloser(bytes.NewBufferString("")),
					Header:     make(http.Header),
				}, nil
			},
		},
	}

	resolver := NewResolver(cacheDir, nil).WithHTTPClient(mockClient)

	_, err := resolver.ResolveWithOS(context.Background(), RegistryRef("v4.465.0"), "nonexistent/pkg", "v1.0.0", "linux", "amd64")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "package not found")
}

func TestResolver_Resolve_UsesRuntimeOS(t *testing.T) {
	t.Parallel()
	// Test that Resolve() uses runtime.GOOS and runtime.GOARCH
	cacheDir := t.TempDir()
	ref := RegistryRef("v4.465.0")
	pkg := "cli/cli"

	registryYAML := `packages:
  - type: github_release
    repo_owner: cli
    repo_name: cli
    asset: gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	resolver := NewResolver(cacheDir, nil)

	// Resolve() should work with the current runtime's OS/Arch
	result, err := resolver.Resolve(context.Background(), ref, pkg, "v2.86.0")

	require.NoError(t, err)
	assert.NotEmpty(t, result.URL)
	assert.Contains(t, result.URL, "gh_2.86.0_")
}

func TestResolver_VersionClient(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	resolver := NewResolver(cacheDir, nil)

	// VersionClient() should return a non-nil client
	client := resolver.VersionClient()
	assert.NotNil(t, client)

	// Same client should be returned on multiple calls
	client2 := resolver.VersionClient()
	assert.Same(t, client, client2)
}

func TestResolver_Resolve_NoChecksum(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	ref := RegistryRef("v4.465.0")
	pkg := "example/tool"

	// Package without checksum configuration
	registryYAML := `packages:
  - type: github_release
    repo_owner: example
    repo_name: tool
    asset: tool_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	resolver := NewResolver(cacheDir, nil)

	result, err := resolver.ResolveWithOS(context.Background(), ref, pkg, "v1.0.0", "linux", "amd64")

	require.NoError(t, err)
	assert.NotEmpty(t, result.URL)
	assert.Empty(t, result.ChecksumURL)
	assert.Empty(t, result.Algorithm)
}

func TestResolver_Resolve_UnsupportedType(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	ref := RegistryRef("v4.465.0")
	pkg := "example/tool"

	// Package with unsupported type
	registryYAML := `packages:
  - type: unknown_type
    repo_owner: example
    repo_name: tool
    asset: tool.tar.gz
    format: tar.gz
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	resolver := NewResolver(cacheDir, nil)

	_, err := resolver.ResolveWithOS(context.Background(), ref, pkg, "v1.0.0", "linux", "amd64")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported package type")
}

func TestResolver_Resolve_SupportedEnvAll(t *testing.T) {
	t.Parallel()
	cacheDir := t.TempDir()
	ref := RegistryRef("v4.465.0")
	pkg := "example/tool"

	registryYAML := `packages:
  - type: github_release
    repo_owner: example
    repo_name: tool
    asset: tool_{{.OS}}_{{.Arch}}.tar.gz
    format: tar.gz
    supported_envs:
      - all
`
	cacheFile := filepath.Join(cacheDir, ref.String(), "pkgs", pkg, "registry.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(cacheFile), 0o755))
	require.NoError(t, os.WriteFile(cacheFile, []byte(registryYAML), 0o644))

	resolver := NewResolver(cacheDir, nil)

	// Any OS/Arch should work with "all"
	result, err := resolver.ResolveWithOS(context.Background(), ref, pkg, "v1.0.0", "freebsd", "386")

	require.NoError(t, err)
	assert.NotEmpty(t, result.URL)
	assert.Empty(t, result.Errors)
}

func TestIsSupportedEnv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		supportedEnvs []string
		goos          string
		goarch        string
		want          bool
	}{
		{
			name:          "all",
			supportedEnvs: []string{"all"},
			goos:          "windows",
			goarch:        "amd64",
			want:          true,
		},
		{
			name:          "os only match",
			supportedEnvs: []string{"linux", "darwin"},
			goos:          "linux",
			goarch:        "arm64",
			want:          true,
		},
		{
			name:          "os only no match",
			supportedEnvs: []string{"linux", "darwin"},
			goos:          "windows",
			goarch:        "amd64",
			want:          false,
		},
		{
			name:          "os/arch match",
			supportedEnvs: []string{"linux/amd64", "darwin/arm64"},
			goos:          "darwin",
			goarch:        "arm64",
			want:          true,
		},
		{
			name:          "os/arch no match",
			supportedEnvs: []string{"linux/amd64", "darwin/arm64"},
			goos:          "darwin",
			goarch:        "amd64",
			want:          false,
		},
		{
			name:          "mixed formats",
			supportedEnvs: []string{"linux", "darwin/arm64"},
			goos:          "linux",
			goarch:        "arm64",
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isSupportedEnv(tt.supportedEnvs, tt.goos, tt.goarch)
			assert.Equal(t, tt.want, got)
		})
	}
}
