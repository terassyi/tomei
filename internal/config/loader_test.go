package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/mod/module"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/verify"
)

// setupMinimalCueMod creates a minimal cue.mod/module.cue in dir for tests.
func setupMinimalCueMod(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0755))
	moduleCue := "module: \"test.local@v0\"\nlanguage: version: \"v0.9.0\"\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "cue.mod", "module.cue"),
		[]byte(moduleCue), 0644,
	))
}

func TestLoader_LoadFile_Tool(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cueFile := filepath.Join(dir, "tool.cue")

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "ripgrep"
spec: {
    installerRef: "aqua"
    version: "14.0.0"
    source: {
        url: "https://github.com/BurntSushi/ripgrep/releases/download/14.0.0/ripgrep-14.0.0-x86_64-unknown-linux-musl.tar.gz"
        checksum: {
            value: "sha256:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
        }
        archiveType: "tar.gz"
    }
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(&Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	res := resources[0]
	if res.Kind() != resource.KindTool {
		t.Errorf("expected kind Tool, got %s", res.Kind())
	}
	if res.Name() != "ripgrep" {
		t.Errorf("expected name ripgrep, got %s", res.Name())
	}

	tool, ok := res.(*resource.Tool)
	if !ok {
		t.Fatalf("expected *resource.Tool, got %T", res)
	}
	if tool.ToolSpec.Version != "14.0.0" {
		t.Errorf("expected version 14.0.0, got %s", tool.ToolSpec.Version)
	}
	if tool.ToolSpec.InstallerRef != "aqua" {
		t.Errorf("expected installerRef aqua, got %s", tool.ToolSpec.InstallerRef)
	}
}

func TestLoader_LoadFile_ToolSet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cueFile := filepath.Join(dir, "toolset.cue")

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "ToolSet"
metadata: name: "cli-tools"
spec: {
    installerRef: "aqua"
    tools: {
        ripgrep: { version: "14.0.0" }
        fd: { version: "9.0.0" }
    }
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(nil)
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	res := resources[0]
	if res.Kind() != resource.KindToolSet {
		t.Errorf("expected kind ToolSet, got %s", res.Kind())
	}

	toolSet, ok := res.(*resource.ToolSet)
	if !ok {
		t.Fatalf("expected *resource.ToolSet, got %T", res)
	}
	if len(toolSet.ToolSetSpec.Tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(toolSet.ToolSetSpec.Tools))
	}
	if toolSet.ToolSetSpec.Tools["ripgrep"].Version != "14.0.0" {
		t.Errorf("expected ripgrep version 14.0.0, got %s", toolSet.ToolSetSpec.Tools["ripgrep"].Version)
	}
}

func TestLoader_LoadFile_Runtime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cueFile := filepath.Join(dir, "runtime.cue")

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Runtime"
metadata: name: "go"
spec: {
    type: "download"
    version: "1.25.1"
    source: {
        url: "https://go.dev/dl/go1.25.1.linux-amd64.tar.gz"
        archiveType: "tar.gz"
    }
    binaries: ["go", "gofmt"]
    toolBinPath: "~/go/bin"
    env: {
        GOROOT: "~/.local/share/tomei/runtimes/go/1.25.1"
    }
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(nil)
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	res := resources[0]
	if res.Kind() != resource.KindRuntime {
		t.Errorf("expected kind Runtime, got %s", res.Kind())
	}

	runtime, ok := res.(*resource.Runtime)
	if !ok {
		t.Fatalf("expected *resource.Runtime, got %T", res)
	}
	if runtime.RuntimeSpec.Version != "1.25.1" {
		t.Errorf("expected version 1.25.1, got %s", runtime.RuntimeSpec.Version)
	}
	if len(runtime.RuntimeSpec.Binaries) != 2 {
		t.Errorf("expected 2 binaries, got %d", len(runtime.RuntimeSpec.Binaries))
	}
}

func TestLoader_LoadFile_SystemPackageSet(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cueFile := filepath.Join(dir, "syspkg.cue")

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "SystemPackageSet"
metadata: name: "cli-tools"
spec: {
    installerRef: "apt"
    packages: ["jq", "curl", "htop"]
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(nil)
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	res := resources[0]
	if res.Kind() != resource.KindSystemPackageSet {
		t.Errorf("expected kind SystemPackageSet, got %s", res.Kind())
	}

	pkgSet, ok := res.(*resource.SystemPackageSet)
	if !ok {
		t.Fatalf("expected *resource.SystemPackageSet, got %T", res)
	}
	if len(pkgSet.SystemPackageSetSpec.Packages) != 3 {
		t.Errorf("expected 3 packages, got %d", len(pkgSet.SystemPackageSetSpec.Packages))
	}
}

func TestLoader_LoadFile_WithLabels(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cueFile := filepath.Join(dir, "tool.cue")

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: {
    name: "ripgrep"
    labels: {
        category: "search"
        priority: "high"
    }
}
spec: {
    installerRef: "aqua"
    version: "14.0.0"
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(nil)
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	res := resources[0]
	labels := res.Labels()
	if len(labels) != 2 {
		t.Errorf("expected 2 labels, got %d", len(labels))
	}
	if labels["category"] != "search" {
		t.Errorf("expected label category=search, got %s", labels["category"])
	}
}

func TestLoader_LoadFile_WithDescription(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cueFile := filepath.Join(dir, "tool.cue")

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: {
    name: "ripgrep"
    description: "A fast line-oriented search tool"
    labels: {
        category: "search"
    }
}
spec: {
    installerRef: "aqua"
    version: "14.0.0"
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(nil)
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	res := resources[0]
	tool, ok := res.(*resource.Tool)
	if !ok {
		t.Fatalf("expected *resource.Tool, got %T", res)
	}
	if tool.Metadata.Description != "A fast line-oriented search tool" {
		t.Errorf("expected description 'A fast line-oriented search tool', got %q", tool.Metadata.Description)
	}
	if res.Labels()["category"] != "search" {
		t.Errorf("expected label category=search, got %s", res.Labels()["category"])
	}
}

func TestLoader_Load_Directory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	// Write a CUE file with package declaration
	content := `
package tomei

apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "ripgrep"
spec: {
    installerRef: "aqua"
    version: "14.0.0"
}
`
	if err := os.WriteFile(filepath.Join(dir, "tool.cue"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(nil)
	resources, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("failed to load directory: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
}

func TestEnv_Detect(t *testing.T) {
	t.Parallel()

	env := DetectEnv()

	// Basic sanity checks
	if env.OS != "linux" && env.OS != "darwin" {
		t.Errorf("unexpected OS: %s", env.OS)
	}
	if env.Arch != "amd64" && env.Arch != "arm64" {
		t.Errorf("unexpected Arch: %s", env.Arch)
	}
}

func TestLoader_Tag_StringInterpolation(t *testing.T) {
	t.Parallel()

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
            url: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_\(_os)_\(_arch).tar.gz"
        }
    }
}
`
	tests := []struct {
		name        string
		env         *Env
		expectedURL string
	}{
		{
			name:        "linux/arm64",
			env:         &Env{OS: "linux", Arch: "arm64", Headless: false},
			expectedURL: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_linux_arm64.tar.gz",
		},
		{
			name:        "darwin/amd64",
			env:         &Env{OS: "darwin", Arch: "amd64", Headless: false},
			expectedURL: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_darwin_amd64.tar.gz",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			setupMinimalCueMod(t, dir)
			if err := os.WriteFile(filepath.Join(dir, "tool.cue"), []byte(content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			loader := NewLoader(tt.env)
			resources, err := loader.Load(dir)
			if err != nil {
				t.Fatalf("failed to load: %v", err)
			}

			tool := resources[0].(*resource.Tool)
			if tool.ToolSpec.Source.URL != tt.expectedURL {
				t.Errorf("expected URL %s, got %s", tt.expectedURL, tool.ToolSpec.Source.URL)
			}
		})
	}
}

func TestLoader_Tag_RuntimeURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	content := `package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

goRuntime: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind: "Runtime"
    metadata: name: "go"
    spec: {
        type: "download"
        version: "1.23.6"
        source: {
            url: "https://go.dev/dl/go1.23.6.\(_os)-\(_arch).tar.gz"
            archiveType: "tar.gz"
        }
        binaries: ["go", "gofmt"]
        toolBinPath: "~/go/bin"
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "runtime.cue"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(&Env{OS: "linux", Arch: "arm64", Headless: false})
	resources, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	runtime := resources[0].(*resource.Runtime)
	expectedURL := "https://go.dev/dl/go1.23.6.linux-arm64.tar.gz"
	if runtime.RuntimeSpec.Source.URL != expectedURL {
		t.Errorf("expected URL %s, got %s", expectedURL, runtime.RuntimeSpec.Source.URL)
	}
}

func TestLoader_Tag_Headless(t *testing.T) {
	t.Parallel()

	content := `package tomei

_headless: bool @tag(headless,type=bool)

testTool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind: "Tool"
    metadata: name: "test-tool"
    spec: {
        installerRef: "download"
        version: "1.0.0"
        if _headless {
            enabled: false
        }
        if !_headless {
            enabled: true
        }
    }
}
`
	tests := []struct {
		name        string
		headless    bool
		wantEnabled bool
	}{
		{name: "headless=true", headless: true, wantEnabled: false},
		{name: "headless=false", headless: false, wantEnabled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			setupMinimalCueMod(t, dir)
			if err := os.WriteFile(filepath.Join(dir, "tool.cue"), []byte(content), 0644); err != nil {
				t.Fatalf("failed to write test file: %v", err)
			}

			loader := NewLoader(&Env{OS: "linux", Arch: "amd64", Headless: tt.headless})
			resources, err := loader.Load(dir)
			if err != nil {
				t.Fatalf("failed to load: %v", err)
			}

			tool := resources[0].(*resource.Tool)
			if tool.ToolSpec.Enabled == nil || *tool.ToolSpec.Enabled != tt.wantEnabled {
				t.Errorf("expected enabled=%v for headless=%v", tt.wantEnabled, tt.headless)
			}
		})
	}
}

func TestLoader_Tag_DirectoryLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	// Write a CUE file with package declaration using @tag()
	content := `
package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

tool: {
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
	if err := os.WriteFile(filepath.Join(dir, "tool.cue"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(&Env{OS: "darwin", Arch: "arm64", Headless: false})
	resources, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("failed to load directory: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	tool := resources[0].(*resource.Tool)
	expectedURL := "https://example.com/gh_darwin_arm64.tar.gz"
	if tool.ToolSpec.Source.URL != expectedURL {
		t.Errorf("expected URL %s, got %s", expectedURL, tool.ToolSpec.Source.URL)
	}
}

func TestLoader_LoadFile_InstallerRepository_Delegation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cueFile := filepath.Join(dir, "repo.cue")

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "InstallerRepository"
metadata: name: "bitnami"
spec: {
    installerRef: "helm"
    source: {
        type: "delegation"
        commands: {
            install: ["helm repo add bitnami https://charts.bitnami.com/bitnami"]
            check:   ["helm repo list | grep bitnami"]
            remove:  ["helm repo remove bitnami"]
        }
    }
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(nil)
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	res := resources[0]
	if res.Kind() != resource.KindInstallerRepository {
		t.Errorf("expected kind InstallerRepository, got %s", res.Kind())
	}
	if res.Name() != "bitnami" {
		t.Errorf("expected name bitnami, got %s", res.Name())
	}

	repo, ok := res.(*resource.InstallerRepository)
	if !ok {
		t.Fatalf("expected *resource.InstallerRepository, got %T", res)
	}
	if repo.InstallerRepositorySpec.InstallerRef != "helm" {
		t.Errorf("expected installerRef helm, got %s", repo.InstallerRepositorySpec.InstallerRef)
	}
	if repo.InstallerRepositorySpec.Source.Type != resource.InstallerRepositorySourceDelegation {
		t.Errorf("expected source type delegation, got %s", repo.InstallerRepositorySpec.Source.Type)
	}
	wantInstall := []string{"helm repo add bitnami https://charts.bitnami.com/bitnami"}
	assert.Equal(t, wantInstall, repo.InstallerRepositorySpec.Source.Commands.Install)
}

func TestLoader_LoadFile_InstallerRepository_Git(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cueFile := filepath.Join(dir, "repo.cue")

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "InstallerRepository"
metadata: name: "custom-registry"
spec: {
    installerRef: "aqua"
    source: {
        type: "git"
        url:  "https://github.com/my-org/aqua-registry"
    }
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(nil)
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}

	repo := resources[0].(*resource.InstallerRepository)
	if repo.InstallerRepositorySpec.InstallerRef != "aqua" {
		t.Errorf("expected installerRef aqua, got %s", repo.InstallerRepositorySpec.InstallerRef)
	}
	if repo.InstallerRepositorySpec.Source.Type != resource.InstallerRepositorySourceGit {
		t.Errorf("expected source type git, got %s", repo.InstallerRepositorySpec.Source.Type)
	}
	if repo.InstallerRepositorySpec.Source.URL != "https://github.com/my-org/aqua-registry" {
		t.Errorf("unexpected URL: %s", repo.InstallerRepositorySpec.Source.URL)
	}
}

func TestLoader_SchemaValidation_DirectoryMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	// Valid manifest in directory mode with package declaration
	content := `
package tomei

gh: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind: "Tool"
    metadata: name: "gh"
    spec: {
        installerRef: "download"
        version: "2.86.0"
        source: {
            url: "https://example.com/gh.tar.gz"
        }
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(&Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("failed to load directory: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Name() != "gh" {
		t.Errorf("expected name gh, got %s", resources[0].Name())
	}
}

func TestLoader_Load_NoImports_StillWorks(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	// Manifest without any imports
	content := `package tomei

tool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind: "Tool"
    metadata: name: "gh"
    spec: {
        installerRef: "download"
        version: "2.86.0"
        source: {
            url: "https://example.com/gh.tar.gz"
        }
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "tool.cue"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(&Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Name() != "gh" {
		t.Errorf("expected name gh, got %s", resources[0].Name())
	}
}

func TestLoader_Tag_NotAvailableWithoutPackage(t *testing.T) {
	t.Parallel()

	// @tag() requires load.Instances() which needs a package declaration.
	// Without a package, CompileString is used and tags are not resolved.
	// When a tag value is used in string interpolation, it fails because
	// the value is non-concrete (still typed as string, not a concrete value).
	dir := t.TempDir()
	cueFile := filepath.Join(dir, "tool.cue")

	content := `
_os: string @tag(os)

apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "test"
spec: {
    installerRef: "download"
    version: "1.0.0"
    source: {
        url: "https://example.com/test_\(_os).tar.gz"
    }
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(&Env{OS: "linux", Arch: "amd64", Headless: false})
	// LoadFile with no package → CompileString path → _os stays as string (incomplete)
	_, err := loader.LoadFile(cueFile)
	// Should fail because _os is not concrete (tag not resolved via CompileString)
	if err == nil {
		t.Error("expected error for @tag() without package, got nil")
	}
}

func TestLoader_Tag_MultipleFilesShareTags(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	// Two files in the same package both declaring @tag()
	file1 := `package tomei

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
	file2 := `package tomei

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
	if err := os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(file1), 0644); err != nil {
		t.Fatalf("failed to write file1: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "runtime.cue"), []byte(file2), 0644); err != nil {
		t.Fatalf("failed to write file2: %v", err)
	}

	loader := NewLoader(&Env{OS: "darwin", Arch: "arm64", Headless: false})
	resources, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}

	// Verify both resources got correct tag values
	for _, res := range resources {
		switch r := res.(type) {
		case *resource.Tool:
			expectedURL := "https://example.com/gh_darwin_arm64.tar.gz"
			if r.ToolSpec.Source.URL != expectedURL {
				t.Errorf("tool URL: expected %s, got %s", expectedURL, r.ToolSpec.Source.URL)
			}
		case *resource.Runtime:
			expectedURL := "https://go.dev/dl/go1.25.6.darwin-arm64.tar.gz"
			if r.RuntimeSpec.Source.URL != expectedURL {
				t.Errorf("runtime URL: expected %s, got %s", expectedURL, r.RuntimeSpec.Source.URL)
			}
		}
	}
}

func TestLoader_Tag_ConstrainedValues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	content := `package tomei

_os: ("linux" | "darwin") @tag(os)

tool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind: "Tool"
    metadata: name: "test"
    spec: {
        installerRef: "download"
        version: "1.0.0"
        source: {
            url: "https://example.com/test_\(_os).tar.gz"
        }
    }
}
`
	if err := os.WriteFile(filepath.Join(dir, "tool.cue"), []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Valid: linux
	loader := NewLoader(&Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err := loader.Load(dir)
	if err != nil {
		t.Fatalf("failed to load with linux: %v", err)
	}
	tool := resources[0].(*resource.Tool)
	if tool.ToolSpec.Source.URL != "https://example.com/test_linux.tar.gz" {
		t.Errorf("unexpected URL: %s", tool.ToolSpec.Source.URL)
	}
}

func TestLoader_SchemaValidation_WorksWithoutPackage(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cueFile := filepath.Join(dir, "tool.cue")

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "test"
spec: {
    installerRef: "download"
    version: "1.0.0"
    source: {
        url: "https://example.com/test.tar.gz"
    }
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(&Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(resources))
	}
	if resources[0].Name() != "test" {
		t.Errorf("expected name test, got %s", resources[0].Name())
	}
}

func TestBuildRegistry(t *testing.T) {
	tests := []struct {
		name        string
		cueRegistry string
	}{
		{
			name:        "default when CUE_REGISTRY not set",
			cueRegistry: "",
		},
		{
			name:        "custom CUE_REGISTRY",
			cueRegistry: "tomei.terassyi.net=ghcr.io/custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.cueRegistry != "" {
				t.Setenv(EnvCUERegistry, tt.cueRegistry)
			} else {
				t.Setenv(EnvCUERegistry, "")
			}

			registry, err := buildRegistry()
			if err != nil {
				t.Fatalf("buildRegistry() returned error: %v", err)
			}
			if registry == nil {
				t.Fatal("buildRegistry() returned nil registry")
			}
		})
	}
}

func TestEvalDir(t *testing.T) {
	t.Parallel()

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
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644))

	loader := NewLoader(&Env{OS: "linux", Arch: "amd64"})
	value, err := loader.EvalDir(dir)
	require.NoError(t, err)
	require.True(t, value.Exists())

	// cue.Value should expose apiVersion
	av := value.LookupPath(cue.ParsePath("myTool.apiVersion"))
	s, err := av.String()
	require.NoError(t, err)
	assert.Equal(t, "tomei.terassyi.net/v1beta1", s)

	// JSON export should work
	jsonBytes, err := value.MarshalJSON()
	require.NoError(t, err)
	assert.Contains(t, string(jsonBytes), `"tomei.terassyi.net/v1beta1"`)
}

func TestEvalFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "tool.cue")

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "jq"
spec: {
	installerRef: "aqua"
	version:      "1.7.1"
}
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))

	loader := NewLoader(&Env{OS: "linux", Arch: "amd64"})
	value, err := loader.EvalFile(path)
	require.NoError(t, err)
	require.True(t, value.Exists())
	require.NoError(t, value.Err())

	name := value.LookupPath(cue.ParsePath("metadata.name"))
	s, err := name.String()
	require.NoError(t, err)
	assert.Equal(t, "jq", s)
}

