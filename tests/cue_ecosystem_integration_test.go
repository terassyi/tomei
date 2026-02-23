//go:build integration

package tests

import (
	"context"
	"fmt"
	"maps"
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
	moduleCue, err := cuemod.GenerateModuleCUE("example.com@v0", cuemod.DefaultModuleVer)
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
// from tomei_platform.cue is resolved correctly and produces different output
// depending on the headless value.
func TestCueEcosystem_CueInitOutput_HeadlessTag(t *testing.T) {
	dir := t.TempDir()

	// Simulate "tomei cue init" output
	moduleCue, err := cuemod.GenerateModuleCUE("example.com@v0", cuemod.DefaultModuleVer)
	require.NoError(t, err)
	moduleCuePath := filepath.Join(dir, "cue.mod", "module.cue")
	require.NoError(t, cuemod.WriteFileIfAllowed(moduleCuePath, moduleCue, false))

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	// Manifest that branches on _headless to produce different tool versions
	toolCue := `package tomei

_version: string
if _headless {
	_version: "2.86.0-headless"
}
if !_headless {
	_version: "2.86.0"
}

myTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gh"
	spec: {
		installerRef: "download"
		version:      _version
		source: url: "https://example.com/gh_\(_os)_\(_arch).tar.gz"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(toolCue), 0644))

	tests := []struct {
		name        string
		headless    bool
		wantVersion string
	}{
		{name: "headless=false", headless: false, wantVersion: "2.86.0"},
		{name: "headless=true", headless: true, wantVersion: "2.86.0-headless"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64", Headless: tt.headless})
			resources, err := loader.Load(dir)
			require.NoError(t, err)
			require.Len(t, resources, 1)
			assert.Equal(t, "gh", resources[0].Name())

			tool, ok := resources[0].(*resource.Tool)
			require.True(t, ok)
			assert.Equal(t, tt.wantVersion, tool.ToolSpec.Version,
				"_headless=%v should produce version %s", tt.headless, tt.wantVersion)
		})
	}
}

// --- Mock OCI Registry Tests (modregistrytest) ---

// buildVersionedModuleFS creates an fstest.MapFS containing the tomei module
// at the given version (schema + presets) for use with modregistrytest.New().
func buildVersionedModuleFS(t *testing.T, version string) fstest.MapFS {
	t.Helper()

	// modregistrytest directory convention: {module_base}_{version}/
	prefix := "tomei.terassyi.net_" + version + "/"

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

// mergeModuleFS merges multiple versioned module FSes into a single MapFS
// for use with modregistrytest.New().
func mergeModuleFS(t *testing.T, versions ...string) fstest.MapFS {
	t.Helper()
	merged := fstest.MapFS{}
	for _, v := range versions {
		maps.Copy(merged, buildVersionedModuleFS(t, v))
	}
	return merged
}

// startMockRegistry starts an in-memory OCI registry containing the tomei module
// (presets + schema) using modregistrytest. Returns the registry for use with Host().
func startMockRegistry(t *testing.T) *modregistrytest.Registry {
	t.Helper()

	reg, err := modregistrytest.New(buildVersionedModuleFS(t, cuemod.DefaultModuleVer), "")
	require.NoError(t, err)
	t.Cleanup(func() { reg.Close() })

	return reg
}

// setupMockRegistryDir creates a temporary directory with cue.mod/module.cue declaring
// a dependency on tomei.terassyi.net@v0 and sets CUE_REGISTRY to point to the mock registry.
// It also sets CUE_CACHE_DIR to an isolated temp directory to prevent stale module cache
// from interfering with tests (CUE caches downloaded module zips by version).
func setupMockRegistryDir(t *testing.T, reg *modregistrytest.Registry) string {
	t.Helper()

	dir := t.TempDir()
	moduleCue, err := cuemod.GenerateModuleCUE("example.com@v0", cuemod.DefaultModuleVer)
	require.NoError(t, err)
	moduleCuePath := filepath.Join(dir, "cue.mod", "module.cue")
	require.NoError(t, cuemod.WriteFileIfAllowed(moduleCuePath, moduleCue, false))
	t.Setenv("CUE_REGISTRY", fmt.Sprintf("tomei.terassyi.net=%s+insecure", reg.Host()))

	// Isolate CUE module cache per test to avoid stale cache from previous runs.
	// CUE extracts module files as read-only, so we must chmod before cleanup.
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
	assert.Equal(t, "https://go.dev/dl/go{{.Version}}.linux-amd64.tar.gz", runtime.RuntimeSpec.Source.URL)

	// Verify []string fields are correctly deserialized via cue.Value.Decode()
	require.NotNil(t, runtime.RuntimeSpec.Commands)
	assert.Equal(t, []string{"go install {{.Package}}@{{.Version}}"}, runtime.RuntimeSpec.Commands.Install)
	assert.Equal(t, []string{"rm -f {{.BinPath}}"}, runtime.RuntimeSpec.Commands.Remove)
	assert.Equal(t, []string{"go", "gofmt"}, runtime.RuntimeSpec.Binaries)
	// ResolveVersion should be present (http-text resolver)
	assert.Equal(t, []string{"http-text:https://go.dev/VERSION?m=text:^go(.+)"}, runtime.RuntimeSpec.ResolveVersion)
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
	assert.Equal(t, "https://go.dev/dl/go{{.Version}}.darwin-arm64.tar.gz", runtime.RuntimeSpec.Source.URL)
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
			expectedURL: "https://go.dev/dl/go{{.Version}}.linux-amd64.tar.gz",
		},
		{
			name:        "linux/arm64",
			os:          "linux",
			arch:        "arm64",
			expectedURL: "https://go.dev/dl/go{{.Version}}.linux-arm64.tar.gz",
		},
		{
			name:        "darwin/arm64",
			os:          "darwin",
			arch:        "arm64",
			expectedURL: "https://go.dev/dl/go{{.Version}}.darwin-arm64.tar.gz",
		},
		{
			name:        "darwin/amd64",
			os:          "darwin",
			arch:        "amd64",
			expectedURL: "https://go.dev/dl/go{{.Version}}.darwin-amd64.tar.gz",
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

	// Verify Rust runtime Bootstrap []string fields are correctly deserialized
	for _, r := range resources {
		if r.Kind() == resource.KindRuntime && r.Name() == "rust" {
			rt := r.(*resource.Runtime)
			require.NotNil(t, rt.RuntimeSpec.Bootstrap)
			assert.Len(t, rt.RuntimeSpec.Bootstrap.Install, 1, "bootstrap.install should be a single-element slice")
			assert.Len(t, rt.RuntimeSpec.Bootstrap.Check, 1, "bootstrap.check should be a single-element slice")
			assert.Len(t, rt.RuntimeSpec.Bootstrap.Remove, 1, "bootstrap.remove should be a single-element slice")
			assert.Len(t, rt.RuntimeSpec.Bootstrap.ResolveVersion, 1, "bootstrap.resolveVersion should be a single-element slice")
		}
	}
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

	// Isolate CUE module cache to avoid stale cache from previous runs
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

	// Simulate cue init output
	moduleCue, err := cuemod.GenerateModuleCUE(cuemod.DefaultModuleName, cuemod.DefaultModuleVer)
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
	assert.Equal(t, "https://go.dev/dl/go{{.Version}}.linux-arm64.tar.gz", runtime.RuntimeSpec.Source.URL)
}

// TestCueEcosystem_MockRegistry_ResolveLatestVersion verifies that
// ResolveLatestVersion returns the highest semver from a multi-version
// mock registry, regardless of insertion order.
func TestCueEcosystem_MockRegistry_ResolveLatestVersion(t *testing.T) {
	tests := []struct {
		name     string
		versions []string
		want     string
	}{
		{
			name:     "picks latest from three versions",
			versions: []string{"v0.0.1", "v0.0.2", "v0.0.3"},
			want:     "v0.0.3",
		},
		{
			name:     "handles non-sequential order",
			versions: []string{"v0.0.3", "v0.0.1", "v0.1.0", "v0.0.2"},
			want:     "v0.1.0",
		},
		{
			name:     "single version",
			versions: []string{"v0.0.1"},
			want:     "v0.0.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg, err := modregistrytest.New(mergeModuleFS(t, tt.versions...), "")
			require.NoError(t, err)
			defer reg.Close()

			t.Setenv(config.EnvCUERegistry, reg.Host()+"+insecure")

			got, err := cuemod.ResolveLatestVersion(context.Background())
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestCueEcosystem_MockRegistry_ResolveLatestVersion_UsedInGenerateModuleCUE
// verifies the end-to-end flow: resolve latest version from registry, generate
// module.cue with it, and confirm the loader can load the result.
func TestCueEcosystem_MockRegistry_ResolveLatestVersion_UsedInGenerateModuleCUE(t *testing.T) {
	reg, err := modregistrytest.New(mergeModuleFS(t, "v0.0.1", "v0.0.2"), "")
	require.NoError(t, err)
	defer reg.Close()

	t.Setenv(config.EnvCUERegistry, fmt.Sprintf("tomei.terassyi.net=%s+insecure", reg.Host()))

	// Resolve latest version
	version, err := cuemod.ResolveLatestVersion(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "v0.0.2", version)

	// Generate module.cue with resolved version
	dir := t.TempDir()
	moduleCue, err := cuemod.GenerateModuleCUE("example.com@v0", version)
	require.NoError(t, err)
	moduleCuePath := filepath.Join(dir, "cue.mod", "module.cue")
	require.NoError(t, cuemod.WriteFileIfAllowed(moduleCuePath, moduleCue, false))

	// Verify the generated module.cue contains the resolved version
	content, err := os.ReadFile(moduleCuePath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "v0.0.2")

	// Verify loader can load with this module.cue + mock registry
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

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)

	runtime, ok := resources[0].(*resource.Runtime)
	require.True(t, ok)
	assert.Equal(t, "go", runtime.Name())
}

// TestCueEcosystem_MockRegistry_PresetSchemaConstraint verifies that schema constraints
// imported by presets are enforced. Using a Go preset with an invalid apiVersion should
// fail at CUE evaluation time.
func TestCueEcosystem_MockRegistry_PresetSchemaConstraint(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	// Manifest with invalid apiVersion — should be rejected by schema.#Runtime constraint
	manifest := `package tomei

import gopreset "tomei.terassyi.net/presets/go"

goRuntime: gopreset.#GoRuntime & {
	platform: { os: _os, arch: _arch }
	apiVersion: "wrong/v1"
	spec: version: "1.25.6"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64", Headless: false})
	_, err = loader.Load(dir)
	assert.Error(t, err, "preset with schema import should reject invalid apiVersion")
}

// TestCueEcosystem_VendoredPkg_PresetResolution verifies that presets can be resolved
// from cue.mod/pkg/ (vendored) without an OCI registry (CUE_REGISTRY=none).
func TestCueEcosystem_VendoredPkg_PresetResolution(t *testing.T) {
	dir := t.TempDir()

	// Create cue.mod/module.cue WITHOUT deps (vendored mode)
	moduleCuePath := filepath.Join(dir, "cue.mod", "module.cue")
	require.NoError(t, os.MkdirAll(filepath.Dir(moduleCuePath), 0755))
	moduleCue := "module: \"manifests.local@v0\"\nlanguage: version: \"v0.9.0\"\n"
	require.NoError(t, os.WriteFile(moduleCuePath, []byte(moduleCue), 0644))

	// Vendor schema and presets into cue.mod/pkg/
	pkgBase := filepath.Join(dir, "cue.mod", "pkg", "tomei.terassyi.net")
	schemaDir := filepath.Join(pkgBase, "schema")
	goPresetDir := filepath.Join(pkgBase, "presets", "go")
	require.NoError(t, os.MkdirAll(schemaDir, 0755))
	require.NoError(t, os.MkdirAll(goPresetDir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(schemaDir, "schema.cue"), []byte(cuemodule.SchemaCUE), 0644))

	goPresetData, err := cuemodule.PresetsFS.ReadFile("presets/go/go.cue")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(goPresetDir, "go.cue"), goPresetData, 0644))

	// Platform tags
	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	// User manifest with Go preset import
	manifest := `package tomei

import gopreset "tomei.terassyi.net/presets/go"

goRuntime: gopreset.#GoRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "1.25.6"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	// Use CUE_REGISTRY=none — vendored files only
	t.Setenv("CUE_REGISTRY", "none")

	loader := config.NewLoader(&config.Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)

	runtime, ok := resources[0].(*resource.Runtime)
	require.True(t, ok)
	assert.Equal(t, "go", runtime.Name())
	assert.Equal(t, "1.25.6", runtime.RuntimeSpec.Version)
}

// TestCueEcosystem_MockRegistry_NodePresetImport verifies that the node preset
// can be imported via the mock OCI registry and #PnpmRuntime resolves correctly.
func TestCueEcosystem_MockRegistry_NodePresetImport(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/presets/node"

pnpmRuntime: node.#PnpmRuntime & {
	spec: version: "10.29.3"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)

	rt, ok := resources[0].(*resource.Runtime)
	require.True(t, ok)
	assert.Equal(t, "pnpm", rt.Name())
	assert.Equal(t, "10.29.3", rt.RuntimeSpec.Version)
	assert.Equal(t, resource.InstallTypeDelegation, rt.RuntimeSpec.Type)

	// Verify bootstrap []string fields
	require.NotNil(t, rt.RuntimeSpec.Bootstrap)
	assert.Len(t, rt.RuntimeSpec.Bootstrap.Install, 4, "bootstrap.install should have 4 steps")
	assert.Len(t, rt.RuntimeSpec.Bootstrap.Check, 1)
	assert.Len(t, rt.RuntimeSpec.Bootstrap.Remove, 1)
	assert.Len(t, rt.RuntimeSpec.Bootstrap.ResolveVersion, 1)

	// Verify binaries
	assert.Equal(t, []string{"pnpm", "pnpx"}, rt.RuntimeSpec.Binaries)

	// Verify env map
	assert.Equal(t, "~/.local/share/pnpm", rt.RuntimeSpec.Env["PNPM_HOME"])

	// Verify commands
	require.NotNil(t, rt.RuntimeSpec.Commands)
	assert.Len(t, rt.RuntimeSpec.Commands.Install, 1)
	assert.Len(t, rt.RuntimeSpec.Commands.Remove, 1)
}

// TestCueEcosystem_MockRegistry_PythonPresetImport verifies that the python preset
// can be imported via the mock OCI registry and #UvRuntime resolves correctly.
func TestCueEcosystem_MockRegistry_PythonPresetImport(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/presets/python"

uvRuntime: python.#UvRuntime & {
	spec: version: "0.10.2"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)

	rt, ok := resources[0].(*resource.Runtime)
	require.True(t, ok)
	assert.Equal(t, "uv", rt.Name())
	assert.Equal(t, "0.10.2", rt.RuntimeSpec.Version)
	assert.Equal(t, resource.InstallTypeDelegation, rt.RuntimeSpec.Type)

	// Verify bootstrap
	require.NotNil(t, rt.RuntimeSpec.Bootstrap)
	assert.Len(t, rt.RuntimeSpec.Bootstrap.Install, 1)
	assert.Contains(t, rt.RuntimeSpec.Bootstrap.Install[0], "astral.sh/uv")

	// Verify binaries
	assert.Equal(t, []string{"uv", "uvx"}, rt.RuntimeSpec.Binaries)

	// Verify commands include {{.Args}} template
	require.NotNil(t, rt.RuntimeSpec.Commands)
	assert.Len(t, rt.RuntimeSpec.Commands.Install, 1)
	assert.Contains(t, rt.RuntimeSpec.Commands.Install[0], "{{.Args}}")
}

// TestCueEcosystem_MockRegistry_NodeToolSetImport verifies that #PnpmToolSet
// resolves correctly via the mock OCI registry.
func TestCueEcosystem_MockRegistry_NodeToolSetImport(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/presets/node"

nodeTools: node.#PnpmToolSet & {
	metadata: name: "node-tools"
	spec: tools: {
		prettier:   {package: "prettier", version: "3.5.3"}
		typescript: {package: "typescript", version: "5.7.3"}
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
	assert.Equal(t, "node-tools", toolset.Name())
	assert.Equal(t, "pnpm", toolset.ToolSetSpec.RuntimeRef)
	assert.Len(t, toolset.ToolSetSpec.Tools, 2)
}

// TestCueEcosystem_MockRegistry_PythonToolSetWithArgs verifies that #UvToolSet
// with the args field resolves correctly via the mock OCI registry.
func TestCueEcosystem_MockRegistry_PythonToolSetWithArgs(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/presets/python"

pythonTools: python.#UvToolSet & {
	metadata: name: "python-tools"
	spec: tools: {
		ruff:    {package: "ruff", version: "0.15.1"}
		ansible: {package: "ansible", version: "13.3.0", args: ["--with-executables-from", "ansible-core"]}
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
	assert.Equal(t, "python-tools", toolset.Name())
	assert.Equal(t, "uv", toolset.ToolSetSpec.RuntimeRef)
	assert.Len(t, toolset.ToolSetSpec.Tools, 2)

	// Verify args survived CUE → JSON → Go round-trip
	ansible, ok := toolset.ToolSetSpec.Tools["ansible"]
	require.True(t, ok)
	assert.Equal(t, []string{"--with-executables-from", "ansible-core"}, ansible.Args)
}

// TestCueEcosystem_MockRegistry_AllPresetsImport verifies that all six presets
// (go, rust, aqua, node, python, deno) can be imported simultaneously without conflicts.
func TestCueEcosystem_MockRegistry_AllPresetsImport(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	platformCue, err := cuemod.GeneratePlatformCUE()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

	manifest := `package tomei

