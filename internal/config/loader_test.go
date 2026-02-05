package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/terassyi/toto/internal/resource"
)

func TestLoader_LoadFile_Tool(t *testing.T) {
	dir := t.TempDir()
	cueFile := filepath.Join(dir, "tool.cue")

	content := `
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "ripgrep"
spec: {
    installerRef: "aqua"
    version: "14.0.0"
    source: {
        url: "https://github.com/BurntSushi/ripgrep/releases/download/14.0.0/ripgrep-14.0.0-x86_64-unknown-linux-musl.tar.gz"
        checksum: {
            value: "sha256:abc123"
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
	dir := t.TempDir()
	cueFile := filepath.Join(dir, "toolset.cue")

	content := `
apiVersion: "toto.terassyi.net/v1beta1"
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
	dir := t.TempDir()
	cueFile := filepath.Join(dir, "runtime.cue")

	content := `
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Runtime"
metadata: name: "go"
spec: {
    installerRef: "download"
    version: "1.25.1"
    source: {
        url: "https://go.dev/dl/go1.25.1.linux-amd64.tar.gz"
        archiveType: "tar.gz"
    }
    binaries: ["go", "gofmt"]
    toolBinPath: "~/go/bin"
    env: {
        GOROOT: "~/.local/share/toto/runtimes/go/1.25.1"
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
	dir := t.TempDir()
	cueFile := filepath.Join(dir, "syspkg.cue")

	content := `
apiVersion: "toto.terassyi.net/v1beta1"
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
	dir := t.TempDir()
	cueFile := filepath.Join(dir, "tool.cue")

	content := `
apiVersion: "toto.terassyi.net/v1beta1"
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

func TestLoader_Load_Directory(t *testing.T) {
	dir := t.TempDir()

	// Write a CUE file with package declaration
	content := `
package toto

apiVersion: "toto.terassyi.net/v1beta1"
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
	env := DetectEnv()

	// Basic sanity checks
	if env.OS != "linux" && env.OS != "darwin" {
		t.Errorf("unexpected OS: %s", env.OS)
	}
	if env.Arch != "amd64" && env.Arch != "arm64" {
		t.Errorf("unexpected Arch: %s", env.Arch)
	}
}

func TestLoader_InjectEnv_StringInterpolation(t *testing.T) {
	dir := t.TempDir()
	cueFile := filepath.Join(dir, "tool.cue")

	// Use _env in string interpolation
	content := `
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "gh"
spec: {
    installerRef: "download"
    version: "2.86.0"
    source: {
        url: "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_\(_env.os)_\(_env.arch).tar.gz"
    }
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Test with linux/arm64
	loader := NewLoader(&Env{OS: "linux", Arch: "arm64", Headless: false})
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	tool := resources[0].(*resource.Tool)
	expectedURL := "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_linux_arm64.tar.gz"
	if tool.ToolSpec.Source.URL != expectedURL {
		t.Errorf("expected URL %s, got %s", expectedURL, tool.ToolSpec.Source.URL)
	}

	// Test with darwin/amd64
	loader = NewLoader(&Env{OS: "darwin", Arch: "amd64", Headless: false})
	resources, err = loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	tool = resources[0].(*resource.Tool)
	expectedURL = "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_darwin_amd64.tar.gz"
	if tool.ToolSpec.Source.URL != expectedURL {
		t.Errorf("expected URL %s, got %s", expectedURL, tool.ToolSpec.Source.URL)
	}
}

func TestLoader_InjectEnv_RuntimeURL(t *testing.T) {
	dir := t.TempDir()
	cueFile := filepath.Join(dir, "runtime.cue")

	// Use _env for runtime URL
	content := `
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Runtime"
metadata: name: "go"
spec: {
    installerRef: "download"
    version: "1.23.6"
    source: {
        url: "https://go.dev/dl/go1.23.6.\(_env.os)-\(_env.arch).tar.gz"
        archiveType: "tar.gz"
    }
    binaries: ["go", "gofmt"]
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	loader := NewLoader(&Env{OS: "linux", Arch: "arm64", Headless: false})
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	runtime := resources[0].(*resource.Runtime)
	expectedURL := "https://go.dev/dl/go1.23.6.linux-arm64.tar.gz"
	if runtime.RuntimeSpec.Source.URL != expectedURL {
		t.Errorf("expected URL %s, got %s", expectedURL, runtime.RuntimeSpec.Source.URL)
	}
}

func TestLoader_InjectEnv_Headless(t *testing.T) {
	dir := t.TempDir()
	cueFile := filepath.Join(dir, "tool.cue")

	// Use _env.headless for conditional field
	content := `
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "test-tool"
spec: {
    installerRef: "download"
    version: "1.0.0"
    if _env.headless {
        enabled: false
    }
    if !_env.headless {
        enabled: true
    }
}
`
	if err := os.WriteFile(cueFile, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Test with headless=true
	loader := NewLoader(&Env{OS: "linux", Arch: "amd64", Headless: true})
	resources, err := loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	tool := resources[0].(*resource.Tool)
	if tool.ToolSpec.Enabled == nil || *tool.ToolSpec.Enabled != false {
		t.Errorf("expected enabled=false for headless environment")
	}

	// Test with headless=false
	loader = NewLoader(&Env{OS: "linux", Arch: "amd64", Headless: false})
	resources, err = loader.LoadFile(cueFile)
	if err != nil {
		t.Fatalf("failed to load file: %v", err)
	}

	tool = resources[0].(*resource.Tool)
	if tool.ToolSpec.Enabled == nil || *tool.ToolSpec.Enabled != true {
		t.Errorf("expected enabled=true for non-headless environment")
	}
}

func TestLoader_InjectEnv_DirectoryLoad(t *testing.T) {
	dir := t.TempDir()

	// Write a CUE file with package declaration using _env
	content := `
package toto

tool: {
    apiVersion: "toto.terassyi.net/v1beta1"
    kind: "Tool"
    metadata: name: "gh"
    spec: {
        installerRef: "download"
        version: "2.86.0"
        source: {
            url: "https://example.com/gh_\(_env.os)_\(_env.arch).tar.gz"
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
