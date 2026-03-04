package cuemod

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/modregistrytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/config"
)

func TestParseModuleFile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		setup     func(t *testing.T, dir string)
		wantErr   bool
		errSubstr string
	}{
		{
			name: "valid module file",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				cueModDir := filepath.Join(dir, "cue.mod")
				require.NoError(t, os.MkdirAll(cueModDir, 0755))
				data := []byte(`module: "manifests.local@v0"
language: version: "v0.9.0"
deps: {
	"tomei.terassyi.net@v0": v: "v0.0.1"
}
`)
				require.NoError(t, os.WriteFile(filepath.Join(cueModDir, "module.cue"), data, 0644))
			},
		},
		{
			name:      "file not found",
			setup:     func(_ *testing.T, _ string) {},
			wantErr:   true,
			errSubstr: "module.cue not found",
		},
		{
			name: "invalid CUE syntax",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				cueModDir := filepath.Join(dir, "cue.mod")
				require.NoError(t, os.MkdirAll(cueModDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(cueModDir, "module.cue"), []byte("invalid {{{"), 0644))
			},
			wantErr:   true,
			errSubstr: "failed to parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tt.setup(t, dir)

			cueModDir := filepath.Join(dir, "cue.mod")
			f, err := ParseModuleFile(cueModDir)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, f)
			assert.Equal(t, "manifests.local@v0", f.Module)
		})
	}
}

func TestUpdateDeps(t *testing.T) {
	t.Parallel()

	parseTestModule := func(t *testing.T, version string) *modfile.File {
		t.Helper()
		data := []byte(`module: "manifests.local@v0"
language: version: "v0.9.0"
deps: {
	"tomei.terassyi.net@v0": v: "` + version + `"
}
`)
		f, err := modfile.Parse(data, "module.cue")
		require.NoError(t, err)
		return f
	}

	tests := []struct {
		name          string
		file          func(t *testing.T) *modfile.File
		latestVersion string
		wantResults   []UpdateResult
		wantErr       bool
		errSubstr     string
	}{
		{
			name: "version updated",
			file: func(t *testing.T) *modfile.File {
				return parseTestModule(t, "v0.0.1")
			},
			latestVersion: "v0.0.3",
			wantResults: []UpdateResult{
				{
					ModulePath: "tomei.terassyi.net@v0",
					OldVersion: "v0.0.1",
					NewVersion: "v0.0.3",
					Updated:    true,
				},
			},
		},
		{
			name: "already at latest",
			file: func(t *testing.T) *modfile.File {
				return parseTestModule(t, "v0.0.3")
			},
			latestVersion: "v0.0.3",
			wantResults: []UpdateResult{
				{
					ModulePath: "tomei.terassyi.net@v0",
					OldVersion: "v0.0.3",
					NewVersion: "v0.0.3",
					Updated:    false,
				},
			},
		},
		{
			name: "no first-party deps",
			file: func(t *testing.T) *modfile.File {
				t.Helper()
				data := []byte(`module: "manifests.local@v0"
language: version: "v0.9.0"
`)
				f, err := modfile.Parse(data, "module.cue")
				require.NoError(t, err)
				return f
			},
			latestVersion: "v0.0.3",
			wantErr:       true,
			errSubstr:     "no first-party",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := tt.file(t)
			results, err := UpdateDeps(f, tt.latestVersion)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSubstr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantResults, results)
		})
	}
}

func TestFormatModuleFile(t *testing.T) {
	t.Parallel()

	// Parse → update → format round trip
	data := []byte(`module: "manifests.local@v0"
language: version: "v0.9.0"
deps: {
	"tomei.terassyi.net@v0": v: "v0.0.1"
}
`)
	f, err := modfile.Parse(data, "module.cue")
	require.NoError(t, err)

	// Update the dep
	f.Deps["tomei.terassyi.net@v0"].Version = "v0.0.5"

	out, err := FormatModuleFile(f)
	require.NoError(t, err)
	assert.Contains(t, string(out), `"v0.0.5"`)
	assert.NotContains(t, string(out), `"v0.0.1"`)
}

func TestWriteModuleFileAtomic(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
	}{
		{
			name: "new file",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0755))
			},
		},
		{
			name: "overwrite existing",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				cueModDir := filepath.Join(dir, "cue.mod")
				require.NoError(t, os.MkdirAll(cueModDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(cueModDir, "module.cue"), []byte("old"), 0644))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tt.setup(t, dir)

			cueModDir := filepath.Join(dir, "cue.mod")
			content := []byte("new content")
			err := WriteModuleFileAtomic(cueModDir, content)
			require.NoError(t, err)

			got, err := os.ReadFile(filepath.Join(cueModDir, "module.cue"))
			require.NoError(t, err)
			assert.Equal(t, content, got)
		})
	}
}

func TestHasVendoredModules(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string)
		want  bool
	}{
		{
			name: "vendor exists",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				vendorDir := filepath.Join(dir, "cue.mod", "pkg", "tomei.terassyi.net")
				require.NoError(t, os.MkdirAll(vendorDir, 0755))
			},
			want: true,
		},
		{
			name:  "no vendor",
			setup: func(_ *testing.T, _ string) {},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tt.setup(t, dir)

			got := HasVendoredModules(dir)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUpdateIntegration(t *testing.T) {
	t.Run("full update flow", func(t *testing.T) {
		reg, err := modregistrytest.New(mergeMockModuleFS("v0.0.1", "v0.0.2", "v0.0.3"), "")
		require.NoError(t, err)
		defer reg.Close()

		t.Setenv(config.EnvCUERegistry, reg.Host()+"+insecure")

		// Set up a module.cue with v0.0.1
		dir := t.TempDir()
		cueModDir := filepath.Join(dir, "cue.mod")
		require.NoError(t, os.MkdirAll(cueModDir, 0755))

		initial := []byte(`module: "manifests.local@v0"
language: version: "v0.9.0"
deps: {
	"tomei.terassyi.net@v0": v: "v0.0.1"
}
`)
		require.NoError(t, os.WriteFile(filepath.Join(cueModDir, "module.cue"), initial, 0644))

		// Parse
		f, err := ParseModuleFile(cueModDir)
		require.NoError(t, err)

		// Resolve latest
		latestVersion, err := ResolveLatestVersion(context.Background())
		require.NoError(t, err)
		assert.Equal(t, "v0.0.3", latestVersion)

		// Update deps
		results, err := UpdateDeps(f, latestVersion)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.True(t, results[0].Updated)
		assert.Equal(t, "v0.0.1", results[0].OldVersion)
		assert.Equal(t, "v0.0.3", results[0].NewVersion)

		// Format and write
		data, err := FormatModuleFile(f)
		require.NoError(t, err)
		err = WriteModuleFileAtomic(cueModDir, data)
		require.NoError(t, err)

		// Verify the written file
		written, err := os.ReadFile(filepath.Join(cueModDir, "module.cue"))
		require.NoError(t, err)
		assert.Contains(t, string(written), `"v0.0.3"`)
		assert.NotContains(t, string(written), `"v0.0.1"`)
	})
}