import (
	gopreset "tomei.terassyi.net/presets/go"
	"tomei.terassyi.net/presets/rust"
	"tomei.terassyi.net/presets/aqua"
	"tomei.terassyi.net/presets/node"
	"tomei.terassyi.net/presets/python"
	"tomei.terassyi.net/presets/deno"
	"tomei.terassyi.net/presets/bun"
)

goRuntime: gopreset.#GoRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "1.25.6"
}

rustRuntime: rust.#RustRuntime

pnpmRuntime: node.#PnpmRuntime & {
	spec: version: "10.29.3"
}

uvRuntime: python.#UvRuntime & {
	spec: version: "0.10.2"
}

denoRuntime: deno.#DenoRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "2.6.10"
}

bunRuntime: bun.#BunRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "1.2.21"
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
	require.Len(t, resources, 7)

	// Verify all resource kinds and names are present
	names := make(map[string]resource.Kind)
	for _, r := range resources {
		names[r.Name()] = r.Kind()
	}
	assert.Equal(t, resource.KindRuntime, names["go"])
	assert.Equal(t, resource.KindRuntime, names["rust"])
	assert.Equal(t, resource.KindRuntime, names["pnpm"])
	assert.Equal(t, resource.KindRuntime, names["uv"])
	assert.Equal(t, resource.KindRuntime, names["deno"])
	assert.Equal(t, resource.KindRuntime, names["bun"])
	assert.Equal(t, resource.KindToolSet, names["cli-tools"])
}

