package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScanDeclaredTags(t *testing.T) {
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
			got := scanDeclaredTags(tt.sources...)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestDetectPackageName(t *testing.T) {
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
			got := detectPackageName(tt.source)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestEnvTagsForSources(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := &Loader{env: tt.env}
			got := loader.envTagsForSources(tt.sources...)
			require.Equal(t, tt.want, got)
		})
	}
}
