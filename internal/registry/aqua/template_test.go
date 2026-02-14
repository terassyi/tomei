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
			name:     "semver without v prefix",
			template: "tool_{{.SemVer}}_{{.OS}}_{{.Arch}}.tar.gz",
			vars: TemplateVars{
				SemVer: "1.2.3",
				OS:     "darwin",
				Arch:   "arm64",
			},
			want: "tool_1.2.3_darwin_arm64.tar.gz",
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