// TestCueEcosystem_MockRegistry_DenoPresetImport verifies that the deno preset
// can be imported via the mock OCI registry and #DenoRuntime resolves correctly
// with platform-specific download URLs.
func TestCueEcosystem_MockRegistry_DenoPresetImport(t *testing.T) {
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
			expectedURL: "https://dl.deno.land/release/v{{.Version}}/deno-x86_64-unknown-linux-gnu.zip",
		},
		{
			name:        "linux/arm64",
			os:          "linux",
			arch:        "arm64",
			expectedURL: "https://dl.deno.land/release/v{{.Version}}/deno-aarch64-unknown-linux-gnu.zip",
		},
		{
			name:        "darwin/arm64",
			os:          "darwin",
			arch:        "arm64",
			expectedURL: "https://dl.deno.land/release/v{{.Version}}/deno-aarch64-apple-darwin.zip",
		},
		{
			name:        "darwin/amd64",
			os:          "darwin",
			arch:        "amd64",
			expectedURL: "https://dl.deno.land/release/v{{.Version}}/deno-x86_64-apple-darwin.zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupMockRegistryDir(t, reg)

			platformCue, err := cuemod.GeneratePlatformCUE()
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

			manifest := `package tomei
import "tomei.terassyi.net/presets/deno"
denoRuntime: deno.#DenoRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "2.6.10"
}
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

			loader := config.NewLoader(&config.Env{OS: config.OS(tt.os), Arch: config.Arch(tt.arch), Headless: false})
			resources, err := loader.Load(dir)
			require.NoError(t, err)
			require.Len(t, resources, 1)

			rt, ok := resources[0].(*resource.Runtime)
			require.True(t, ok)
			assert.Equal(t, "deno", rt.Name())
			assert.Equal(t, "2.6.10", rt.RuntimeSpec.Version)
			assert.Equal(t, tt.expectedURL, rt.RuntimeSpec.Source.URL)
			assert.Equal(t, "zip", string(rt.RuntimeSpec.Source.ArchiveType))
		})
	}
}

// TestCueEcosystem_MockRegistry_DenoToolSetImport verifies that #DenoToolSet
// resolves correctly via the mock OCI registry.
func TestCueEcosystem_MockRegistry_DenoToolSetImport(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/presets/deno"

denoTools: deno.#DenoToolSet & {
	metadata: name: "deno-tools"
	spec: tools: {
		deployctl: {package: "jsr:@deno/deployctl", version: "1.12.0"}
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
	assert.Equal(t, "deno-tools", toolset.Name())
	assert.Equal(t, "deno", toolset.ToolSetSpec.RuntimeRef)
	assert.Len(t, toolset.ToolSetSpec.Tools, 1)
}

// TestCueEcosystem_MockRegistry_BunPresetImport verifies that the bun preset
// can be imported via the mock OCI registry and #BunRuntime resolves correctly
// with platform-specific download URLs.
func TestCueEcosystem_MockRegistry_BunPresetImport(t *testing.T) {
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
			expectedURL: "https://github.com/oven-sh/bun/releases/download/bun-v{{.Version}}/bun-linux-x64.zip",
		},
		{
			name:        "linux/arm64",
			os:          "linux",
			arch:        "arm64",
			expectedURL: "https://github.com/oven-sh/bun/releases/download/bun-v{{.Version}}/bun-linux-aarch64.zip",
		},
		{
			name:        "darwin/arm64",
			os:          "darwin",
			arch:        "arm64",
			expectedURL: "https://github.com/oven-sh/bun/releases/download/bun-v{{.Version}}/bun-darwin-aarch64.zip",
		},
		{
			name:        "darwin/amd64",
			os:          "darwin",
			arch:        "amd64",
			expectedURL: "https://github.com/oven-sh/bun/releases/download/bun-v{{.Version}}/bun-darwin-x64.zip",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupMockRegistryDir(t, reg)

			platformCue, err := cuemod.GeneratePlatformCUE()
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "tomei_platform.cue"), platformCue, 0644))

			manifest := `package tomei
import "tomei.terassyi.net/presets/bun"
bunRuntime: bun.#BunRuntime & {
	platform: { os: _os, arch: _arch }
	spec: version: "1.2.21"
}
`
			require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

			loader := config.NewLoader(&config.Env{OS: config.OS(tt.os), Arch: config.Arch(tt.arch), Headless: false})
			resources, err := loader.Load(dir)
			require.NoError(t, err)
			require.Len(t, resources, 1)

			rt, ok := resources[0].(*resource.Runtime)
			require.True(t, ok)
			assert.Equal(t, "bun", rt.Name())
			assert.Equal(t, "1.2.21", rt.RuntimeSpec.Version)
			assert.Equal(t, tt.expectedURL, rt.RuntimeSpec.Source.URL)
			assert.Equal(t, "zip", string(rt.RuntimeSpec.Source.ArchiveType))
			assert.Equal(t, []string{"bun"}, rt.RuntimeSpec.Binaries)
		})
	}
}

// TestCueEcosystem_MockRegistry_BunToolSetImport verifies that #BunToolSet
// resolves correctly via the mock OCI registry.
func TestCueEcosystem_MockRegistry_BunToolSetImport(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/presets/bun"

bunTools: bun.#BunToolSet & {
	metadata: name: "bun-tools"
	spec: tools: {
		prettier: {package: "prettier", version: "3.5.0"}
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
	assert.Equal(t, "bun-tools", toolset.Name())
	assert.Equal(t, "bun", toolset.ToolSetSpec.RuntimeRef)
	assert.Len(t, toolset.ToolSetSpec.Tools, 1)
}

// TestCueEcosystem_MockRegistry_ToolWithCommands verifies that a Tool with
// the commands pattern (self-managed tool) can be loaded via schema import
// from the mock OCI registry and all ToolCommandSet fields survive the
// CUE -> JSON -> Go round-trip.
func TestCueEcosystem_MockRegistry_ToolWithCommands(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/schema"

myTool: schema.#Tool & {
	metadata: name: "mytool"
	spec: {
		commands: {
			install: ["curl -fsSL https://example.com/install.sh | sh"]
			update: ["mytool update"]
			check: ["mytool --version"]
			remove: ["mytool uninstall"]
			resolveVersion: ["mytool --version | head -1"]
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)

	tool, ok := resources[0].(*resource.Tool)
	require.True(t, ok, "resource should be *resource.Tool")
	assert.Equal(t, "mytool", tool.Name())

	// Commands must be non-nil
	require.NotNil(t, tool.ToolSpec.Commands, "commands must not be nil for commands-pattern tool")

	// Verify all ToolCommandSet fields
	assert.Equal(t, []string{"curl -fsSL https://example.com/install.sh | sh"}, tool.ToolSpec.Commands.Install)
	assert.Equal(t, []string{"mytool update"}, tool.ToolSpec.Commands.Update)
	assert.Equal(t, []string{"mytool --version"}, tool.ToolSpec.Commands.Check)
	assert.Equal(t, []string{"mytool uninstall"}, tool.ToolSpec.Commands.Remove)
	assert.Equal(t, []string{"mytool --version | head -1"}, tool.ToolSpec.Commands.ResolveVersion)

	// InstallerRef and RuntimeRef must be empty for commands pattern
	assert.Empty(t, tool.ToolSpec.InstallerRef)
	assert.Empty(t, tool.ToolSpec.RuntimeRef)
}

// TestCueEcosystem_MockRegistry_ToolCommandSetSchemaValidation verifies that
// the CUE schema rejects invalid ToolCommandSet configurations.
func TestCueEcosystem_MockRegistry_ToolCommandSetSchemaValidation(t *testing.T) {
	reg := startMockRegistry(t)

	tests := []struct {
		name     string
		manifest string
	}{
		{
			name: "empty commands (missing required install)",
			manifest: `package tomei

