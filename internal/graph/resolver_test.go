package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func TestResolver_AddResource_Runtime(t *testing.T) {
	resolver := NewResolver()

	runtime := &resource.Runtime{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindRuntime,
			Metadata:     resource.Metadata{Name: "go"},
		},
		RuntimeSpec: &resource.RuntimeSpec{
			Version: "1.23.0",
		},
	}

	resolver.AddResource(runtime)

	assert.Equal(t, 1, resolver.NodeCount())
	assert.Equal(t, 0, resolver.EdgeCount())
}

func TestResolver_AddResource_ToolWithRuntimeRef(t *testing.T) {
	resolver := NewResolver()

	tool := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "gopls"},
		},
		ToolSpec: &resource.ToolSpec{
			RuntimeRef: "go",
			Package:    &resource.Package{Name: "golang.org/x/tools/gopls"},
			Version:    "v0.17.0",
		},
	}

	resolver.AddResource(tool)

	assert.Equal(t, 2, resolver.NodeCount()) // tool + runtime (auto-added)
	assert.Equal(t, 1, resolver.EdgeCount())
}

func TestResolver_AddResource_InstallerWithToolRef(t *testing.T) {
	resolver := NewResolver()

	installer := &resource.Installer{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindInstaller,
			Metadata:     resource.Metadata{Name: "pnpm"},
		},
		InstallerSpec: &resource.InstallerSpec{
			Type:    resource.InstallTypeDelegation,
			ToolRef: "pnpm",
			Commands: &resource.CommandsSpec{
				Install: "pnpm add -g {{.Package}}@{{.Version}}",
			},
		},
	}

	resolver.AddResource(installer)

	assert.Equal(t, 2, resolver.NodeCount()) // installer + tool (auto-added)
	assert.Equal(t, 1, resolver.EdgeCount())
}

func TestResolver_AddResource_ToolWithInstallerRef(t *testing.T) {
	resolver := NewResolver()

	tool := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "ripgrep"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "14.0.0",
		},
	}

	resolver.AddResource(tool)

	assert.Equal(t, 2, resolver.NodeCount()) // tool + installer (auto-added)
	assert.Equal(t, 1, resolver.EdgeCount())
}

func TestResolver_Resolve_ToolChain(t *testing.T) {
	resolver := NewResolver()

	// Build: Runtime(rust) <- Tool(cargo-binstall) <- Installer(binstall) <- Tool(ripgrep)
	// This reflects the new design where Tool can directly reference Runtime
	rustRuntime := &resource.Runtime{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindRuntime,
			Metadata:     resource.Metadata{Name: "rust"},
		},
		RuntimeSpec: &resource.RuntimeSpec{
			Type:        resource.InstallTypeDelegation,
			Version:     "stable",
			ToolBinPath: "~/.cargo/bin",
		},
	}

	cargoBinstallTool := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "cargo-binstall"},
		},
		ToolSpec: &resource.ToolSpec{
			RuntimeRef: "rust", // Tool directly references Runtime
			Package:    &resource.Package{Name: "cargo-binstall"},
			Version:    "1.6.4",
		},
	}

	binstallInstaller := &resource.Installer{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindInstaller,
			Metadata:     resource.Metadata{Name: "binstall"},
		},
		InstallerSpec: &resource.InstallerSpec{
			Type:    resource.InstallTypeDelegation,
			ToolRef: "cargo-binstall", // Installer depends on Tool
			Commands: &resource.CommandsSpec{
				Install: "cargo binstall -y {{.Package}}{{if .Version}}@{{.Version}}{{end}}",
			},
		},
	}

	ripgrepTool := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "ripgrep"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "binstall",
			Package:      &resource.Package{Name: "ripgrep"},
			Version:      "14.1.0",
		},
	}

	// Add in random order to ensure sorting works
	resolver.AddResource(ripgrepTool)
	resolver.AddResource(rustRuntime)
	resolver.AddResource(binstallInstaller)
	resolver.AddResource(cargoBinstallTool)

	layers, err := resolver.Resolve()
	require.NoError(t, err)
	require.Len(t, layers, 4)

	// Verify execution order
	assert.Equal(t, resource.KindRuntime, layers[0].Nodes[0].Kind)
	assert.Equal(t, "rust", layers[0].Nodes[0].Name)

	assert.Equal(t, resource.KindTool, layers[1].Nodes[0].Kind)
	assert.Equal(t, "cargo-binstall", layers[1].Nodes[0].Name)

	assert.Equal(t, resource.KindInstaller, layers[2].Nodes[0].Kind)
	assert.Equal(t, "binstall", layers[2].Nodes[0].Name)

	assert.Equal(t, resource.KindTool, layers[3].Nodes[0].Kind)
	assert.Equal(t, "ripgrep", layers[3].Nodes[0].Name)
}

func TestResolver_Validate_CircularDependency(t *testing.T) {
	resolver := NewResolver()

	// Create circular dependency: tool A depends on installer B, installer B depends on tool A
	toolA := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "tool-a"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "installer-b",
			Version:      "1.0.0",
		},
	}

	installerB := &resource.Installer{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindInstaller,
			Metadata:     resource.Metadata{Name: "installer-b"},
		},
		InstallerSpec: &resource.InstallerSpec{
			Type:    resource.InstallTypeDelegation,
			ToolRef: "tool-a",
			Commands: &resource.CommandsSpec{
				Install: "some-command",
			},
		},
	}

	resolver.AddResource(toolA)
	resolver.AddResource(installerB)

	err := resolver.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestResolver_Resolve_ParallelTools(t *testing.T) {
	resolver := NewResolver()

	aquaInstaller := &resource.Installer{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindInstaller,
			Metadata:     resource.Metadata{Name: "aqua"},
		},
		InstallerSpec: &resource.InstallerSpec{
			Type: resource.InstallTypeDownload,
		},
	}

	ripgrep := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "ripgrep"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "14.0.0",
		},
	}

	fd := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "fd"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "9.0.0",
		},
	}

	bat := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "bat"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "0.24.0",
		},
	}

	resolver.AddResource(ripgrep)
	resolver.AddResource(fd)
	resolver.AddResource(bat)
	resolver.AddResource(aquaInstaller)

	layers, err := resolver.Resolve()
	require.NoError(t, err)
	require.Len(t, layers, 2)

	// Layer 0: aqua installer
	assert.Len(t, layers[0].Nodes, 1)
	assert.Equal(t, "aqua", layers[0].Nodes[0].Name)

	// Layer 1: all tools (can be executed in parallel)
	assert.Len(t, layers[1].Nodes, 3)

	toolNames := make([]string, 0, 3)
	for _, node := range layers[1].Nodes {
		toolNames = append(toolNames, node.Name)
	}
	assert.Contains(t, toolNames, "ripgrep")
	assert.Contains(t, toolNames, "fd")
	assert.Contains(t, toolNames, "bat")
}
