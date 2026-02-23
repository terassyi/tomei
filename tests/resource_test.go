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

func TestLoadAndStore(t *testing.T) {
	dir := t.TempDir()

	// Create CUE files with multiple resource types
	toolCue := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: {
	name: "ripgrep"
	labels: {
		category: "search"
	}
}
spec: {
	installerRef: "aqua"
	version: "14.0.0"
}
`
	runtimeCue := `
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
}
`
	installerCue := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "aqua"
spec: {
	type: "download"
}
`
	toolSetCue := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "ToolSet"
metadata: name: "cli-tools"
spec: {
	installerRef: "aqua"
	tools: {
		fd: { version: "9.0.0" }
		bat: { version: "0.24.0" }
	}
}
`

	// Write CUE files
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tool.cue"), []byte(toolCue), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "runtime.cue"), []byte(runtimeCue), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "installer.cue"), []byte(installerCue), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "toolset.cue"), []byte(toolSetCue), 0644))

	// Load resources
	loader := config.NewLoader(nil)
	var allResources []resource.Resource

	for _, file := range []string{"tool.cue", "runtime.cue", "installer.cue", "toolset.cue"} {
		resources, err := loader.LoadFile(filepath.Join(dir, file))
		require.NoError(t, err)
		allResources = append(allResources, resources...)
	}

	assert.Len(t, allResources, 4)

	// Create store and add resources
	store := resource.NewStore()
	for _, res := range allResources {
		store.Add(res)
	}

	// Test generic Get
	t.Run("Get Tool by type", func(t *testing.T) {
		tool, ok := resource.Get[*resource.Tool](store, "ripgrep")
		require.True(t, ok)
		assert.Equal(t, "ripgrep", tool.Name())
		assert.Equal(t, resource.KindTool, tool.Kind())
		assert.Equal(t, "14.0.0", tool.ToolSpec.Version)
		assert.Equal(t, "aqua", tool.ToolSpec.InstallerRef)
		assert.Equal(t, "search", tool.Labels()["category"])
	})

	t.Run("Get Runtime by type", func(t *testing.T) {
		runtime, ok := resource.Get[*resource.Runtime](store, "go")
		require.True(t, ok)
		assert.Equal(t, "go", runtime.Name())
		assert.Equal(t, resource.KindRuntime, runtime.Kind())
		assert.Equal(t, "1.25.1", runtime.RuntimeSpec.Version)
		assert.Contains(t, runtime.RuntimeSpec.Binaries, "go")
		assert.Contains(t, runtime.RuntimeSpec.Binaries, "gofmt")
	})

	t.Run("Get Installer by type", func(t *testing.T) {
		installer, ok := resource.Get[*resource.Installer](store, "aqua")
		require.True(t, ok)
		assert.Equal(t, "aqua", installer.Name())
		assert.Equal(t, resource.KindInstaller, installer.Kind())
		assert.Equal(t, resource.InstallTypeDownload, installer.InstallerSpec.Type)
	})

	t.Run("Get ToolSet by type", func(t *testing.T) {
		toolSet, ok := resource.Get[*resource.ToolSet](store, "cli-tools")
		require.True(t, ok)
		assert.Equal(t, "cli-tools", toolSet.Name())
		assert.Equal(t, resource.KindToolSet, toolSet.Kind())
		assert.Len(t, toolSet.ToolSetSpec.Tools, 2)
		assert.Equal(t, "9.0.0", toolSet.ToolSetSpec.Tools["fd"].Version)
		assert.Equal(t, "0.24.0", toolSet.ToolSetSpec.Tools["bat"].Version)
	})

	t.Run("Get non-existent resource returns false", func(t *testing.T) {
		_, ok := resource.Get[*resource.Tool](store, "nonexistent")
		assert.False(t, ok)
	})

	t.Run("Get with wrong type returns false", func(t *testing.T) {
		// "ripgrep" is a Tool, not a Runtime
		_, ok := resource.Get[*resource.Runtime](store, "ripgrep")
		assert.False(t, ok)
	})

	// Test generic List
	t.Run("List Tools", func(t *testing.T) {
		tools := resource.List[*resource.Tool](store)
		assert.Len(t, tools, 1)
		assert.Equal(t, "ripgrep", tools[0].Name())
	})

	t.Run("List Runtimes", func(t *testing.T) {
		runtimes := resource.List[*resource.Runtime](store)
		assert.Len(t, runtimes, 1)
		assert.Equal(t, "go", runtimes[0].Name())
	})

	t.Run("List empty type returns empty slice", func(t *testing.T) {
		systemInstallers := resource.List[*resource.SystemInstaller](store)
		assert.Empty(t, systemInstallers)
	})
}

func TestSpecValidation(t *testing.T) {
	dir := t.TempDir()

	toolCue := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "ripgrep"
spec: {
	installerRef: "aqua"
	version: "14.0.0"
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tool.cue"), []byte(toolCue), 0644))

	loader := config.NewLoader(nil)
	resources, err := loader.LoadFile(filepath.Join(dir, "tool.cue"))
	require.NoError(t, err)
	require.Len(t, resources, 1)

	tool := resources[0].(*resource.Tool)

	// Test Spec() returns valid Spec interface
	spec := tool.Spec()
	require.NotNil(t, spec)

	// Test Validate()
	err = spec.Validate()
	require.NoError(t, err)

	// Test Dependencies()
	deps := spec.Dependencies()
	require.Len(t, deps, 1)
	assert.Equal(t, resource.KindInstaller, deps[0].Kind)
	assert.Equal(t, "aqua", deps[0].Name)
}

func TestDependencyResolution(t *testing.T) {
	dir := t.TempDir()

	// Tool uses runtimeRef for delegation installation (go install).
	// runtimeRef and installerRef are mutually exclusive per Validate(),
	// so we use only runtimeRef here.
	toolCue := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "golangci-lint"
spec: {
	runtimeRef: "go"
	package: "github.com/golangci/golangci-lint/cmd/golangci-lint"
	version: "v1.55.0"
}
`
	// Installer depends on Runtime
	installerCue := `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "go"
spec: {
	type: "delegation"
	runtimeRef: "go"
	commands: {
		install: ["go install {{.Package}}@{{.Version}}"]
		check: ["go version -m {{.BinPath}}"]
	}
}
`
	runtimeCue := `
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
}
`

	require.NoError(t, os.WriteFile(filepath.Join(dir, "tool.cue"), []byte(toolCue), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "installer.cue"), []byte(installerCue), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "runtime.cue"), []byte(runtimeCue), 0644))

	loader := config.NewLoader(nil)
	store := resource.NewStore()

	for _, file := range []string{"tool.cue", "installer.cue", "runtime.cue"} {
		resources, err := loader.LoadFile(filepath.Join(dir, file))
		require.NoError(t, err)
		for _, res := range resources {
			store.Add(res)
		}
	}

	// Get tool and check dependencies
	tool, ok := resource.Get[*resource.Tool](store, "golangci-lint")
	require.True(t, ok)

	// Validate passes — runtimeRef only, no mutual exclusivity violation
	require.NoError(t, tool.Spec().Validate())

	deps := tool.Spec().Dependencies()
	assert.Len(t, deps, 1) // Runtime only (no installerRef)

	// Verify runtime dependency can be resolved from store
	for _, dep := range deps {
		switch dep.Kind {
		case resource.KindRuntime:
			runtime, ok := resource.Get[*resource.Runtime](store, dep.Name)
			assert.True(t, ok, "Runtime dependency should be resolvable")
			assert.Equal(t, "go", runtime.Name())
		default:
			t.Errorf("unexpected dependency kind: %s", dep.Kind)
		}
	}

	// Check installer's dependency on runtime
	installer, _ := resource.Get[*resource.Installer](store, "go")
	installerDeps := installer.Spec().Dependencies()
	require.Len(t, installerDeps, 1)
	assert.Equal(t, resource.KindRuntime, installerDeps[0].Kind)
	assert.Equal(t, "go", installerDeps[0].Name)
}