func TestEvalFile_ConfigCueSkipped(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.cue")

	require.NoError(t, os.WriteFile(path, []byte(`package tomei`), 0644))

	loader := NewLoader(nil)
	value, err := loader.EvalFile(path)
	require.NoError(t, err)
	assert.False(t, value.Exists(), "config.cue should be skipped")
}

func TestEvalDir_ResolvesTagValues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	content := `package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

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

	loader := NewLoader(&Env{OS: "darwin", Arch: "arm64"})
	value, err := loader.EvalDir(dir)
	require.NoError(t, err)

	url := value.LookupPath(cue.ParsePath("myRuntime.spec.source.url"))
	s, err := url.String()
	require.NoError(t, err)
	assert.Contains(t, s, "darwin-arm64")
}

func TestEvalPaths(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	content := `package tomei

myTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "aqua"
		version:      "15.1.0"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644))

	loader := NewLoader(&Env{OS: "linux", Arch: "amd64"})
	values, err := loader.EvalPaths([]string{dir})
	require.NoError(t, err)
	require.Len(t, values, 1)

	name := values[0].LookupPath(cue.ParsePath("myTool.metadata.name"))
	s, err := name.String()
	require.NoError(t, err)
	assert.Equal(t, "rg", s)
}

func TestEvalDir_EmptyDir(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	loader := NewLoader(nil)
	value, err := loader.EvalDir(dir)
	require.NoError(t, err)
	assert.False(t, value.Exists(), "empty dir should return zero value")
}

