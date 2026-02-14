package aqua

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchVersionConstraint(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		constraint string
		version    string
		want       bool
	}{
		// "true" always matches
		{name: "true with v prefix", constraint: "true", version: "v1.0.0", want: true},
		{name: "true with v2.5.0", constraint: "true", version: "v2.5.0", want: true},
		{name: "true without v prefix", constraint: "true", version: "0.1.0", want: true},

		// empty string always matches
		{name: "empty with v prefix", constraint: "", version: "v1.0.0", want: true},
		{name: "empty with v2.5.0", constraint: "", version: "v2.5.0", want: true},
		{name: "empty without v prefix", constraint: "", version: "0.1.0", want: true},

		// semver("< X.Y.Z")
		{name: "less than - match v0.9.0", constraint: `semver("< 1.0.0")`, version: "v0.9.0", want: true},
		{name: "less than - match 0.5.0", constraint: `semver("< 1.0.0")`, version: "0.5.0", want: true},
		{name: "less than - no match v1.0.0", constraint: `semver("< 1.0.0")`, version: "v1.0.0", want: false},
		{name: "less than - no match v2.0.0", constraint: `semver("< 1.0.0")`, version: "v2.0.0", want: false},

		// semver(">= X.Y.Z")
		{name: "gte - match v2.0.0", constraint: `semver(">= 2.0.0")`, version: "v2.0.0", want: true},
		{name: "gte - match v3.0.0", constraint: `semver(">= 2.0.0")`, version: "v3.0.0", want: true},
		{name: "gte - no match v1.9.9", constraint: `semver(">= 2.0.0")`, version: "v1.9.9", want: false},

		// semver("<= X.Y.Z")
		{name: "lte - match v0.4.0", constraint: `semver("<= 0.4.0")`, version: "v0.4.0", want: true},
		{name: "lte - match v0.3.0", constraint: `semver("<= 0.4.0")`, version: "v0.3.0", want: true},
		{name: "lte - no match v0.5.0", constraint: `semver("<= 0.4.0")`, version: "v0.5.0", want: false},

		// semver range
		{name: "range - match v1.0.0", constraint: `semver(">= 1.0.0, < 2.0.0")`, version: "v1.0.0", want: true},
		{name: "range - match v1.5.0", constraint: `semver(">= 1.0.0, < 2.0.0")`, version: "v1.5.0", want: true},
		{name: "range - no match v0.9.9", constraint: `semver(">= 1.0.0, < 2.0.0")`, version: "v0.9.9", want: false},
		{name: "range - no match v2.0.0", constraint: `semver(">= 1.0.0, < 2.0.0")`, version: "v2.0.0", want: false},

		// invalid constraint format
		{name: "invalid constraint foo", constraint: "foo", version: "v1.0.0", want: false},
		{name: "invalid constraint text", constraint: "invalid", version: "v1.0.0", want: false},

		// invalid version
		{name: "invalid version text", constraint: `semver(">= 1.0.0")`, version: "invalid", want: false},
		{name: "invalid version empty", constraint: `semver(">= 1.0.0")`, version: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := matchVersionConstraint(tt.constraint, tt.version)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestApplyVersionOverrides(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		info    *PackageInfo
		version string
		check   func(t *testing.T, result *PackageInfo)
	}{
		{
			name: "no overrides",
			info: &PackageInfo{
				Type:      "github_release",
				RepoOwner: "cli",
				RepoName:  "cli",
				Asset:     "gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz",
				Format:    "tar.gz",
			},
			version: "v2.0.0",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz", result.Asset)
				assert.Equal(t, "tar.gz", result.Format)
			},
		},
		{
			name: "top-level constraint match skips overrides",
			info: &PackageInfo{
				Type:              "github_release",
				Asset:             "tool_{{.SemVer}}_{{.OS}}.tar.gz",
				Format:            "tar.gz",
				VersionConstraint: `semver(">= 5.0.0")`,
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: `semver(">= 4.0.0")`,
						Asset:             "should_not_be_applied.zip",
						Format:            "zip",
					},
				},
			},
			version: "v5.8.1",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "tool_{{.SemVer}}_{{.OS}}.tar.gz", result.Asset, "top-level asset should be used")
				assert.Equal(t, "tar.gz", result.Format, "top-level format should be used")
			},
		},
		{
			name: "empty top-level constraint does not skip overrides",
			info: &PackageInfo{
				Type:   "github_release",
				Asset:  "base_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz",
				Format: "tar.gz",
				// VersionConstraint is empty (not set in registry YAML)
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: `semver(">= 2.0.0")`,
						Asset:             "v2_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz",
					},
					{
						VersionConstraint: `semver("< 2.0.0")`,
						Asset:             "legacy_{{.Version}}.zip",
						Format:            "zip",
					},
				},
			},
			version: "v2.5.0",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "v2_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz", result.Asset, "override should be applied")
				assert.Equal(t, "tar.gz", result.Format, "format should remain from base")
			},
		},
		{
			name: "no match",
			info: &PackageInfo{
				Type:              "github_release",
				Asset:             "original.tar.gz",
				Format:            "tar.gz",
				VersionConstraint: `semver(">= 3.0.0")`,
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: `semver("< 1.0.0")`,
						Asset:             "old.zip",
						Format:            "zip",
					},
				},
			},
			version: "v2.0.0",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "original.tar.gz", result.Asset)
				assert.Equal(t, "tar.gz", result.Format)
			},
		},
		{
			name: "first match wins",
			info: &PackageInfo{
				Type:              "github_release",
				Asset:             "original.tar.gz",
				Format:            "tar.gz",
				VersionConstraint: `semver(">= 3.0.0")`,
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: `semver("< 2.0.0")`,
						Asset:             "first_match.zip",
						Format:            "zip",
					},
					{
						VersionConstraint: `semver("< 1.0.0")`,
						Asset:             "second_match.tar.gz",
						Format:            "tar.gz",
					},
				},
			},
			version: "v0.5.0",
			check: func(t *testing.T, result *PackageInfo) {
				// First matching override (< 2.0.0) should be applied
				assert.Equal(t, "first_match.zip", result.Asset)
				assert.Equal(t, "zip", result.Format)
			},
		},
		{
			name: "partial override",
			info: &PackageInfo{
				Type:              "github_release",
				Asset:             "original.tar.gz",
				Format:            "tar.gz",
				VersionConstraint: `semver(">= 3.0.0")`,
				Replacements: map[string]string{
					"amd64": "x86_64",
				},
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: `semver("< 2.0.0")`,
						Asset:             "new_asset.tar.gz",
						// Format and Replacements not specified
					},
				},
			},
			version: "v1.5.0",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "new_asset.tar.gz", result.Asset)
				assert.Equal(t, "tar.gz", result.Format)                // unchanged
				assert.Equal(t, "x86_64", result.Replacements["amd64"]) // unchanged
			},
		},
		{
			name: "checksum override",
			info: &PackageInfo{
				Type:              "github_release",
				Asset:             "original.tar.gz",
				Format:            "tar.gz",
				VersionConstraint: `semver(">= 3.0.0")`,
				Checksum: &ChecksumSpec{
					Type:      "github_release",
					Asset:     "checksums.txt",
					Algorithm: "sha256",
				},
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: `semver("< 2.0.0")`,
						Checksum: &ChecksumSpec{
							Type:      "github_release",
							Asset:     "SHA256SUMS",
							Algorithm: "sha256",
						},
					},
				},
			},
			version: "v1.5.0",
			check: func(t *testing.T, result *PackageInfo) {
				assert.NotNil(t, result.Checksum)
				assert.Equal(t, "SHA256SUMS", result.Checksum.Asset)
			},
		},
		{
			name: "replacements override",
			info: &PackageInfo{
				Type:              "github_release",
				Asset:             "original.tar.gz",
				Format:            "tar.gz",
				VersionConstraint: `semver(">= 3.0.0")`,
				Replacements: map[string]string{
					"amd64": "x86_64",
				},
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: `semver("< 2.0.0")`,
						Replacements: map[string]string{
							"amd64":  "amd64",
							"darwin": "macOS",
						},
					},
				},
			},
			version: "v1.5.0",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "amd64", result.Replacements["amd64"])
				assert.Equal(t, "macOS", result.Replacements["darwin"])
			},
		},
		{
			name: "overrides override",
			info: &PackageInfo{
				Type:              "github_release",
				Asset:             "original.tar.gz",
				Format:            "tar.gz",
				VersionConstraint: `semver(">= 3.0.0")`,
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: `semver("< 2.0.0")`,
						Overrides: []Override{
							{
								GOOS:   "windows",
								Format: "zip",
							},
						},
					},
				},
			},
			version: "v1.5.0",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Len(t, result.Overrides, 1)
				assert.Equal(t, "windows", result.Overrides[0].GOOS)
				assert.Equal(t, "zip", result.Overrides[0].Format)
			},
		},
		{
			name: "URL override (type: http)",
			info: &PackageInfo{
				Type:              "http",
				RepoOwner:         "kubernetes",
				RepoName:          "kubectl",
				VersionConstraint: `semver(">= 99.0.0")`,
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: "true",
						URL:               "https://dl.k8s.io/{{.Version}}/bin/{{.OS}}/{{.Arch}}/kubectl",
					},
				},
			},
			version: "v1.35.1",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "https://dl.k8s.io/{{.Version}}/bin/{{.OS}}/{{.Arch}}/kubectl", result.URL)
			},
		},
		{
			name: "version_prefix override",
			info: &PackageInfo{
				Type:              "github_release",
				RepoOwner:         "kubernetes-sigs",
				RepoName:          "kustomize",
				Asset:             "kustomize_{{.SemVer}}_{{.OS}}_{{.Arch}}.tar.gz",
				VersionPrefix:     "kustomize/",
				VersionConstraint: `semver(">= 5.0.0")`,
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: `semver("< 4.0.0")`,
						VersionPrefix:     strPtr(""),
					},
				},
			},
			version: "v3.5.0",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Empty(t, result.VersionPrefix, "version_prefix should be cleared by override")
			},
		},
		{
			name: "combined override (URL + VersionPrefix + Format)",
			info: &PackageInfo{
				Type:              "github_release",
				RepoOwner:         "example",
				RepoName:          "tool",
				Asset:             "tool_{{.SemVer}}_{{.OS}}_{{.Arch}}.tar.gz",
				Format:            "tar.gz",
				VersionPrefix:     "app/",
				VersionConstraint: `semver(">= 3.0.0")`,
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: `semver("< 2.0.0")`,
						URL:               "https://cdn.example.com/{{.Version}}/tool.zip",
						VersionPrefix:     strPtr("legacy/"),
						Format:            "zip",
						Asset:             "tool_legacy.zip",
					},
				},
			},
			version: "v1.5.0",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "https://cdn.example.com/{{.Version}}/tool.zip", result.URL)
				assert.Equal(t, "legacy/", result.VersionPrefix)
				assert.Equal(t, "zip", result.Format)
				assert.Equal(t, "tool_legacy.zip", result.Asset)
			},
		},
		{
			name: "version_prefix not overridden when nil",
			info: &PackageInfo{
				Type:              "github_release",
				RepoOwner:         "kubernetes-sigs",
				RepoName:          "kustomize",
				Asset:             "kustomize_{{.SemVer}}_{{.OS}}_{{.Arch}}.tar.gz",
				VersionPrefix:     "kustomize/",
				VersionConstraint: `semver(">= 6.0.0")`,
				VersionOverrides: []VersionOverride{
					{
						VersionConstraint: "true",
						Asset:             "new_asset.tar.gz",
						// VersionPrefix not set (nil)
					},
				},
			},
			version: "v5.8.1",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "kustomize/", result.VersionPrefix, "version_prefix should be preserved")
				assert.Equal(t, "new_asset.tar.gz", result.Asset)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ApplyVersionOverrides(tt.info, tt.version)
			tt.check(t, result)
		})
	}
}

func strPtr(s string) *string {
	return &s
}