// TestSchemaValidation_AcceptsValid tests that valid manifests for all resource
// types pass schema validation through the full loading pipeline.
func TestSchemaValidation_AcceptsValid(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantKind resource.Kind
		wantName string
	}{
		{
			name: "Tool with download source",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "ripgrep"
spec: {
	installerRef: "download"
	version: "14.0.0"
	source: {
		url: "https://example.com/rg.tar.gz"
		checksum: value: "sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
		archiveType: "tar.gz"
	}
}
`,
			wantKind: resource.KindTool,
			wantName: "ripgrep",
		},
		{
			name: "Tool with tar.xz download source",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "zig"
spec: {
	installerRef: "download"
	version: "0.14.0"
	source: url: "https://ziglang.org/download/0.14.0/zig-x86_64-linux-0.14.0.tar.xz"
}
`,
			wantKind: resource.KindTool,
			wantName: "zig",
		},
		{
			name: "Tool with runtime delegation",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "gopls"
spec: {
	runtimeRef: "go"
	package: "golang.org/x/tools/gopls"
	version: "v0.16.0"
}
`,
			wantKind: resource.KindTool,
			wantName: "gopls",
		},
		{
			name: "Runtime download type",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Runtime"
metadata: name: "go"
spec: {
	type: "download"
	version: "1.25.5"
	source: {
		url: "https://go.dev/dl/go1.25.5.linux-amd64.tar.gz"
	}
	binaries: ["go", "gofmt"]
	toolBinPath: "~/go/bin"
	env: {
		GOROOT: "/opt/go/1.25.5"
	}
	commands: {
		install: ["go install {{.Package}}@{{.Version}}"]
	}
}
`,
			wantKind: resource.KindRuntime,
			wantName: "go",
		},
		{
			name: "Runtime delegation type",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Runtime"
metadata: name: "rust"
spec: {
	type: "delegation"
	version: "stable"
	toolBinPath: "~/.cargo/bin"
	bootstrap: {
		install: ["curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y"]
		check: ["rustup --version"]
	}
	binaries: ["rustc", "cargo"]
}
`,
			wantKind: resource.KindRuntime,
			wantName: "rust",
		},
		{
			name: "Installer download type",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "aqua"
spec: {
	type: "download"
}
`,
			wantKind: resource.KindInstaller,
			wantName: "aqua",
		},
		{
			name: "Installer delegation type",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "brew"
spec: {
	type: "delegation"
	commands: {
		install: ["brew install {{.Package}}"]
		remove: ["brew uninstall {{.Package}}"]
	}
}
`,
			wantKind: resource.KindInstaller,
			wantName: "brew",
		},
		{
			name: "InstallerRepository with git source",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "InstallerRepository"
metadata: name: "aqua-registry"
spec: {
	installerRef: "aqua"
	source: {
		type: "git"
		url: "https://github.com/aquaproj/aqua-registry.git"
	}
}
`,
			wantKind: resource.KindInstallerRepository,
			wantName: "aqua-registry",
		},
		{
			name: "ToolSet with installerRef",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "ToolSet"
metadata: name: "cli-tools"
spec: {
	installerRef: "aqua"
	tools: {
		fd:  { version: "9.0.0" }
		bat: { version: "0.24.0" }
	}
}
`,
			wantKind: resource.KindToolSet,
			wantName: "cli-tools",
		},
		{
			name: "Tool with commands pattern",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "claude"
spec: {
	commands: {
		install: ["curl -fsSL https://cli.claude.ai/install.sh | sh"]
	}
}
`,
			wantKind: resource.KindTool,
			wantName: "claude",
		},
		{
			name: "Tool with commands and version",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "mise"
spec: {
	commands: {
		install: ["curl -fsSL https://mise.run | sh"]
	}
	version: "2025.1.0"
}
`,
			wantKind: resource.KindTool,
			wantName: "mise",
		},
		{
			name: "Tool with full commands",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "custom-tool"
spec: {
	commands: {
		install: ["curl -fsSL https://example.com/install.sh | sh"]
		update: ["curl -fsSL https://example.com/update.sh | sh"]
		check: ["custom-tool --version"]
		remove: ["rm -f ~/.local/bin/custom-tool"]
		resolveVersion: ["custom-tool --version"]
	}
}
`,
			wantKind: resource.KindTool,
			wantName: "custom-tool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cueFile := filepath.Join(dir, "test.cue")
			require.NoError(t, os.WriteFile(cueFile, []byte(tt.content), 0644))

			loader := config.NewLoader(nil)
			resources, err := loader.LoadFile(cueFile)
			require.NoError(t, err)
			require.Len(t, resources, 1)
			assert.Equal(t, tt.wantKind, resources[0].Kind())
			assert.Equal(t, tt.wantName, resources[0].Name())
		})
	}
}

