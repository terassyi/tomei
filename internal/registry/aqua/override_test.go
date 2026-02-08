package aqua

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyOSOverrides(t *testing.T) {
	tests := []struct {
		name   string
		info   *PackageInfo
		goos   string
		goarch string
		check  func(t *testing.T, result *PackageInfo)
	}{
		{
			name: "no overrides",
			info: &PackageInfo{
				Type:   "github_release",
				Asset:  "tool_{{.OS}}_{{.Arch}}.tar.gz",
				Format: "tar.gz",
			},
			goos:   "darwin",
			goarch: "arm64",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "tool_{{.OS}}_{{.Arch}}.tar.gz", result.Asset)
				assert.Equal(t, "tar.gz", result.Format)
			},
		},
		{
			name: "match goos only",
			info: &PackageInfo{
				Type:   "github_release",
				Asset:  "tool_{{.OS}}_{{.Arch}}.tar.gz",
				Format: "tar.gz",
				Overrides: []Override{
					{
						GOOS:   "windows",
						Format: "zip",
					},
				},
			},
			goos:   "windows",
			goarch: "amd64",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "zip", result.Format)
			},
		},
		{
			name: "match goarch only",
			info: &PackageInfo{
				Type:   "github_release",
				Asset:  "tool_{{.OS}}_{{.Arch}}.tar.gz",
				Format: "tar.gz",
				Overrides: []Override{
					{
						GOArch: "arm64",
						Asset:  "tool_{{.OS}}_aarch64.tar.gz",
					},
				},
			},
			goos:   "linux",
			goarch: "arm64",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "tool_{{.OS}}_aarch64.tar.gz", result.Asset)
			},
		},
		{
			name: "match goos and goarch",
			info: &PackageInfo{
				Type:   "github_release",
				Asset:  "tool_{{.OS}}_{{.Arch}}.tar.gz",
				Format: "tar.gz",
				Overrides: []Override{
					{
						GOOS:   "darwin",
						GOArch: "arm64",
						Asset:  "tool_macOS_aarch64.tar.gz",
						Format: "tar.gz",
					},
				},
			},
			goos:   "darwin",
			goarch: "arm64",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "tool_macOS_aarch64.tar.gz", result.Asset)
			},
		},
		{
			name: "no match",
			info: &PackageInfo{
				Type:   "github_release",
				Asset:  "tool_{{.OS}}_{{.Arch}}.tar.gz",
				Format: "tar.gz",
				Overrides: []Override{
					{
						GOOS:   "windows",
						Format: "zip",
					},
				},
			},
			goos:   "linux",
			goarch: "amd64",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "tar.gz", result.Format)
			},
		},
		{
			name: "first match wins",
			info: &PackageInfo{
				Type:   "github_release",
				Asset:  "original.tar.gz",
				Format: "tar.gz",
				Overrides: []Override{
					{
						GOOS:  "darwin",
						Asset: "first_match.tar.gz",
					},
					{
						GOOS:   "darwin",
						GOArch: "arm64",
						Asset:  "second_match.tar.gz",
					},
				},
			},
			goos:   "darwin",
			goarch: "arm64",
			check: func(t *testing.T, result *PackageInfo) {
				// First matching override (goos only) should be applied
				assert.Equal(t, "first_match.tar.gz", result.Asset)
			},
		},
		{
			name: "replacements override",
			info: &PackageInfo{
				Type:   "github_release",
				Asset:  "tool_{{.OS}}_{{.Arch}}.tar.gz",
				Format: "tar.gz",
				Replacements: map[string]string{
					"amd64": "x86_64",
				},
				Overrides: []Override{
					{
						GOOS: "darwin",
						Replacements: map[string]string{
							"darwin": "macOS",
							"amd64":  "x86_64",
						},
					},
				},
			},
			goos:   "darwin",
			goarch: "amd64",
			check: func(t *testing.T, result *PackageInfo) {
				assert.Equal(t, "macOS", result.Replacements["darwin"])
				assert.Equal(t, "x86_64", result.Replacements["amd64"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyOSOverrides(tt.info, tt.goos, tt.goarch)
			tt.check(t, result)
		})
	}
}

func TestMatchesOS(t *testing.T) {
	tests := []struct {
		name     string
		override Override
		goos     string
		goarch   string
		want     bool
	}{
		{
			name:     "empty override matches all",
			override: Override{},
			goos:     "linux",
			goarch:   "amd64",
			want:     true,
		},
		{
			name:     "goos match",
			override: Override{GOOS: "darwin"},
			goos:     "darwin",
			goarch:   "amd64",
			want:     true,
		},
		{
			name:     "goos no match",
			override: Override{GOOS: "darwin"},
			goos:     "linux",
			goarch:   "amd64",
			want:     false,
		},
		{
			name:     "goarch match",
			override: Override{GOArch: "arm64"},
			goos:     "darwin",
			goarch:   "arm64",
			want:     true,
		},
		{
			name:     "goarch no match",
			override: Override{GOArch: "arm64"},
			goos:     "darwin",
			goarch:   "amd64",
			want:     false,
		},
		{
			name:     "both match",
			override: Override{GOOS: "darwin", GOArch: "arm64"},
			goos:     "darwin",
			goarch:   "arm64",
			want:     true,
		},
		{
			name:     "goos match goarch no match",
			override: Override{GOOS: "darwin", GOArch: "arm64"},
			goos:     "darwin",
			goarch:   "amd64",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesOS(tt.override, tt.goos, tt.goarch)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestApplyReplacement(t *testing.T) {
	tests := []struct {
		name         string
		replacements map[string]string
		key          string
		want         string
	}{
		{
			name:         "nil replacements",
			replacements: nil,
			key:          "amd64",
			want:         "amd64",
		},
		{
			name:         "empty replacements",
			replacements: map[string]string{},
			key:          "amd64",
			want:         "amd64",
		},
		{
			name: "key found",
			replacements: map[string]string{
				"amd64": "x86_64",
			},
			key:  "amd64",
			want: "x86_64",
		},
		{
			name: "key not found",
			replacements: map[string]string{
				"amd64": "x86_64",
			},
			key:  "arm64",
			want: "arm64",
		},
		{
			name: "darwin to macOS",
			replacements: map[string]string{
				"darwin": "macOS",
				"linux":  "Linux",
			},
			key:  "darwin",
			want: "macOS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := applyReplacement(tt.replacements, tt.key)
			assert.Equal(t, tt.want, got)
		})
	}
}
