//go:build integration

package tests

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"cuelang.org/go/mod/modregistrytest"

	"github.com/terassyi/tomei/cuemodule"
	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/cuemod"
	"github.com/terassyi/tomei/internal/resource"
)

// TestCueEcosystem_CueInitOutput_LoadableByLoader verifies that the files generated
// by tomei cue init (cue.mod/module.cue + tomei_platform.cue) can be loaded by the
// Loader together with a user manifest. This tests the full initialization flow.
func TestCueEcosystem_CueInitOutput_LoadableByLoader(t *testing.T) {
	dir := t.TempDir()

	// Simulate "tomei cue init" output
	moduleCue, err := cuemod.GenerateModuleCUE("example.com@v0")
	require.NoError(t, err)
	moduleCuePath := filepath.Join(dir, "cue.mod", "module.cue")
	require.NoError(t, cuemod.WriteFileIfAllowed(moduleCuePath, moduleCue, false))

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	// User manifest that uses the platform tags (without imports)
	toolCue := `package tomei

myTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gh"
	spec: {
		installerRef: "download"
		version:      "2.86.0"
		source: url: "https://example.com/gh_\(_os)_\(_arch).tar.gz"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(toolCue), 0644))

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)

	tool, ok := resources[0].(*resource.Tool)
	require.True(t, ok)
	assert.Equal(t, "https://example.com/gh_linux_amd64.tar.gz", tool.ToolSpec.Source.URL)
}

// TestCueEcosystem_CueInitOutput_HeadlessTag verifies that the _headless tag
// from tomei_platform.cue is resolved correctly in the cue.mod/ path.
func TestCueEcosystem_CueInitOutput_HeadlessTag(t *testing.T) {
	dir := t.TempDir()

	// Simulate "tomei cue init" output
	moduleCue, err := cuemod.GenerateModuleCUE("example.com@v0")
	require.NoError(t, err)
	moduleCuePath := filepath.Join(dir, "cue.mod", "module.cue")
	require.NoError(t, cuemod.WriteFileIfAllowed(moduleCuePath, moduleCue, false))

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	// Manifest using _headless for conditional URL
	toolCue := `package tomei

myTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gh"
	spec: {
		installerRef: "download"
		version:      "2.86.0"
		source: url: "https://example.com/gh_\(_os)_\(_arch).tar.gz"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(toolCue), 0644))

	tests := []struct {
		name     string
		headless bool
	}{
		{name: "headless=false", headless: false},
		{name: "headless=true", headless: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64", Headless: tt.headless})
			resources, err := loader.Load(dir)
			require.NoError(t, err)
			require.Len(t, resources, 1)
			assert.Equal(t, "gh", resources[0].Name())
		})
	}
}

// --- Mock OCI Registry Tests (modregistrytest) ---

// buildModuleFS creates an fstest.MapFS containing the tomei module
// (schema + presets) for use with modregistrytest.New().
func buildModuleFS(t *testing.T) fstest.MapFS {
	t.Helper()

	// modregistrytest directory convention: {module_base}_{version}/
	// For "tomei.terassyi.net@v0" version "v0.0.1":
	const prefix = "tomei.terassyi.net_v0.0.1/"

	fs := fstest.MapFS{
		prefix + "cue.mod/module.cue": &fstest.MapFile{
			Data: []byte("module: \"tomei.terassyi.net@v0\"\nlanguage: version: \"v0.9.0\"\n"),
		},
		prefix + "schema/schema.cue": &fstest.MapFile{
			Data: []byte(cuemodule.SchemaCUE),
		},
	}

	// Add presets from embedded FS
	const presetsDir = "presets"
	presetsEntries, err := cuemodule.PresetsFS.ReadDir(presetsDir)
	require.NoError(t, err)
	for _, entry := range presetsEntries {
		if !entry.IsDir() {
			continue
		}
		subEntries, err := cuemodule.PresetsFS.ReadDir(filepath.Join(presetsDir, entry.Name()))
		require.NoError(t, err)
		for _, sub := range subEntries {
			if sub.IsDir() || filepath.Ext(sub.Name()) != ".cue" {
				continue
			}
			data, err := cuemodule.PresetsFS.ReadFile(filepath.Join(presetsDir, entry.Name(), sub.Name()))
			require.NoError(t, err)
			fs[prefix+presetsDir+"/"+entry.Name()+"/"+sub.Name()] = &fstest.MapFile{Data: data}
		}
	}

	return fs
}

// startMockRegistry starts an in-memory OCI registry containing the tomei module
// (presets + schema) using modregistrytest. Returns the registry for use with Host().
func startMockRegistry(t *testing.T) *modregistrytest.Registry {
	t.Helper()

	reg, err := modregistrytest.New(buildModuleFS(t), "")
	require.NoError(t, err)
	t.Cleanup(func() { reg.Close() })

	return reg
}

