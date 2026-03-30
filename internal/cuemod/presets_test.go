package cuemod

import (
	"archive/zip"
	"bytes"
	"context"
	"maps"
	"testing"
	"testing/fstest"

	"cuelang.org/go/mod/modregistrytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/config"
)

// presetExtraFiles returns extra CUE preset files for mock module FS.
var presetExtraFiles = fstest.MapFS{
	"presets/go/go.cue": &fstest.MapFile{
		Data: []byte("package gopreset\n\n#GoRuntime: {\n\tkind: \"Runtime\"\n}\n\n#GoTool: {\n\tkind: \"Tool\"\n}\n\n#GoToolSet: {\n\tkind: \"Tool\"\n}\n"),
	},
	"presets/aqua/aqua.cue": &fstest.MapFile{
		Data: []byte("package aqua\n\n#AquaTool: {\n\tkind: \"Tool\"\n}\n\n#AquaToolSet: {\n\tkind: \"Tool\"\n}\n"),
	},
}

func setupMockRegistryWithPresets(t *testing.T, versions ...string) {
	t.Helper()
	merged := fstest.MapFS{}
	for _, v := range versions {
		maps.Copy(merged, buildMockModuleFS(v, presetExtraFiles))
	}
	reg, err := modregistrytest.New(merged, "")
	require.NoError(t, err)
	t.Cleanup(reg.Close)
	t.Setenv(config.EnvCUERegistry, reg.Host()+"+insecure")
}

func TestFetchPresets(t *testing.T) {
	tests := []struct {
		name        string
		version     string
		wantPresets []string
		wantVersion string
	}{
		{
			name:        "fetches presets with latest version",
			version:     "",
			wantPresets: []string{"aqua", "go"},
			wantVersion: "v0.0.2",
		},
		{
			name:        "fetches presets with explicit version",
			version:     "v0.0.1",
			wantPresets: []string{"aqua", "go"},
			wantVersion: "v0.0.1",
		},
	}

	for _, tt := range tests {
		// Subtests use t.Setenv via setupMockRegistryWithPresets, so they cannot be parallel.
		t.Run(tt.name, func(t *testing.T) {
			setupMockRegistryWithPresets(t, "v0.0.1", "v0.0.2")

			presets, version, err := FetchPresets(context.Background(), tt.version)
			require.NoError(t, err)
			assert.Equal(t, tt.wantVersion, version)

			var names []string
			for _, p := range presets {
				names = append(names, p.Name)
			}
			assert.Equal(t, tt.wantPresets, names)

			for _, p := range presets {
				assert.NotEmpty(t, p.Definitions, "definitions for %s", p.Name)
				assert.NotEmpty(t, p.Source, "source for %s", p.Name)
				assert.Contains(t, p.ImportPath, "presets/"+p.Name)
			}
		})
	}
}

func TestFetchPresets_RegistryNone(t *testing.T) {
	t.Setenv(config.EnvCUERegistry, "none")
	_, _, err := FetchPresets(context.Background(), "")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrRegistryDisabled)
}

func TestExtractPresetsFromZip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		files      map[string]string
		wantNames  []string
		wantDefs   map[string][]string
		wantSource bool
	}{
		{
			name: "extracts presets from bare-path zip",
			files: map[string]string{
				"presets/go/go.cue":     "package gopreset\n#GoRuntime: {}\n#GoTool: {}\n",
				"presets/aqua/aqua.cue": "package aqua\n#AquaTool: {}\n",
				"schema/schema.cue":     "package schema\n",
			},
			wantNames: []string{"aqua", "go"},
			wantDefs: map[string][]string{
				"aqua": {"#AquaTool"},
				"go":   {"#GoRuntime", "#GoTool"},
			},
			wantSource: true,
		},
		{
			name: "empty presets directory",
			files: map[string]string{
				"schema/schema.cue": "package schema\n",
			},
			wantNames: nil,
		},
		{
			name: "ignores non-cue files",
			files: map[string]string{
				"presets/go/go.cue":    "package gopreset\n#GoRuntime: {}\n",
				"presets/go/README.md": "# Go preset\n",
			},
			wantNames: []string{"go"},
			wantDefs: map[string][]string{
				"go": {"#GoRuntime"},
			},
		},
		{
			name: "ignores deeply nested files",
			files: map[string]string{
				"presets/go/sub/nested.cue": "package sub\n#Nested: {}\n",
			},
			wantNames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			zipData := createTestZip(t, tt.files)

			presets, err := extractPresetsFromZip(
				bytes.NewReader(zipData),
				int64(len(zipData)),
			)
			require.NoError(t, err)

			var names []string
			for _, p := range presets {
				names = append(names, p.Name)
			}
			assert.Equal(t, tt.wantNames, names)

			if tt.wantDefs != nil {
				for _, p := range presets {
					assert.Equal(t, tt.wantDefs[p.Name], p.Definitions, "definitions for %s", p.Name)
				}
			}

			if tt.wantSource {
				for _, p := range presets {
					assert.NotEmpty(t, p.Source, "source for %s should not be empty", p.Name)
				}
			}
		})
	}
}

// createTestZip creates a zip archive in memory from the given file map.
func createTestZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for name, content := range files {
		f, err := w.Create(name)
		require.NoError(t, err)
		_, err = f.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	return buf.Bytes()
}