import "tomei.terassyi.net/schema"

badTool: schema.#Tool & {
	metadata: name: "bad"
	spec: {
		commands: {}
	}
}
`,
		},
		{
			name: "commands with empty install list",
			manifest: `package tomei

import "tomei.terassyi.net/schema"

badTool: schema.#Tool & {
	metadata: name: "bad"
	spec: {
		commands: {
			install: []
		}
	}
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setupMockRegistryDir(t, reg)
			require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(tt.manifest), 0644))

			loader := config.NewLoader(nil)
			_, err := loader.Load(dir)
			assert.Error(t, err, "CUE schema should reject invalid commands: %s", tt.name)
		})
	}
}

// TestCueEcosystem_MockRegistry_ToolCommandsSingleElementList verifies that
// single-element lists in ToolCommandSet survive the CUE -> MarshalJSON ->
// UnmarshalJSON round-trip. CUE's MarshalJSON serializes single-element
// [...string] as a bare string, which our custom UnmarshalJSON must handle.
func TestCueEcosystem_MockRegistry_ToolCommandsSingleElementList(t *testing.T) {
	reg := startMockRegistry(t)
	dir := setupMockRegistryDir(t, reg)

	manifest := `package tomei

import "tomei.terassyi.net/schema"

myTool: schema.#Tool & {
	metadata: name: "singletool"
	spec: {
		commands: {
			install: ["single-install-cmd"]
		}
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(manifest), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.Load(dir)
	require.NoError(t, err)
	require.Len(t, resources, 1)

	tool, ok := resources[0].(*resource.Tool)
	require.True(t, ok)
	require.NotNil(t, tool.ToolSpec.Commands)

	// Single-element list must survive as []string{"single-install-cmd"}, not a bare string
	assert.Equal(t, []string{"single-install-cmd"}, tool.ToolSpec.Commands.Install,
		"single-element install list must survive CUE -> JSON -> Go round-trip")
	// Optional fields not specified should be nil
	assert.Nil(t, tool.ToolSpec.Commands.Update)
	assert.Nil(t, tool.ToolSpec.Commands.Check)
	assert.Nil(t, tool.ToolSpec.Commands.Remove)
	assert.Nil(t, tool.ToolSpec.Commands.ResolveVersion)
}
