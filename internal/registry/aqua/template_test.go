package aqua

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderTemplate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		template string
		vars     TemplateVars
		want     string
		wantErr  bool
	}{
		{
			name:     "basic variable substitution",
			template: "{{.OS}}_{{.Arch}}",
			vars: TemplateVars{
				OS:   "linux",
				Arch: "amd64",
			},
			want: "linux_amd64",
		},
		{
			name:     "version with v prefix",
			template: "tool_{{.Version}}_{{.OS}}_{{.Arch}}.tar.gz",
			vars: TemplateVars{
				Version: "v1.2.3",
				OS:      "darwin",
				Arch:    "arm64",
			},
			want: "tool_v1.2.3_darwin_arm64.tar.gz",
		},
		{
			name:     "semver equals version when no prefix",
			template: "tool_{{.SemVer}}_{{.OS}}_{{.Arch}}.tar.gz",
			vars: TemplateVars{
				Version: "v1.2.3",
				SemVer:  "v1.2.3",
				OS:      "darwin",
				Arch:    "arm64",
			},
			want: "tool_v1.2.3_darwin_arm64.tar.gz",
		},
		{
			name:     "semver with version_prefix stripped retains v",
			template: "kustomize_{{.SemVer}}_{{.OS}}_{{.Arch}}.tar.gz",
			vars: TemplateVars{
				Version: "v5.8.1",
				SemVer:  "v5.8.1", // version_prefix "kustomize/" stripped, v retained
				OS:      "linux",
				Arch:    "amd64",
			},
			want: "kustomize_v5.8.1_linux_amd64.tar.gz",
		},
		{
			name:     "trimV on SemVer to remove v prefix",
			template: "tool_{{trimV .SemVer}}_{{.OS}}_{{.Arch}}.tar.gz",
			vars: TemplateVars{
				Version: "v2.0.0",
				SemVer:  "v2.0.0",
				OS:      "darwin",
				Arch:    "arm64",
			},
			want: "tool_2.0.0_darwin_arm64.tar.gz",
		},
		{
			name:     "trimV function",
			template: "gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz",
			vars: TemplateVars{
				Version: "v2.86.0",
				OS:      "linux",
				Arch:    "amd64",
			},
			want: "gh_2.86.0_linux_amd64.tar.gz",
		},
		{
			name:     "trimV with no v prefix",
			template: "gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.tar.gz",
			vars: TemplateVars{
				Version: "2.86.0",
				OS:      "linux",
				Arch:    "amd64",
			},
			want: "gh_2.86.0_linux_amd64.tar.gz",
		},
		{
			name:     "trimPrefix function",
			template: "{{trimPrefix .OS \"darwin\"}}",
			vars: TemplateVars{
				OS: "darwin",
			},
			want: "",
		},
		{
			name:     "trimPrefix no match",
			template: "{{trimPrefix .OS \"linux\"}}",
			vars: TemplateVars{
				OS: "darwin",
			},
			want: "darwin",
		},
		{
			name:     "trimSuffix function",
			template: "{{trimSuffix .Format \".gz\"}}",
			vars: TemplateVars{
				Format: "tar.gz",
			},
			want: "tar",
		},
		{
			name:     "complex aqua asset pattern - gh",
			template: "gh_{{trimV .Version}}_{{.OS}}_{{.Arch}}.{{.Format}}",
			vars: TemplateVars{
				Version: "v2.86.0",
				OS:      "macOS",
				Arch:    "arm64",
				Format:  "zip",
			},
			want: "gh_2.86.0_macOS_arm64.zip",
		},
		{
			name:     "complex aqua asset pattern - ripgrep",
			template: "ripgrep-{{trimV .Version}}-{{.Arch}}-{{.OS}}.tar.gz",
			vars: TemplateVars{
				Version: "v14.0.0",
				OS:      "unknown-linux-musl",
				Arch:    "x86_64",
			},
			want: "ripgrep-14.0.0-x86_64-unknown-linux-musl.tar.gz",
		},
		{
			name:     "title function - goreleaser pattern",
			template: "goreleaser_{{title .OS}}_{{.Arch}}.{{.Format}}",
			vars: TemplateVars{
				OS:     "linux",
				Arch:   "x86_64",
				Format: "tar.gz",
			},
			want: "goreleaser_Linux_x86_64.tar.gz",
		},
		{
			name:     "title function - darwin",
			template: "{{title .OS}}",
			vars:     TemplateVars{OS: "darwin"},
			want:     "Darwin",
		},
		{
			name:     "title function - empty string",
			template: "{{title .OS}}",
			vars:     TemplateVars{OS: ""},
			want:     "",
		},
		{
			name:     "title function - single character",
			template: "{{title .OS}}",
			vars:     TemplateVars{OS: "a"},
			want:     "A",
		},
		{
			name:     "title function - already capitalized",
			template: "{{title .OS}}",
			vars:     TemplateVars{OS: "Linux"},
			want:     "Linux",
		},
		{
			name:     "title function - all uppercase",
			template: "{{title .OS}}",
			vars:     TemplateVars{OS: "LINUX"},
			want:     "LINUX",
		},
		{
			// Note: unlike Sprig's title (strings.Title) which uppercases each word,
			// our title only uppercases the first rune. This is sufficient for
			// aqua-registry where inputs are single-word OS names.
			name:     "title function - multi-word differs from Sprig",
			template: "{{title .OS}}",
			vars:     TemplateVars{OS: "hello world"},
			want:     "Hello world",
		},
		{
			name:     "tolower function",
			template: "{{tolower .OS}}",
			vars:     TemplateVars{OS: "Darwin"},
			want:     "darwin",
		},
		{
			name:     "toupper function",
			template: "{{toupper .OS}}",
			vars:     TemplateVars{OS: "linux"},
			want:     "LINUX",
		},
		{
			name:     "title with trimV combined - porter pattern",
			template: "porter_{{.Version}}_{{title .OS}}_x86_64.zip",
			vars: TemplateVars{
				Version: "v1.0.0",
				OS:      "linux",
			},
			want: "porter_v1.0.0_Linux_x86_64.zip",
		},
		{
			name:     "AssetWithoutExt variable",
			template: "{{.AssetWithoutExt}}",
			vars:     TemplateVars{AssetWithoutExt: "fd-v10.3.0-x86_64-unknown-linux-gnu"},
			want:     "fd-v10.3.0-x86_64-unknown-linux-gnu",
		},
		{
			name:     "invalid template syntax",
			template: "{{.Invalid",
			vars:     TemplateVars{},
			wantErr:  true,
		},
		{
			name:     "undefined field access",
			template: "{{.Undefined}}",
			vars:     TemplateVars{},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := RenderTemplate(tt.template, tt.vars)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTrimArchiveExtension(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"tar.gz", "fd-v10.3.0-x86_64-unknown-linux-gnu.tar.gz", "fd-v10.3.0-x86_64-unknown-linux-gnu"},
		{"tar.xz", "tool.tar.xz", "tool"},
		{"zip", "tool.zip", "tool"},
		{"tgz", "tool.tgz", "tool"},
		{"txz", "tool.txz", "tool"},
		{"pkg", "tool.pkg", "tool"},
		{"gz", "tool.gz", "tool"},
		{"case insensitive", "Tool.TAR.GZ", "Tool"},
		{"no extension", "yq_linux_amd64", "yq_linux_amd64"},
		{"empty", "", ""},
		{"non-archive suffix", "tool.tar.gz.sig", "tool.tar.gz.sig"},
		{"tar.bz2 not recognized", "tool.tar.bz2", "tool.tar.bz2"},
		{"extension only", ".tar.gz", ""},
		{"extension only zip", ".zip", ""},
		{"dot prefix with extension", "..tar.gz", "."},
		{"double tar.gz strips trailing only", "tool.tar.gz.tar.gz", "tool.tar.gz"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TrimArchiveExtension(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
