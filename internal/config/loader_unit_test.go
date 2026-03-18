package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanDeclaredTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sources []string
		want    map[string]bool
	}{
		{
			name:    "empty source",
			sources: []string{""},
			want:    map[string]bool{},
		},
		{
			name:    "no sources",
			sources: nil,
			want:    map[string]bool{},
		},
		{
			name: "single tag os",
			sources: []string{`package tomei
_os: string @tag(os)
`},
			want: map[string]bool{"os": true},
		},
		{
			name: "multiple tags os arch headless",
			sources: []string{`package tomei
_os:       string @tag(os)
_arch:     string @tag(arch)
_headless: bool   @tag(headless,type=bool)
`},
			want: map[string]bool{"os": true, "arch": true, "headless": true},
		},
		{
			name: "no tags in source",
			sources: []string{`package tomei
tool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind: "Tool"
    metadata: name: "test"
}
`},
			want: map[string]bool{},
		},
		{
			name:    "malformed CUE graceful skip",
			sources: []string{`this is not valid CUE {{{{`},
			want:    map[string]bool{},
		},
		{
			name: "tag with options extracts name only",
			sources: []string{`package tomei
_headless: bool @tag(headless,type=bool)
`},
			want: map[string]bool{"headless": true},
		},
		{
			name: "nested field with tag",
			sources: []string{`package tomei
outer: {
    _os: string @tag(os)
}
`},
			want: map[string]bool{"os": true},
		},
		{
			name: "non-tag attribute ignored",
			sources: []string{`package tomei
_os: string @tag(os)
someField: int @other(something)
`},
			want: map[string]bool{"os": true},
		},
		{
			name: "multiple sources union tags",
			sources: []string{
				`package tomei
_os: string @tag(os)
`,
				`package tomei
_arch: string @tag(arch)
`,
			},
			want: map[string]bool{"os": true, "arch": true},
		},
		{
			name: "duplicate tags across sources",
			sources: []string{
				`package tomei
_os: string @tag(os)
`,
				`package tomei
_os: string @tag(os)
_arch: string @tag(arch)
`,
			},
			want: map[string]bool{"os": true, "arch": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := scanDeclaredTags(tt.sources...)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestScanIfTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sources []string
		want    map[string]bool
	}{
		{
			name:    "no sources",
			sources: nil,
			want:    map[string]bool{},
		},
		{
			name: "@if(darwin)",
			sources: []string{`@if(darwin)
package tomei
`},
			want: map[string]bool{"darwin": true},
		},
		{
			name: "@if(!darwin) still references darwin",
			sources: []string{`@if(!darwin)
package tomei
`},
			want: map[string]bool{"darwin": true},
		},
		{
			name: "@if(darwin && arm64)",
			sources: []string{`@if(darwin && arm64)
package tomei
`},
			want: map[string]bool{"darwin": true, "arm64": true},
		},
		{
			name: "@if(linux || darwin)",
			sources: []string{`@if(linux || darwin)
package tomei
`},
			want: map[string]bool{"linux": true, "darwin": true},
		},
		{
			name: "@if(headless)",
			sources: []string{`@if(headless)
package tomei
`},
			want: map[string]bool{"headless": true},
		},
		{
			name: "no @if() attribute",
			sources: []string{`package tomei
_os: string @tag(os)
`},
			want: map[string]bool{},
		},
		{
			name:    "malformed CUE graceful skip",
			sources: []string{`this is not valid CUE {{{{`},
			want:    map[string]bool{},
		},
		{
			name: "multiple sources union",
			sources: []string{
				`@if(darwin)
package tomei
`,
				`@if(arm64)
package tomei
`,
			},
			want: map[string]bool{"darwin": true, "arm64": true},
		},
		{
			name:    "@if() with empty body",
			sources: []string{"@if()\npackage tomei\n"},
			want:    map[string]bool{},
		},
		{
			name: "@if with nested parens",
			sources: []string{`@if((darwin && arm64) || linux)
package tomei
`},
			want: map[string]bool{"darwin": true, "arm64": true, "linux": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := scanIfTags(tt.sources...)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectPackageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "simple package declaration",
			source: "package foo\n",
			want:   "foo",
		},
		{
			name:   "empty string",
			source: "",
			want:   "",
		},
		{
			name:   "comment then package",
			source: "// This is a comment\npackage tomei\n",
			want:   "tomei",
		},
		{
			name:   "no package line",
			source: "_os: string @tag(os)\n",
			want:   "",
		},
		{
			name:   "non-package first line breaks loop",
			source: "apiVersion: \"v1\"\npackage tomei\n",
			want:   "",
		},
		{
			name:   "leading blank lines then package",
			source: "\n\npackage tomei\n",
			want:   "tomei",
		},
		{
			name:   "leading blank lines and comments",
			source: "\n// comment\n\npackage myapp\n",
			want:   "myapp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := detectPackageName(tt.source)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnvTagsForSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		env     *Env
		sources []string
		want    []string
	}{
		{
			name: "source with os+arch+headless tags",
			env:  &Env{OS: "linux", Arch: "amd64", Headless: true},
			sources: []string{`package tomei
_os:       string @tag(os)
_arch:     string @tag(arch)
_headless: bool   @tag(headless,type=bool)
`},
			want: []string{"os=linux", "arch=amd64", "headless=true"},
		},
		{
			name: "source with os only",
			env:  &Env{OS: "darwin", Arch: "arm64", Headless: false},
			sources: []string{`package tomei
_os: string @tag(os)
`},
			want: []string{"os=darwin"},
		},
		{
			name: "no tags results in empty slice",
			env:  &Env{OS: "linux", Arch: "amd64", Headless: false},
			sources: []string{`package tomei
tool: { apiVersion: "v1" }
`},
			want: nil,
		},
		{
			name: "multiple sources union tags",
			env:  &Env{OS: "linux", Arch: "arm64", Headless: false},
			sources: []string{
				`package tomei
_os: string @tag(os)
`,
				`package tomei
_arch: string @tag(arch)
`,
			},
			want: []string{"os=linux", "arch=arm64"},
		},
		{
			name: "headless false value",
			env:  &Env{OS: "linux", Arch: "amd64", Headless: false},
			sources: []string{`package tomei
_headless: bool @tag(headless,type=bool)
`},
			want: []string{"headless=false"},
		},
		{
			name: "all tags darwin arm64 headless",
			env:  &Env{OS: "darwin", Arch: "arm64", Headless: true},
			sources: []string{`package tomei
_os:       string @tag(os)
_arch:     string @tag(arch)
_headless: bool   @tag(headless,type=bool)
`},
			want: []string{"os=darwin", "arch=arm64", "headless=true"},
		},
		{
			name: "@if(darwin) with env.OS=darwin adds boolean tag",
			env:  &Env{OS: "darwin", Arch: "arm64", Headless: false},
			sources: []string{`@if(darwin)
package tomei
tool: { apiVersion: "v1" }
`},
			want: []string{"darwin"},
		},
		{
			name: "@if(darwin) with env.OS=linux no boolean tag",
			env:  &Env{OS: "linux", Arch: "amd64", Headless: false},
			sources: []string{`@if(darwin)
package tomei
tool: { apiVersion: "v1" }
`},
			want: nil,
		},
		{
			name: "@if(darwin && arm64) with matching env",
			env:  &Env{OS: "darwin", Arch: "arm64", Headless: false},
			sources: []string{`@if(darwin && arm64)
package tomei
tool: { apiVersion: "v1" }
`},
			want: []string{"darwin", "arm64"},
		},
		{
			name: "@tag(os) and @if(darwin) coexist",
			env:  &Env{OS: "darwin", Arch: "arm64", Headless: false},
			sources: []string{`@if(darwin)
package tomei
_os: string @tag(os)
`},
			want: []string{"os=darwin", "darwin"},
		},
		{
			name: "@if(headless) with headless=true",
			env:  &Env{OS: "linux", Arch: "amd64", Headless: true},
			sources: []string{`@if(headless)
package tomei
`},
			want: []string{"headless"},
		},
		{
			name: "@if(headless) with headless=false no boolean tag",
			env:  &Env{OS: "linux", Arch: "amd64", Headless: false},
			sources: []string{`@if(headless)
package tomei
`},
			want: nil,
		},
		{
			name: "@if(windows) unknown platform ignored",
			env:  &Env{OS: "linux", Arch: "amd64", Headless: false},
			sources: []string{`@if(windows)
package tomei
`},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			loader := &Loader{env: tt.env}
			got := loader.envTagsForSources(tt.sources...)
			require.Equal(t, tt.want, got)
		})
	}
}