// setupMockRegistryDir creates a temporary directory with cue.mod/module.cue declaring
// a dependency on tomei.terassyi.net@v0 and sets CUE_REGISTRY to point to the mock registry.
func setupMockRegistryDir(t *testing.T, reg *modregistrytest.Registry) string {
	t.Helper()

	dir := t.TempDir()
	moduleCue, err := cuemod.GenerateModuleCUE("example.com@v0")
	require.NoError(t, err)
	moduleCuePath := filepath.Join(dir, "cue.mod", "module.cue")
	require.NoError(t, cuemod.WriteFileIfAllowed(moduleCuePath, moduleCue, false))
	t.Setenv("CUE_REGISTRY", fmt.Sprintf("tomei.terassyi.net=%s+insecure", reg.Host()))

	return dir
}

// TestCueEcosystem_MockRegistry_PresetImportResolution verifies that when a user
// has cue.mod/ and uses import "tomei.terassyi.net/presets/go", the loader resolves
// the import via a mock OCI registry and correctly loads the Go preset.
func TestCueEcosystem_MockRegistry_PresetImportResolution(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	// User manifest with import
	manifest := `package tomei

import gopreset "tomei.terassyi.net/presets/go"

goRuntime: gopreset.#GoRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "1.25.6"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)

	runtime, ok := resources[0].(*resource.Runtime)
	require.True(t, ok)
	assert.Equal(t, "go", runtime.Name())
	assert.Equal(t, "1.25.6", runtime.RuntimeSpec.Version)
	assert.Equal(t, "https://go.dev/dl/go1.25.6.linux-amd64.tar.gz", runtime.RuntimeSpec.Source.URL)
}

// TestCueEcosystem_MockRegistry_SchemaImportResolution verifies that
// import "tomei.terassyi.net/schema" resolves via the mock OCI registry.
func TestCueEcosystem_MockRegistry_SchemaImportResolution(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	// Manifest using schema import
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
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, "jq", resources[0].Name())
}

// TestCueEcosystem_MockRegistry_PresetAndSchemaImport verifies that both preset
// and schema imports work together via the mock OCI registry.
func TestCueEcosystem_MockRegistry_PresetAndSchemaImport(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	// Manifest using both imports
	manifest := `package tomei

import (
	"tomei.terassyi.net/schema"
	gopreset "tomei.terassyi.net/presets/go"
)

myTool: schema.#Tool & {
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      "1.7.1"
		package:      "jqlang/jq"
	}
}

goRuntime: gopreset.#GoRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "1.25.6"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(&config.Env{OS: "darwin", Arch: "arm64", Headless: false})
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 2)

	// Find resources by name (order may vary)
	var tool *resource.Tool
	var runtime *resource.Runtime
	for _, r := range resources {
		switch v := r.(type) {
		case *resource.Tool:
			tool = v
		case *resource.Runtime:
			runtime = v
		}
	}

	require.NotNil(t, tool)
	assert.Equal(t, "jq", tool.Name())

	require.NotNil(t, runtime)
	assert.Equal(t, "go", runtime.Name())
	assert.Equal(t, "https://go.dev/dl/go1.25.6.darwin-arm64.tar.gz", runtime.RuntimeSpec.Source.URL)
}

// TestCueEcosystem_MockRegistry_PlatformVariations verifies that the platform
// parameter is correctly resolved for different os/arch combinations via the
// mock OCI registry.
func TestCueEcosystem_MockRegistry_PlatformVariations(t *testing.T) {
	reg := startMockRegistry(t)

	tests := []struct {
		name        string
		os          string
		arch        string
		expectedURL string
	}{
		{
			name:        "linux/amd64",
			os:          "linux",
			arch:        "amd64",
			expectedURL: "https://go.dev/dl/go1.25.6.linux-amd64.tar.gz",
		},
		{
			name:        "linux/arm64",
			os:          "linux",
			arch:        "arm64",
			expectedURL: "https://go.dev/dl/go1.25.6.linux-arm64.tar.gz",
		},
		{
			name:        "darwin/arm64",
			os:          "darwin",
			arch:        "arm64",
			expectedURL: "https://go.dev/dl/go1.25.6.darwin-arm64.tar.gz",
		},
		{
			name:        "darwin/amd64",
			os:          "darwin",
			arch:        "amd64",
			expectedURL: "https://go.dev/dl/go1.25.6.darwin-amd64.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
			require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

			loader := config.NewLoader(&config.Env{OS: config.OS(tt.os), Arch: config.Arch(tt.arch), Headless: false})
			resources, err := loader.Load(dir)
			require.NoError(t, err)
			require.Len(t, resources, 1)

			runtime, ok := resources[0].(*resource.Runtime)
			require.True(t, ok)
			assert.Equal(t, tt.expectedURL, runtime.RuntimeSpec.Source.URL)
		})
	}
}

