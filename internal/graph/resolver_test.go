package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func TestResolver_AddResource_Runtime(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
				Install: []string{"pnpm add -g {{.Package}}@{{.Version}}"},
			},
		},
	}

	resolver.AddResource(installer)

	assert.Equal(t, 2, resolver.NodeCount()) // installer + tool (auto-added)
	assert.Equal(t, 1, resolver.EdgeCount())
}

func TestResolver_AddResource_ToolWithInstallerRef(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
				Install: []string{"cargo binstall -y {{.Package}}{{if .Version}}@{{.Version}}{{end}}"},
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
	t.Parallel()
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
				Install: []string{"some-command"},
			},
		},
	}

	resolver.AddResource(toolA)
	resolver.AddResource(installerB)

	err := resolver.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestResolver_AddResource_InstallerWithDependsOn(t *testing.T) {
	t.Parallel()
	resolver := NewResolver()

	installer := &resource.Installer{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindInstaller,
			Metadata:     resource.Metadata{Name: "krew-installer"},
		},
		InstallerSpec: &resource.InstallerSpec{
			Type:      resource.InstallTypeDelegation,
			ToolRef:   "krew",
			DependsOn: []string{"kubectl"},
			Commands: &resource.CommandsSpec{
				Install: []string{"krew install {{.Package}}"},
			},
		},
	}

	resolver.AddResource(installer)

	assert.Equal(t, 3, resolver.NodeCount()) // installer + krew + kubectl (auto-added)
	assert.Equal(t, 2, resolver.EdgeCount()) // installer->krew, installer->kubectl
}

func TestResolver_Resolve_InstallerDependsOnOrdering(t *testing.T) {
	t.Parallel()
	resolver := NewResolver()

	// Setup: krew-installer depends on krew (toolRef) and kubectl (dependsOn)
	// ctx tool depends on krew-installer
	kubectl := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "kubectl"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "v1.32.0",
		},
	}

	krew := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "krew"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "v0.4.4",
		},
	}

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

	krewInstaller := &resource.Installer{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindInstaller,
			Metadata:     resource.Metadata{Name: "krew-installer"},
		},
		InstallerSpec: &resource.InstallerSpec{
			Type:      resource.InstallTypeDelegation,
			ToolRef:   "krew",
			DependsOn: []string{"kubectl"},
			Commands: &resource.CommandsSpec{
				Install: []string{"krew install {{.Package}}"},
			},
		},
	}

	ctx := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "ctx"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "krew-installer",
			Version:      "v0.9.5",
		},
	}

	// Add in random order
	resolver.AddResource(ctx)
	resolver.AddResource(krewInstaller)
	resolver.AddResource(kubectl)
	resolver.AddResource(krew)
	resolver.AddResource(aquaInstaller)

	layers, err := resolver.Resolve()
	require.NoError(t, err)

	// Build layer map
	nodeLayer := make(map[string]int)
	for layerIdx, layer := range layers {
		for _, node := range layer.Nodes {
			nodeLayer[node.Name] = layerIdx
		}
	}

	// kubectl and krew must be before krew-installer
	assert.Less(t, nodeLayer["kubectl"], nodeLayer["krew-installer"])
	assert.Less(t, nodeLayer["krew"], nodeLayer["krew-installer"])
	// krew-installer must be before ctx
	assert.Less(t, nodeLayer["krew-installer"], nodeLayer["ctx"])
}

func TestResolver_Resolve_ParallelTools(t *testing.T) {
	t.Parallel()
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