// TestSchemaValidation_CommandsToolFields verifies that the Commands field
// is correctly populated when loading a commands-pattern Tool from CUE.
func TestSchemaValidation_CommandsToolFields(t *testing.T) {
	tests := []struct {
		name               string
		content            string
		wantInstall        []string
		wantUpdate         []string
		wantCheck          []string
		wantRemove         []string
		wantResolveVersion []string
		wantVersion        string
	}{
		{
			name: "minimal commands (install only)",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "claude"
spec: {
	commands: {
		install: ["curl -fsSL https://cli.claude.ai/install.sh | sh"]
	}
}
`,
			wantInstall: []string{"curl -fsSL https://cli.claude.ai/install.sh | sh"},
		},
		{
			name: "commands with version",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "mise"
spec: {
	commands: {
		install: ["curl -fsSL https://mise.run | sh"]
	}
	version: "2025.1.0"
}
`,
			wantInstall: []string{"curl -fsSL https://mise.run | sh"},
			wantVersion: "2025.1.0",
		},
		{
			name: "full commands (all subfields)",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "custom-tool"
spec: {
	commands: {
		install: ["curl -fsSL https://example.com/install.sh | sh"]
		update: ["curl -fsSL https://example.com/update.sh | sh"]
		check: ["custom-tool --version"]
		remove: ["rm -f ~/.local/bin/custom-tool"]
		resolveVersion: ["custom-tool --version"]
	}
}
`,
			wantInstall:        []string{"curl -fsSL https://example.com/install.sh | sh"},
			wantUpdate:         []string{"curl -fsSL https://example.com/update.sh | sh"},
			wantCheck:          []string{"custom-tool --version"},
			wantRemove:         []string{"rm -f ~/.local/bin/custom-tool"},
			wantResolveVersion: []string{"custom-tool --version"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cueFile := filepath.Join(dir, "test.cue")
			require.NoError(t, os.WriteFile(cueFile, []byte(tt.content), 0644))

			loader := config.NewLoader(nil)
			resources, err := loader.LoadFile(cueFile)
			require.NoError(t, err)
			require.Len(t, resources, 1)

			tool, ok := resources[0].(*resource.Tool)
			require.True(t, ok, "expected *resource.Tool")

			require.NotNil(t, tool.ToolSpec.Commands, "Commands should be non-nil for commands-pattern tool")
			assert.Equal(t, tt.wantInstall, tool.ToolSpec.Commands.Install)
			assert.Equal(t, tt.wantUpdate, tool.ToolSpec.Commands.Update)
			assert.Equal(t, tt.wantCheck, tool.ToolSpec.Commands.Check)
			assert.Equal(t, tt.wantRemove, tool.ToolSpec.Commands.Remove)
			assert.Equal(t, tt.wantResolveVersion, tool.ToolSpec.Commands.ResolveVersion)
			assert.Equal(t, tt.wantVersion, tool.ToolSpec.Version)

			// Validate passes for commands-pattern tools
			require.NoError(t, tool.Spec().Validate())

			// Commands-pattern tools have no dependencies
			assert.Empty(t, tool.Spec().Dependencies())
		})
	}
}

// TestSchemaValidation_RejectsInvalid tests that Go-level Validate() correctly
// rejects invalid resource configurations that CUE schema allows (since CUE
// does not enforce all mutual exclusivity rules).
func TestSchemaValidation_RejectsInvalid(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantErrMsg  string
	}{
		{
			name: "Tool with commands and installerRef (mutual exclusivity)",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "bad-tool"
spec: {
	installerRef: "aqua"
	commands: {
		install: ["echo install"]
	}
}
`,
			wantErrMsg: "mutually exclusive",
		},
		{
			name: "Tool with commands and runtimeRef (mutual exclusivity)",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "bad-tool"
spec: {
	runtimeRef: "go"
	package: "example.com/tool"
	commands: {
		install: ["echo install"]
	}
}
`,
			wantErrMsg: "mutually exclusive",
		},
		{
			name: "Tool with commands but empty install",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "bad-tool"
spec: {
	commands: {
		check: ["echo ok"]
	}
}
`,
			wantErrMsg: "commands.install is required",
		},
		{
			name: "Tool with installerRef and runtimeRef (mutual exclusivity)",
			content: `
apiVersion: "tomei.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "bad-tool"
spec: {
	installerRef: "go"
	runtimeRef: "go"
	package: "example.com/tool"
	version: "v1.0.0"
}
`,
			wantErrMsg: "mutually exclusive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			cueFile := filepath.Join(dir, "test.cue")
			require.NoError(t, os.WriteFile(cueFile, []byte(tt.content), 0644))

			loader := config.NewLoader(nil)
			resources, err := loader.LoadFile(cueFile)
			// CUE loading may succeed (CUE schema is permissive for some combinations),
			// but Go Validate() must reject them.
			if err != nil {
				// If CUE rejects it, that's also acceptable
				return
			}
			require.Len(t, resources, 1)

			tool, ok := resources[0].(*resource.Tool)
			require.True(t, ok, "expected *resource.Tool")

			err = tool.Spec().Validate()
			require.Error(t, err, "Validate() should reject invalid tool spec")
			assert.Contains(t, err.Error(), tt.wantErrMsg)
		})
	}
}