func TestEvalDir_JSONExport(t *testing.T) {
	t.Parallel()

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
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644))

	loader := NewLoader(&Env{OS: "linux", Arch: "amd64"})
	value, err := loader.EvalDir(dir)
	require.NoError(t, err)

	jsonBytes, err := value.MarshalJSON()
	require.NoError(t, err)

	var parsed struct {
		MyTool struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
		} `json:"myTool"`
	}
	require.NoError(t, json.Unmarshal(jsonBytes, &parsed))
	assert.Equal(t, "tomei.terassyi.net/v1beta1", parsed.MyTool.APIVersion)
	assert.Equal(t, "Tool", parsed.MyTool.Kind)
}

// mockVerifier implements verify.Verifier for testing.
type mockVerifier struct {
	called bool
	deps   []module.Version
	err    error
}

func (m *mockVerifier) Verify(_ context.Context, deps []module.Version) ([]verify.Result, error) {
	m.called = true
	m.deps = deps
	if m.err != nil {
		return nil, m.err
	}
	results := make([]verify.Result, len(deps))
	for i, dep := range deps {
		results[i] = verify.Result{
			Module:   dep,
			Verified: true,
		}
	}
	return results, nil
}

func TestLoader_WithVerifier_Called(t *testing.T) {
	dir := t.TempDir()
	moduleCue := `module: "test.local@v0"
language: version: "v0.9.0"
deps: {
	"tomei.terassyi.net@v0": v: "v0.0.3"
}
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "cue.mod", "module.cue"),
		[]byte(moduleCue), 0644,
	))

	content := `package test
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "jq"
spec: {
	installerRef: "aqua"
	version: "1.7.1"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644))

	mv := &mockVerifier{}
	loader := NewLoader(&Env{OS: "linux", Arch: "amd64"}, WithVerifier(mv))

	// EvalDir will call verifyModuleDeps before load.Instances.
	// The CUE evaluation itself may fail (no real registry), but verifier should have been called.
	_, _ = loader.EvalDir(dir)

	assert.True(t, mv.called, "verifier should have been called")
	require.Len(t, mv.deps, 1)
	assert.Equal(t, "tomei.terassyi.net@v0", mv.deps[0].Path())
	assert.Equal(t, "v0.0.3", mv.deps[0].Version())
}

