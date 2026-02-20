package verify

import (
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/mod/module"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractFirstPartyDeps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		moduleCUE string
		want      []module.Version
		wantErr   bool
	}{
		{
			name: "single first-party dep",
			moduleCUE: `module: "manifests.local@v0"
language: version: "v0.9.0"
deps: {
	"tomei.terassyi.net@v0": v: "v0.0.3"
}
`,
			want: []module.Version{
				module.MustNewVersion("tomei.terassyi.net@v0", "v0.0.3"),
			},
		},
		{
			name: "multiple first-party deps",
			moduleCUE: `module: "manifests.local@v0"
language: version: "v0.9.0"
deps: {
	"tomei.terassyi.net@v0": v: "v0.0.3"
	"tomei.terassyi.net/presets/go@v0": v: "v0.0.1"
}
`,
			want: []module.Version{
				module.MustNewVersion("tomei.terassyi.net/presets/go@v0", "v0.0.1"),
				module.MustNewVersion("tomei.terassyi.net@v0", "v0.0.3"),
			},
		},
		{
			name: "no first-party deps",
			moduleCUE: `module: "manifests.local@v0"
language: version: "v0.9.0"
deps: {
	"example.com@v0": v: "v0.1.0"
}
`,
			want: nil,
		},
		{
			name: "no deps at all",
			moduleCUE: `module: "manifests.local@v0"
language: version: "v0.9.0"
`,
			want: nil,
		},
		{
			name: "mixed first-party and third-party",
			moduleCUE: `module: "manifests.local@v0"
language: version: "v0.9.0"
deps: {
	"tomei.terassyi.net@v0": v: "v0.0.3"
	"example.com@v0": v: "v0.1.0"
}
`,
			want: []module.Version{
				module.MustNewVersion("tomei.terassyi.net@v0", "v0.0.3"),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cueModDir := filepath.Join(dir, "cue.mod")
			require.NoError(t, os.MkdirAll(cueModDir, 0755))
			require.NoError(t, os.WriteFile(
				filepath.Join(cueModDir, "module.cue"),
				[]byte(tt.moduleCUE),
				0644,
			))

			got, err := ExtractFirstPartyDeps(cueModDir)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestExtractFirstPartyDeps_NoCueModDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	nonexistent := filepath.Join(dir, "cue.mod")

	got, err := ExtractFirstPartyDeps(nonexistent)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestExtractFirstPartyDeps_InvalidModuleCUE(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cueModDir := filepath.Join(dir, "cue.mod")
	require.NoError(t, os.MkdirAll(cueModDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(cueModDir, "module.cue"),
		[]byte("this is not valid CUE { [ }"),
		0644,
	))

	_, err := ExtractFirstPartyDeps(cueModDir)
	require.Error(t, err)
}
