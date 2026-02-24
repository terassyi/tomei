//go:build integration

package tests

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/mod/modregistrytest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/cuemod"
)

func TestEval_MockRegistry_PresetImport(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	manifest := `package tomei

import gopreset "tomei.terassyi.net/presets/go"

goRuntime: gopreset.#GoRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "1.25.6"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "runtime.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64"})
	value, err := loader.EvalDir(dir)
	require.NoError(t, err)

	// @tag() should be resolved
	url := value.LookupPath(cue.ParsePath("goRuntime.spec.source.url"))
	s, err := url.String()
	require.NoError(t, err)
	assert.Contains(t, s, "linux-amd64")

	// JSON export should work
	jsonBytes, err := value.MarshalJSON()
	require.NoError(t, err)
	assert.Contains(t, string(jsonBytes), `"tomei.terassyi.net/v1beta1"`)
}

func TestEval_MockRegistry_SchemaImport(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/schema"

myTool: schema.#Tool & {
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      "1.7.1"
		package:      "jqlang/jq"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(nil)
	value, err := loader.EvalDir(dir)
	require.NoError(t, err)

	name := value.LookupPath(cue.ParsePath("myTool.metadata.name"))
	s, err := name.String()
	require.NoError(t, err)
	assert.Equal(t, "jq", s)
}

func TestEval_ResolvesTagValues(t *testing.T) {
	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	content := `package tomei

myRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: name: "go"
	spec: {
		type:        "download"
		version:     "1.25.6"
		toolBinPath: "~/go/bin"
		source: url: "https://go.dev/dl/go1.25.6.\(_os)-\(_arch).tar.gz"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "runtime.cue"), []byte(content), 0644))

	tests := []struct {
		name    string
		os      config.OS
		arch    config.Arch
		wantURL string
	}{
		{
			name:    "linux/amd64",
			os:      "linux",
			arch:    "amd64",
			wantURL: "linux-amd64",
		},
		{
			name:    "darwin/arm64",
			os:      "darwin",
			arch:    "arm64",
			wantURL: "darwin-arm64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := config.NewLoader(&config.Env{OS: tt.os, Arch: tt.arch})
			value, err := loader.EvalDir(dir)
			require.NoError(t, err)

			url := value.LookupPath(cue.ParsePath("myRuntime.spec.source.url"))
			s, err := url.String()
			require.NoError(t, err)
			assert.Contains(t, s, tt.wantURL)
		})
	}
}

func TestEval_JSONExportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	content := `package tomei

myTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      "1.7.1"
		package:      "jqlang/jq"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644))

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64"})
	value, err := loader.EvalDir(dir)
	require.NoError(t, err)

	// Export as JSON
	jsonBytes, err := value.MarshalJSON()
	require.NoError(t, err)

	// Parse and verify structure with typed struct
	var parsed struct {
		MyTool struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Spec       struct {
				InstallerRef string `json:"installerRef"`
				Version      string `json:"version"`
				Package      string `json:"package"`
			} `json:"spec"`
		} `json:"myTool"`
	}
	require.NoError(t, json.Unmarshal(jsonBytes, &parsed))
	assert.Equal(t, "tomei.terassyi.net/v1beta1", parsed.MyTool.APIVersion)
	assert.Equal(t, "Tool", parsed.MyTool.Kind)
	assert.Equal(t, "aqua", parsed.MyTool.Spec.InstallerRef)
	assert.Equal(t, "1.7.1", parsed.MyTool.Spec.Version)
	assert.Equal(t, "jqlang/jq", parsed.MyTool.Spec.Package)
}

func TestEvalPaths_MultipleDirectories(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	setupMinimalCueMod(t, dir1)
	setupMinimalCueMod(t, dir2)

	tool1 := `package tomei

rg: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "aqua"
		version:      "15.1.0"
	}
}
`
	tool2 := `package tomei

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version:      "v10.3.0"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir1, "tools.cue"), []byte(tool1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "tools.cue"), []byte(tool2), 0644))

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64"})
	values, err := loader.EvalPaths([]string{dir1, dir2})
	require.NoError(t, err)
	require.Len(t, values, 2)

	// First value should contain rg
	name1 := values[0].LookupPath(cue.ParsePath("rg.metadata.name"))
	s1, err := name1.String()
	require.NoError(t, err)
	assert.Equal(t, "rg", s1)

	// Second value should contain fd
	name2 := values[1].LookupPath(cue.ParsePath("fd.metadata.name"))
	s2, err := name2.String()
	require.NoError(t, err)
	assert.Equal(t, "fd", s2)
}

func TestEvalFile_SingleFile(t *testing.T) {
	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	content := `package tomei

myTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "aqua"
		version:      "0.24.0"
	}
}
`
	path := filepath.Join(dir, "tools.cue")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64"})
	value, err := loader.EvalFile(path)
	require.NoError(t, err)
	require.True(t, value.Exists())

	name := value.LookupPath(cue.ParsePath("myTool.metadata.name"))
	s, err := name.String()
	require.NoError(t, err)
	assert.Equal(t, "bat", s)
}