func TestLoader_WithVerifier_Error(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	moduleCue := `module: "test.local@v0"
language: version: "v0.9.0"
deps: {
	"tomei.terassyi.net@v0": v: "v0.0.3"
}
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "cue.mod", "module.cue"),
		[]byte(moduleCue), 0644,
	))

	content := `package test
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "jq"
spec: {
	installerRef: "aqua"
	version: "1.7.1"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644))

	mv := &mockVerifier{err: fmt.Errorf("signature verification failed")}
	loader := NewLoader(&Env{OS: "linux", Arch: "amd64"}, WithVerifier(mv))

	_, err := loader.EvalDir(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cosign signature verification failed")
}

func TestLoader_WithoutVerifier_NoError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	setupMinimalCueMod(t, dir)

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "jq"
spec: {
	installerRef: "aqua"
	version: "1.7.1"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644))

	// No verifier, no package declaration → uses CompileString path, should load without error
	loader := NewLoader(&Env{OS: "linux", Arch: "amd64"})
	resources, err := loader.LoadFile(filepath.Join(dir, "tools.cue"))
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, "jq", resources[0].Name())
}

func TestLoader_WithVerifier_NoCueMod(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	// No cue.mod/ — verifier should not be called

	content := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "jq"
spec: {
	installerRef: "aqua"
	version: "1.7.1"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644))

	mv := &mockVerifier{}
	loader := NewLoader(&Env{OS: "linux", Arch: "amd64"}, WithVerifier(mv))

	// No package declaration → uses CompileString, no verifyModuleDeps called
	_, err := loader.LoadFile(filepath.Join(dir, "tools.cue"))
	require.NoError(t, err)
	assert.False(t, mv.called, "verifier should not be called when there is no cue.mod/")
}