// TestCueEcosystem_MockRegistry_AquaPresetImport verifies that the aqua preset
// can be imported via the mock OCI registry and #AquaToolSet resolves correctly.
func TestCueEcosystem_MockRegistry_AquaPresetImport(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/presets/aqua"

cliTools: aqua.#AquaToolSet & {
	metadata: name: "cli-tools"
	spec: tools: {
		rg: {package: "BurntSushi/ripgrep", version: "15.1.0"}
		fd: {package: "sharkdp/fd", version: "v10.3.0"}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)

	toolset, ok := resources[0].(*resource.ToolSet)
	require.True(t, ok)
	assert.Equal(t, "cli-tools", toolset.Name())
	assert.Equal(t, resource.KindToolSet, toolset.Kind())
	assert.Equal(t, "aqua", toolset.ToolSetSpec.InstallerRef)
	assert.Len(t, toolset.ToolSetSpec.Tools, 2)
}

// TestCueEcosystem_MockRegistry_RustPresetImport verifies that the rust preset
// can be imported via the mock OCI registry and #RustRuntime, #CargoBinstall,
// #BinstallInstaller all resolve correctly.
func TestCueEcosystem_MockRegistry_RustPresetImport(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/presets/rust"

rustRuntime:       rust.#RustRuntime
cargoBinstall:     rust.#CargoBinstall
binstallInstaller: rust.#BinstallInstaller
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 3)

	// Verify resource types
	kinds := make(map[resource.Kind][]string)
	for _, r := range resources {
		kinds[r.Kind()] = append(kinds[r.Kind()], r.Name())
	}
	assert.Contains(t, kinds[resource.KindRuntime], "rust")
	assert.Contains(t, kinds[resource.KindTool], "cargo-binstall")
	assert.Contains(t, kinds[resource.KindInstaller], "binstall")
}

// TestCueEcosystem_MockRegistry_MultiplePresetImports verifies that go + aqua
// presets can be imported together via the mock OCI registry.
func TestCueEcosystem_MockRegistry_MultiplePresetImports(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	manifest := `package tomei

import (
	gopreset "tomei.terassyi.net/presets/go"
	"tomei.terassyi.net/presets/aqua"
)

goRuntime: gopreset.#GoRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "1.25.6"
}

cliTools: aqua.#AquaToolSet & {
	metadata: name: "cli-tools"
	spec: tools: {
		rg: {package: "BurntSushi/ripgrep", version: "15.1.0"}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 2)

	kinds := make(map[resource.Kind]bool)
	for _, r := range resources {
		kinds[r.Kind()] = true
	}
	assert.True(t, kinds[resource.KindRuntime])
	assert.True(t, kinds[resource.KindToolSet])
}

// TestCueEcosystem_MockRegistry_InvalidSchema verifies that schema.#Tool with
// wrong apiVersion produces an error via the mock OCI registry.
func TestCueEcosystem_MockRegistry_InvalidSchema(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/schema"

badTool: schema.#Tool & {
	apiVersion: "wrong/v1"
	metadata: name: "test"
	spec: {
		installerRef: "download"
		version:      "1.0.0"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(nil)
	_, err := loader.Load(dir)
	require.Error(t, err)
}

// TestCueEcosystem_MockRegistry_CueInitThenLoad simulates the full cue init → write
// manifest → load via mock registry workflow.
func TestCueEcosystem_MockRegistry_CueInitThenLoad(t *testing.T) {
	reg := startMockRegistry(t)

	dir := t.TempDir()
	t.Setenv("CUE_REGISTRY", fmt.Sprintf("tomei.terassyi.net=%s+insecure", reg.Host()))

	// Simulate cue init output
	moduleCue, err := cuemod.GenerateModuleCUE(cuemod.DefaultModuleName)
	require.NoError(t, err)
	moduleCuePath := filepath.Join(dir, "cue.mod", "module.cue")
	require.NoError(t, cuemod.WriteFileIfAllowed(moduleCuePath, moduleCue, false))

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	// User writes a manifest using schema + preset imports
	manifest := `package tomei

import (
	"tomei.terassyi.net/schema"
	gopreset "tomei.terassyi.net/presets/go"
)

myTool: schema.#Tool & {
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      "1.7.1"
		package:      "jqlang/jq"
	}
}

goRuntime: gopreset.#GoRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "1.25.6"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "arm64", Headless: false})
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 2)

	// Verify resources
	var tool *resource.Tool
	var runtime *resource.Runtime
	for _, r := range resources {
		switch v := r.(type) {
		case *resource.Tool:
			tool = v
		case *resource.Runtime:
			runtime = v
		}
	}

	require.NotNil(t, tool)
	assert.Equal(t, "jq", tool.Name())
	assert.Equal(t, "1.7.1", tool.ToolSpec.Version)

	require.NotNil(t, runtime)
	assert.Equal(t, "go", runtime.Name())
	assert.Equal(t, "https://go.dev/dl/go1.25.6.linux-arm64.tar.gz", runtime.RuntimeSpec.Source.URL)
}