func TestEval_MockRegistry_MultipleVersions(t *testing.T) {
	// Test that eval works with the mock registry having multiple versions
	reg, err := modregistrytest.New(mergeModuleFS(t, "v0.0.1", cuemod.DefaultModuleVer), "")
	require.NoError(t, err)
	t.Cleanup(func() { reg.Close() })

	dir := t.TempDir()
	moduleCue, err := cuemod.GenerateModuleCUE("example.com@v0", cuemod.DefaultModuleVer)
	require.NoError(t, err)
	moduleCuePath := filepath.Join(dir, "cue.mod", "module.cue")
	require.NoError(t, cuemod.WriteFileIfAllowed(moduleCuePath, moduleCue, false))
	t.Setenv("CUE_REGISTRY", fmt.Sprintf("tomei.terassyi.net=%s+insecure", reg.Host()))

	// Isolate CUE module cache per test to avoid stale cache from previous runs.
	cueCache := filepath.Join(dir, "cue-cache")
	require.NoError(t, os.MkdirAll(cueCache, 0755))
	t.Setenv("CUE_CACHE_DIR", cueCache)
	t.Cleanup(func() {
		_ = filepath.Walk(cueCache, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			return os.Chmod(path, 0755)
		})
	})

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	manifest := `package tomei

import gopreset "tomei.terassyi.net/presets/go"

goRuntime: gopreset.#GoRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "1.25.6"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "runtime.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(&config.Env{OS: "darwin", Arch: "arm64"})
	value, err := loader.EvalDir(dir)
	require.NoError(t, err)

	url := value.LookupPath(cue.ParsePath("goRuntime.spec.source.url"))
	s, err := url.String()
	require.NoError(t, err)
	assert.Contains(t, s, "darwin-arm64")
}

// TestEvalDir_CommandsPatternTool verifies that EvalDir produces a cue.Value
// containing commands.install for a commands-pattern tool.
func TestEvalDir_CommandsPatternTool(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/schema"

myTool: schema.#Tool & {
	metadata: name: "cmdtool"
	spec: {
		commands: {
			install: ["curl -fsSL https://example.com/install.sh | sh"]
			update: ["cmdtool self-update"]
			check: ["cmdtool --version"]
			remove: ["rm -f /usr/local/bin/cmdtool"]
			resolveVersion: ["cmdtool --version | head -1"]
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(nil)
	value, err := loader.EvalDir(dir)
	require.NoError(t, err)

	// Verify commands.install path exists and is correct
	install := value.LookupPath(cue.ParsePath("myTool.spec.commands.install"))
	require.True(t, install.Exists(), "commands.install path must exist in cue.Value")

	// Verify it is a list with one element
	iter, err := install.List()
	require.NoError(t, err)
	require.True(t, iter.Next())
	s, err := iter.Value().String()
	require.NoError(t, err)
	assert.Equal(t, "curl -fsSL https://example.com/install.sh | sh", s)

	// Verify other command fields
	update := value.LookupPath(cue.ParsePath("myTool.spec.commands.update"))
	require.True(t, update.Exists(), "commands.update path must exist")

	check := value.LookupPath(cue.ParsePath("myTool.spec.commands.check"))
	require.True(t, check.Exists(), "commands.check path must exist")

	remove := value.LookupPath(cue.ParsePath("myTool.spec.commands.remove"))
	require.True(t, remove.Exists(), "commands.remove path must exist")

	resolveVersion := value.LookupPath(cue.ParsePath("myTool.spec.commands.resolveVersion"))
	require.True(t, resolveVersion.Exists(), "commands.resolveVersion path must exist")

	// Verify metadata
	name := value.LookupPath(cue.ParsePath("myTool.metadata.name"))
	nameStr, err := name.String()
	require.NoError(t, err)
	assert.Equal(t, "cmdtool", nameStr)

	// JSON export should contain commands
	jsonBytes, err := value.MarshalJSON()
	require.NoError(t, err)
	assert.Contains(t, string(jsonBytes), `"commands"`)
	assert.Contains(t, string(jsonBytes), `"install"`)
}

// TestEvalFile_CommandsPatternTool verifies that EvalFile produces a cue.Value
// containing commands.install for a commands-pattern tool loaded from a single file.
func TestEvalFile_CommandsPatternTool(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/schema"

myTool: schema.#Tool & {
	metadata: name: "filetool"
	spec: {
		commands: {
			install: ["wget -O /tmp/filetool https://example.com/filetool && chmod +x /tmp/filetool && mv /tmp/filetool /usr/local/bin/"]
			check: ["filetool version"]
		}
	}
}
`
	filePath := filepath.Join(dir, "tool.cue")
	require.NoError(t, os.WriteFile(filePath, []byte(manifest), 0644))

	loader := config.NewLoader(nil)
	value, err := loader.EvalFile(filePath)
	require.NoError(t, err)
	require.True(t, value.Exists())

	// Verify commands.install path exists
	install := value.LookupPath(cue.ParsePath("myTool.spec.commands.install"))
	require.True(t, install.Exists(), "commands.install path must exist in EvalFile result")

	// Verify metadata
	name := value.LookupPath(cue.ParsePath("myTool.metadata.name"))
	nameStr, err := name.String()
	require.NoError(t, err)
	assert.Equal(t, "filetool", nameStr)

	// Verify check command
	check := value.LookupPath(cue.ParsePath("myTool.spec.commands.check"))
	require.True(t, check.Exists(), "commands.check path must exist")
}
