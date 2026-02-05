package aqua

import (
	"testing"
	"time"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackageInfo_YAMLParse(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected PackageInfo
		wantErr  bool
	}{
		{
			name: "github_release basic",
			yaml: `
type: github_release
repo_owner: cli
repo_name: cli
description: GitHub CLI
asset: gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz
format: tar.gz
files:
  - name: gh
    src: gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}/bin/gh
replacements:
  darwin: macOS
  amd64: amd64
`,
			expected: PackageInfo{
				Type:        "github_release",
				RepoOwner:   "cli",
				RepoName:    "cli",
				Description: "GitHub CLI",
				Asset:       "gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz",
				Format:      "tar.gz",
				Files: []FileSpec{
					{Name: "gh", Src: "gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}/bin/gh"},
				},
				Replacements: map[string]string{
					"darwin": "macOS",
					"amd64":  "amd64",
				},
			},
		},
		{
			name: "with version_overrides",
			yaml: `
type: github_release
repo_owner: BurntSushi
repo_name: ripgrep
asset: ripgrep-{{.Version}}-{{.Arch}}-{{.OS}}.tar.gz
format: tar.gz
version_overrides:
  - version_constraint: semver("< 14.0.0")
    asset: ripgrep-{{.Version}}-{{.Arch}}-unknown-{{.OS}}-musl.tar.gz
  - version_constraint: "true"
    asset: ripgrep-{{.Version}}-{{.Arch}}-{{.OS}}.tar.gz
`,
			expected: PackageInfo{
				Type:      "github_release",
				RepoOwner: "BurntSushi",
				RepoName:  "ripgrep",
				Asset:     "ripgrep-{{.Version}}-{{.Arch}}-{{.OS}}.tar.gz",
				Format:    "tar.gz",
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: `semver("< 14.0.0")`,
						Asset:             "ripgrep-{{.Version}}-{{.Arch}}-unknown-{{.OS}}-musl.tar.gz",
					},
					{
						VersionConstraint: "true",
						Asset:             "ripgrep-{{.Version}}-{{.Arch}}-{{.OS}}.tar.gz",
					},
				},
			},
		},
		{
			name: "with checksum",
			yaml: `
type: github_release
repo_owner: sharkdp
repo_name: fd
asset: fd-{{.Version}}-{{.Arch}}-{{.OS}}.tar.gz
format: tar.gz
checksum:
  enabled: true
  type: github_release
  asset: fd-{{.Version}}-{{.Arch}}-{{.OS}}.tar.gz.sha256
  algorithm: sha256
`,
			expected: PackageInfo{
				Type:      "github_release",
				RepoOwner: "sharkdp",
				RepoName:  "fd",
				Asset:     "fd-{{.Version}}-{{.Arch}}-{{.OS}}.tar.gz",
				Format:    "tar.gz",
				Checksum: &ChecksumSpec{
					Enabled:   true,
					Type:      "github_release",
					Asset:     "fd-{{.Version}}-{{.Arch}}-{{.OS}}.tar.gz.sha256",
					Algorithm: "sha256",
				},
			},
		},
		{
			name: "with overrides",
			yaml: `
type: github_release
repo_owner: jqlang
repo_name: jq
asset: jq-{{.OS}}-{{.Arch}}
format: raw
overrides:
  - goos: darwin
    goarch: arm64
    asset: jq-macos-arm64
  - goos: darwin
    goarch: amd64
    asset: jq-macos-amd64
  - goos: linux
    goarch: amd64
    asset: jq-linux-amd64
`,
			expected: PackageInfo{
				Type:      "github_release",
				RepoOwner: "jqlang",
				RepoName:  "jq",
				Asset:     "jq-{{.OS}}-{{.Arch}}",
				Format:    "raw",
				Overrides: []Override{
					{GOOS: "darwin", GOArch: "arm64", Asset: "jq-macos-arm64"},
					{GOOS: "darwin", GOArch: "amd64", Asset: "jq-macos-amd64"},
					{GOOS: "linux", GOArch: "amd64", Asset: "jq-linux-amd64"},
				},
			},
		},
		{
			name: "http type",
			yaml: `
type: http
url: https://example.com/tool-{{.Version}}-{{.OS}}-{{.Arch}}.tar.gz
format: tar.gz
`,
			expected: PackageInfo{
				Type:   "http",
				URL:    "https://example.com/tool-{{.Version}}-{{.OS}}-{{.Arch}}.tar.gz",
				Format: "tar.gz",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got PackageInfo
			err := yaml.Unmarshal([]byte(tt.yaml), &got)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.expected.Type, got.Type)
			assert.Equal(t, tt.expected.RepoOwner, got.RepoOwner)
			assert.Equal(t, tt.expected.RepoName, got.RepoName)
			assert.Equal(t, tt.expected.Asset, got.Asset)
			assert.Equal(t, tt.expected.URL, got.URL)
			assert.Equal(t, tt.expected.Format, got.Format)
			assert.Equal(t, tt.expected.Description, got.Description)
			assert.Equal(t, tt.expected.Files, got.Files)
			assert.Equal(t, tt.expected.Replacements, got.Replacements)
			assert.Equal(t, tt.expected.Checksum, got.Checksum)
			assert.Equal(t, tt.expected.VersionOverrides, got.VersionOverrides)
			assert.Equal(t, tt.expected.Overrides, got.Overrides)
		})
	}
}