func TestLoader_WithVerifier_VendorMode(t *testing.T) {
	dir := t.TempDir()
	moduleCue := `module: "test.local@v0"
language: version: "v0.9.0"
deps: {
	"tomei.terassyi.net@v0": v: "v0.0.3"
}
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "cue.mod", "module.cue"),
		[]byte(moduleCue), 0644,
	))

	content := `package test
myTool: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind: "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version: "1.7.1"
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644))

	mv := &mockVerifier{}
	loader := NewLoader(&Env{OS: "linux", Arch: "amd64"}, WithVerifier(mv))

	t.Setenv(EnvCUERegistry, "none")
	_, _ = loader.EvalDir(dir) // May fail for CUE reasons, but verifier should be skipped

	assert.False(t, mv.called, "verifier should not be called in vendor mode (CUE_REGISTRY=none)")
}

func TestFindCueModDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setup     func(t *testing.T) string // returns the dir to search from
		wantFound bool
	}{
		{
			name: "cue.mod in current directory",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0755))
				return dir
			},
			wantFound: true,
		},
		{
			name: "cue.mod in parent directory",
			setup: func(t *testing.T) string {
				t.Helper()
				parent := t.TempDir()
				require.NoError(t, os.MkdirAll(filepath.Join(parent, "cue.mod"), 0755))
				child := filepath.Join(parent, "subdir")
				require.NoError(t, os.MkdirAll(child, 0755))
				return child
			},
			wantFound: true,
		},
		{
			name: "no cue.mod anywhere",
			setup: func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			},
			wantFound: false,
		},
		{
			name: "cue.mod is a file not a directory",
			setup: func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, "cue.mod"), []byte("not a dir"), 0644))
				return dir
			},
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := tt.setup(t)
			got := findCueModDir(dir)
			if tt.wantFound {
				assert.NotEmpty(t, got)
				assert.DirExists(t, got)
			} else {
				assert.Empty(t, got)
			}
		})
	}
}

