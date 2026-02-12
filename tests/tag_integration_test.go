//go:build integration

package tests

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/resource"
)

// TestTagIntegration_PresetWithTag verifies that preset @tag() declarations
// are resolved correctly when loaded via Load().
func TestTagIntegration_PresetWithTag(t *testing.T) {
	tests := []struct {
		name        string
		env         *config.Env
		expectedURL string
	}{
		{
			name:        "linux/amd64",
			env:         &config.Env{OS: "linux", Arch: "amd64", Headless: false},
			expectedURL: "https://go.dev/dl/go1.25.6.linux-amd64.tar.gz",
		},
		{
			name:        "darwin/arm64",
			env:         &config.Env{OS: "darwin", Arch: "arm64", Headless: false},
			expectedURL: "https://go.dev/dl/go1.25.6.darwin-arm64.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			content := `package tomei

import gopreset "tomei.terassyi.net/presets/go"

goRuntime: gopreset.#GoRuntime & {
    spec: version: "1.25.6"
}
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "go.cue"), []byte(content), 0644))

			loader := config.NewLoader(tt.env)
			resources, err := loader.Load(dir)
			require.NoError(t, err)
			require.Len(t, resources, 1)

			runtime, ok := resources[0].(*resource.Runtime)
			require.True(t, ok)
			assert.Equal(t, tt.expectedURL, runtime.RuntimeSpec.Source.URL)
		})
	}
}

// TestTagIntegration_UserDefinedTag verifies that user-defined @tag() declarations
// are resolved correctly by the loader.
func TestTagIntegration_UserDefinedTag(t *testing.T) {
	dir := t.TempDir()
	content := `package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

gh: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind: "Tool"
    metadata: name: "gh"
    spec: {
        installerRef: "download"
        version: "2.86.0"
        source: {
            url: "https://example.com/gh_\(_os)_\(_arch).tar.gz"
        }
    }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644))

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "arm64", Headless: false})
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)

	tool, ok := resources[0].(*resource.Tool)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/gh_linux_arm64.tar.gz", tool.ToolSpec.Source.URL)
}

// TestTagIntegration_LoadPaths_MultiDirectory verifies that @tag() is applied
// correctly when loading from multiple directories via LoadPaths.
func TestTagIntegration_LoadPaths_MultiDirectory(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	content1 := `package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

gh: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind: "Tool"
    metadata: name: "gh"
    spec: {
        installerRef: "download"
        version: "2.86.0"
        source: {
            url: "https://example.com/gh_\(_os)_\(_arch).tar.gz"
        }
    }
}
`
	content2 := `package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

goRuntime: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind: "Runtime"
    metadata: name: "go"
    spec: {
        type: "download"
        version: "1.25.6"
        source: {
            url: "https://go.dev/dl/go1.25.6.\(_os)-\(_arch).tar.gz"
        }
        binaries: ["go", "gofmt"]
        toolBinPath: "~/go/bin"
    }
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir1, "tools.cue"), []byte(content1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "runtime.cue"), []byte(content2), 0644))

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err := loader.LoadPaths([]string{dir1, dir2})
	require.NoError(t, err)
	require.Len(t, resources, 2)

	// Check tool from dir1
	tool, ok := resources[0].(*resource.Tool)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/gh_linux_amd64.tar.gz", tool.ToolSpec.Source.URL)

	// Check runtime from dir2
	runtime, ok := resources[1].(*resource.Runtime)
	require.True(t, ok)
	assert.Equal(t, "https://go.dev/dl/go1.25.6.linux-amd64.tar.gz", runtime.RuntimeSpec.Source.URL)
}