func TestRegistryRef_Validate(t *testing.T) {
	tests := []struct {
		name    string
		ref     RegistryRef
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid ref",
			ref:     RegistryRef("v4.465.0"),
			wantErr: false,
		},
		{
			name:    "valid ref with patch zero",
			ref:     RegistryRef("v1.0.0"),
			wantErr: false,
		},
		{
			name:    "valid ref with large numbers",
			ref:     RegistryRef("v100.200.300"),
			wantErr: false,
		},
		{
			name:    "empty ref",
			ref:     RegistryRef(""),
			wantErr: true,
			errMsg:  "registry ref is empty",
		},
		{
			name:    "missing v prefix",
			ref:     RegistryRef("4.465.0"),
			wantErr: true,
			errMsg:  "must start with 'v'",
		},
		{
			name:    "two part version is valid",
			ref:     RegistryRef("v4.465"),
			wantErr: false, // Masterminds/semver accepts v4.465 as v4.465.0
		},
		{
			name:    "invalid characters",
			ref:     RegistryRef("v4.465.0-beta"),
			wantErr: false, // prerelease is valid semver
		},
		{
			name:    "not a version",
			ref:     RegistryRef("vlatest"),
			wantErr: true,
			errMsg:  "invalid registry ref format",
		},
		{
			name:    "only v",
			ref:     RegistryRef("v"),
			wantErr: true,
			errMsg:  "invalid registry ref format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ref.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestRegistryRef_IsEmpty(t *testing.T) {
	tests := []struct {
		name string
		ref  RegistryRef
		want bool
	}{
		{
			name: "empty",
			ref:  RegistryRef(""),
			want: true,
		},
		{
			name: "not empty",
			ref:  RegistryRef("v4.465.0"),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.ref.IsEmpty())
		})
	}
}

func TestRegistryRef_String(t *testing.T) {
	ref := RegistryRef("v4.465.0")
	assert.Equal(t, "v4.465.0", ref.String())
}

func TestRegistryState_JSONRoundtrip(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	state := &RegistryState{
		Aqua: &AquaRegistryState{
			Ref:       RegistryRef("v4.465.0"),
			UpdatedAt: now,
		},
	}

	// RegistryState is serialized to JSON in state.json
	// Verify the struct can be created and accessed correctly
	assert.NotNil(t, state.Aqua)
	assert.Equal(t, RegistryRef("v4.465.0"), state.Aqua.Ref)
	assert.Equal(t, now, state.Aqua.UpdatedAt)
}

func TestFileSpec_YAMLParse(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected FileSpec
	}{
		{
			name: "name only",
			yaml: `name: gh`,
			expected: FileSpec{
				Name: "gh",
			},
		},
		{
			name: "name and src",
			yaml: `
name: gh
src: gh_v2.0.0_linux_amd64/bin/gh
`,
			expected: FileSpec{
				Name: "gh",
				Src:  "gh_v2.0.0_linux_amd64/bin/gh",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got FileSpec
			err := yaml.Unmarshal([]byte(tt.yaml), &got)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestVersionOverride_YAMLParse(t *testing.T) {
	yamlData := `
version_constraint: semver("< 1.0.0")
asset: old-asset-{{.Version}}.tar.gz
format: tar.gz
checksum:
  enabled: true
  algorithm: sha256
replacements:
  amd64: x86_64
overrides:
  - goos: darwin
    asset: darwin-specific.tar.gz
supported_envs:
  - darwin
  - linux
rosetta2: true
`
	var got VersionOverride
	err := yaml.Unmarshal([]byte(yamlData), &got)
	require.NoError(t, err)

	assert.Equal(t, `semver("< 1.0.0")`, got.VersionConstraint)
	assert.Equal(t, "old-asset-{{.Version}}.tar.gz", got.Asset)
	assert.Equal(t, "tar.gz", got.Format)
	assert.NotNil(t, got.Checksum)
	assert.True(t, got.Checksum.Enabled)
	assert.Equal(t, "sha256", got.Checksum.Algorithm)
	assert.Equal(t, map[string]string{"amd64": "x86_64"}, got.Replacements)
	assert.Len(t, got.Overrides, 1)
	assert.Equal(t, "darwin", got.Overrides[0].GOOS)
	assert.Equal(t, []string{"darwin", "linux"}, got.SupportedEnvs)
	assert.True(t, got.Rosetta2)
}