func TestLoader_WithVerifier_DedupVerification(t *testing.T) {
	dir := t.TempDir()
	moduleCue := `module: "test.local@v0"
language: version: "v0.9.0"
deps: {
	"tomei.terassyi.net@v0": v: "v0.0.3"
}
`
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "cue.mod"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "cue.mod", "module.cue"),
		[]byte(moduleCue), 0644,
	))

	content := `package test
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "jq"
spec: {
	installerRef: "aqua"
	version: "1.7.1"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tools.cue"), []byte(content), 0644))

	callCount := 0
	mv := &mockVerifier{}
	originalVerify := mv.Verify
	_ = originalVerify
	// Use a counting wrapper to track how many times Verify is called
	countingVerifier := &countingMockVerifier{}
	loader := NewLoader(&Env{OS: "linux", Arch: "amd64"}, WithVerifier(countingVerifier))

	// Call EvalDir twice on the same directory
	_, _ = loader.EvalDir(dir)
	_, _ = loader.EvalDir(dir)

	// Verify should only be called once due to dedup
	assert.Equal(t, 1, countingVerifier.callCount,
		"verifier should only be called once for the same cue.mod directory")
	_ = callCount
}

// countingMockVerifier counts how many times Verify is called.
type countingMockVerifier struct {
	callCount int
}

func (m *countingMockVerifier) Verify(_ context.Context, deps []module.Version) ([]verify.Result, error) {
	m.callCount++
	results := make([]verify.Result, len(deps))
	for i, dep := range deps {
		results[i] = verify.Result{
			Module:   dep,
			Verified: true,
		}
	}
	return results, nil
}
