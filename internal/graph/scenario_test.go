package graph

import (
	"fmt"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/resource"
)

// =============================================================================
// Complex Manifest Pattern Tests
// =============================================================================

// TestResolver_ComplexManifest_MultipleRuntimeChains tests a realistic scenario
// with multiple runtimes and their tool chains.
func TestResolver_ComplexManifest_MultipleRuntimeChains(t *testing.T) {
	resolver := NewResolver()

	// Go runtime chain
	goRuntime := createRuntime("go", resource.InstallTypeDownload)
	gopls := createToolWithRuntime("gopls", "go")
	golangciLint := createToolWithRuntime("golangci-lint", "go")
	goInstaller := createInstallerWithRuntime("go", "go")
	goimports := createToolWithInstaller("goimports", "go")

	// Rust runtime chain
	rustRuntime := createRuntime("rust", resource.InstallTypeDelegation)
	rustAnalyzer := createToolWithRuntime("rust-analyzer", "rust")
	cargoBinstall := createToolWithRuntime("cargo-binstall", "rust")
	binstallInstaller := createInstallerWithTool("binstall", "cargo-binstall")
	ripgrep := createToolWithInstaller("ripgrep", "binstall")
	fd := createToolWithInstaller("fd", "binstall")
	bat := createToolWithInstaller("bat", "binstall")

	// Download pattern tools (independent)
	aquaInstaller := createDownloadInstaller("aqua")
	jq := createToolWithInstaller("jq", "aqua")
	yq := createToolWithInstaller("yq", "aqua")

	// Add all resources
	resources := []resource.Resource{
		goRuntime, gopls, golangciLint, goInstaller, goimports,
		rustRuntime, rustAnalyzer, cargoBinstall, binstallInstaller, ripgrep, fd, bat,
		aquaInstaller, jq, yq,
	}
	for _, res := range resources {
		resolver.AddResource(res)
	}

	layers, err := resolver.Resolve()
	require.NoError(t, err)

	// Verify basic properties
	totalNodes := countTotalNodes(layers)
	assert.Equal(t, 15, totalNodes)

	// Verify dependency ordering
	executionOrder := flattenLayers(layers)
	assertDependencyOrder(t, executionOrder, "Runtime/go", "Tool/gopls")
	assertDependencyOrder(t, executionOrder, "Runtime/go", "Tool/golangci-lint")
	assertDependencyOrder(t, executionOrder, "Runtime/go", "Installer/go")
	assertDependencyOrder(t, executionOrder, "Installer/go", "Tool/goimports")

	assertDependencyOrder(t, executionOrder, "Runtime/rust", "Tool/rust-analyzer")
	assertDependencyOrder(t, executionOrder, "Runtime/rust", "Tool/cargo-binstall")
	assertDependencyOrder(t, executionOrder, "Tool/cargo-binstall", "Installer/binstall")
	assertDependencyOrder(t, executionOrder, "Installer/binstall", "Tool/ripgrep")
	assertDependencyOrder(t, executionOrder, "Installer/binstall", "Tool/fd")
	assertDependencyOrder(t, executionOrder, "Installer/binstall", "Tool/bat")

	assertDependencyOrder(t, executionOrder, "Installer/aqua", "Tool/jq")
	assertDependencyOrder(t, executionOrder, "Installer/aqua", "Tool/yq")

	// Verify parallel execution capability
	// Runtimes and aqua installer should be in early layers (can run in parallel)
	layer0IDs := layerNodeIDs(layers[0])
	// All three should be at layer 0 since they have no dependencies
	assert.Contains(t, layer0IDs, NodeID("Runtime/go"))
	assert.Contains(t, layer0IDs, NodeID("Runtime/rust"))
	assert.Contains(t, layer0IDs, NodeID("Installer/aqua"))
}

// TestResolver_ComplexManifest_DeepChain tests a deep dependency chain
// to ensure correct layer assignment.
func TestResolver_ComplexManifest_DeepChain(t *testing.T) {
	resolver := NewResolver()

	// Create a deep chain: Runtime -> Tool1 -> Installer1 -> Tool2 -> Installer2 -> Tool3
	rustRuntime := createRuntime("rust", resource.InstallTypeDelegation)
	tool1 := createToolWithRuntime("tool-1", "rust")
	installer1 := createInstallerWithTool("installer-1", "tool-1")
	tool2 := createToolWithInstaller("tool-2", "installer-1")
	installer2 := createInstallerWithTool("installer-2", "tool-2")
	tool3 := createToolWithInstaller("tool-3", "installer-2")

	resolver.AddResource(tool3)
	resolver.AddResource(installer2)
	resolver.AddResource(tool2)
	resolver.AddResource(installer1)
	resolver.AddResource(tool1)
	resolver.AddResource(rustRuntime)

	layers, err := resolver.Resolve()
	require.NoError(t, err)
	require.Len(t, layers, 6)

	// Each layer should have exactly one node
	for i, layer := range layers {
		assert.Len(t, layer.Nodes, 1, "Layer %d should have exactly 1 node", i)
	}

	// Verify order
	assert.Equal(t, "rust", layers[0].Nodes[0].Name)
	assert.Equal(t, "tool-1", layers[1].Nodes[0].Name)
	assert.Equal(t, "installer-1", layers[2].Nodes[0].Name)
	assert.Equal(t, "tool-2", layers[3].Nodes[0].Name)
	assert.Equal(t, "installer-2", layers[4].Nodes[0].Name)
	assert.Equal(t, "tool-3", layers[5].Nodes[0].Name)
}

// TestResolver_ComplexManifest_WideDependencies tests wide (fan-out) dependencies.
func TestResolver_ComplexManifest_WideDependencies(t *testing.T) {
	resolver := NewResolver()

	// One runtime with many direct tool dependencies
	goRuntime := createRuntime("go", resource.InstallTypeDownload)
	resolver.AddResource(goRuntime)

	numTools := 20
	for i := range numTools {
		tool := createToolWithRuntime(fmt.Sprintf("go-tool-%d", i), "go")
		resolver.AddResource(tool)
	}

	layers, err := resolver.Resolve()
	require.NoError(t, err)
	require.Len(t, layers, 2)

	// Layer 0: runtime only
	assert.Len(t, layers[0].Nodes, 1)
	assert.Equal(t, "go", layers[0].Nodes[0].Name)

	// Layer 1: all tools (parallelizable)
	assert.Len(t, layers[1].Nodes, numTools)
}

// TestResolver_ComplexManifest_DiamondDependency tests diamond dependency patterns.
func TestResolver_ComplexManifest_DiamondDependency(t *testing.T) {
	resolver := NewResolver()

	// Diamond pattern:
	//       Runtime(go)
	//        /       \
	//   Tool(a)    Tool(b)
	//        \       /
	//     Installer(combined)
	//           |
	//        Tool(final)

	goRuntime := createRuntime("go", resource.InstallTypeDownload)
	toolA := createToolWithRuntime("tool-a", "go")
	toolB := createToolWithRuntime("tool-b", "go")

	// Combined installer depends on both tools (using custom creation)
	combinedInstaller := &resource.Installer{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindInstaller,
			Metadata:     resource.Metadata{Name: "combined"},
		},
		InstallerSpec: &resource.InstallerSpec{
			Type:    resource.InstallTypeDelegation,
			ToolRef: "tool-a", // Can only have one ToolRef, but we simulate diamond via multiple tools
			Commands: &resource.CommandsSpec{
				Install: "combined-install",
			},
		},
	}

	finalTool := createToolWithInstaller("final-tool", "combined")

	resolver.AddResource(finalTool)
	resolver.AddResource(combinedInstaller)
	resolver.AddResource(toolA)
	resolver.AddResource(toolB)
	resolver.AddResource(goRuntime)

	layers, err := resolver.Resolve()
	require.NoError(t, err)

	// Verify ordering
	executionOrder := flattenLayers(layers)
	assertDependencyOrder(t, executionOrder, "Runtime/go", "Tool/tool-a")
	assertDependencyOrder(t, executionOrder, "Runtime/go", "Tool/tool-b")
	assertDependencyOrder(t, executionOrder, "Tool/tool-a", "Installer/combined")
	assertDependencyOrder(t, executionOrder, "Installer/combined", "Tool/final-tool")

	// tool-a and tool-b should be in the same layer (parallel)
	for _, layer := range layers {
		ids := layerNodeIDs(layer)
		if containsNodeID(ids, "Tool/tool-a") {
			assert.Contains(t, ids, NodeID("Tool/tool-b"),
				"tool-a and tool-b should be in the same layer")
			break
		}
	}
}

// =============================================================================
// Cycle Detection Tests
// =============================================================================

// TestResolver_CycleDetection_SelfReference tests self-referential dependency.
func TestResolver_CycleDetection_SelfReference(t *testing.T) {
	// Note: This would be caught at CUE validation level,
	// but we test the graph layer anyway.
	d := newDAG()
	node := d.addNode(resource.KindTool, "self-ref")
	d.addEdge(node, node)

	cycle := d.detectCycle()
	require.NotNil(t, cycle)
}

// TestResolver_CycleDetection_TwoNodeCycle tests A -> B -> A cycle.
func TestResolver_CycleDetection_TwoNodeCycle(t *testing.T) {
	resolver := NewResolver()

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
				Install: "install-b",
			},
		},
	}

	resolver.AddResource(toolA)
	resolver.AddResource(installerB)

	err := resolver.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")

	// Also test that Resolve() fails
	_, err = resolver.Resolve()
	require.Error(t, err)
}

// TestResolver_CycleDetection_ThreeNodeCycle tests A -> B -> C -> A cycle.
func TestResolver_CycleDetection_ThreeNodeCycle(t *testing.T) {
	resolver := NewResolver()

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
			ToolRef: "tool-c",
			Commands: &resource.CommandsSpec{
				Install: "install-b",
			},
		},
	}

	toolC := &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: "tool-c"},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "installer-a",
			Version:      "1.0.0",
		},
	}

	installerA := &resource.Installer{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindInstaller,
			Metadata:     resource.Metadata{Name: "installer-a"},
		},
		InstallerSpec: &resource.InstallerSpec{
			Type:    resource.InstallTypeDelegation,
			ToolRef: "tool-a",
			Commands: &resource.CommandsSpec{
				Install: "install-a",
			},
		},
	}

	resolver.AddResource(toolA)
	resolver.AddResource(installerB)
	resolver.AddResource(toolC)
	resolver.AddResource(installerA)

	err := resolver.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular dependency")
}

// TestResolver_CycleDetection_LongCycle tests a longer cycle (5 nodes).
func TestResolver_CycleDetection_LongCycle(t *testing.T) {
	d := newDAG()

	// Create a cycle: 1 -> 2 -> 3 -> 4 -> 5 -> 1
	nodes := make([]*Node, 5)
	for i := range 5 {
		nodes[i] = d.addNode(resource.KindTool, fmt.Sprintf("tool-%d", i))
	}

	for i := range 5 {
		next := (i + 1) % 5
		d.addEdge(nodes[i], nodes[next])
	}

	cycle := d.detectCycle()
	require.NotNil(t, cycle)
	assert.GreaterOrEqual(t, len(cycle), 5)
}

// TestResolver_CycleDetection_CycleInSubgraph tests cycle detection in a subgraph.
func TestResolver_CycleDetection_CycleInSubgraph(t *testing.T) {
	d := newDAG()

	// Independent chain: A -> B -> C
	a := d.addNode(resource.KindRuntime, "a")
	b := d.addNode(resource.KindInstaller, "b")
	c := d.addNode(resource.KindTool, "c")
	d.addEdge(b, a)
	d.addEdge(c, b)

	// Separate cycle: X -> Y -> X
	x := d.addNode(resource.KindTool, "x")
	y := d.addNode(resource.KindInstaller, "y")
	d.addEdge(x, y)
	d.addEdge(y, x)

	cycle := d.detectCycle()
	require.NotNil(t, cycle)

	// Verify the cycle is detected in the X-Y subgraph
	cycleIDs := make([]string, len(cycle))
	for i, id := range cycle {
		cycleIDs[i] = id.String()
	}
	// The cycle should contain either Tool/x or Installer/y
	hasX := false
	hasY := false
	for _, id := range cycleIDs {
		if id == "Tool/x" {
			hasX = true
		}
		if id == "Installer/y" {
			hasY = true
		}
	}
	assert.True(t, hasX || hasY, "Cycle should be detected in X-Y subgraph")
}

// =============================================================================
// Edge Case Tests
// =============================================================================

// TestResolver_EdgeCase_EmptyManifest tests empty manifest handling.
func TestResolver_EdgeCase_EmptyManifest(t *testing.T) {
	resolver := NewResolver()

	layers, err := resolver.Resolve()
	require.NoError(t, err)
	assert.Empty(t, layers)
}

// TestResolver_EdgeCase_SingleNode tests single node handling.
func TestResolver_EdgeCase_SingleNode(t *testing.T) {
	resolver := NewResolver()
	resolver.AddResource(createRuntime("go", resource.InstallTypeDownload))

	layers, err := resolver.Resolve()
	require.NoError(t, err)
	require.Len(t, layers, 1)
	assert.Len(t, layers[0].Nodes, 1)
}

// TestResolver_EdgeCase_DisconnectedComponents tests multiple disconnected components.
func TestResolver_EdgeCase_DisconnectedComponents(t *testing.T) {
	resolver := NewResolver()

	// Component 1: Go chain
	goRuntime := createRuntime("go", resource.InstallTypeDownload)
	gopls := createToolWithRuntime("gopls", "go")

	// Component 2: Rust chain
	rustRuntime := createRuntime("rust", resource.InstallTypeDelegation)
	rustAnalyzer := createToolWithRuntime("rust-analyzer", "rust")

	// Component 3: Standalone installer
	aquaInstaller := createDownloadInstaller("aqua")

	resolver.AddResource(goRuntime)
	resolver.AddResource(gopls)
	resolver.AddResource(rustRuntime)
	resolver.AddResource(rustAnalyzer)
	resolver.AddResource(aquaInstaller)

	layers, err := resolver.Resolve()
	require.NoError(t, err)

	// All roots should be in layer 0
	layer0IDs := layerNodeIDs(layers[0])
	assert.Contains(t, layer0IDs, NodeID("Runtime/go"))
	assert.Contains(t, layer0IDs, NodeID("Runtime/rust"))
	assert.Contains(t, layer0IDs, NodeID("Installer/aqua"))

	// Verify total nodes
	totalNodes := countTotalNodes(layers)
	assert.Equal(t, 5, totalNodes)
}

// TestResolver_EdgeCase_DuplicateResources tests duplicate resource handling.
func TestResolver_EdgeCase_DuplicateResources(t *testing.T) {
	resolver := NewResolver()

	runtime := createRuntime("go", resource.InstallTypeDownload)
	resolver.AddResource(runtime)
	resolver.AddResource(runtime) // Add same resource again

	layers, err := resolver.Resolve()
	require.NoError(t, err)
	assert.Equal(t, 1, countTotalNodes(layers))
}

// =============================================================================
// Stress Tests
// =============================================================================

// TestResolver_Stress_LargeGraph tests performance with large graphs.
func TestResolver_Stress_LargeGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	resolver := NewResolver()

	// Create a large acyclic graph
	numRuntimes := 5
	numToolsPerRuntime := 100

	for i := range numRuntimes {
		runtime := createRuntime(fmt.Sprintf("runtime-%d", i), resource.InstallTypeDownload)
		resolver.AddResource(runtime)

		for j := range numToolsPerRuntime {
			tool := createToolWithRuntime(
				fmt.Sprintf("tool-%d-%d", i, j),
				fmt.Sprintf("runtime-%d", i),
			)
			resolver.AddResource(tool)
		}
	}

	layers, err := resolver.Resolve()
	require.NoError(t, err)

	// All runtimes in layer 0
	assert.Len(t, layers[0].Nodes, numRuntimes)

	// All tools in layer 1
	assert.Len(t, layers[1].Nodes, numRuntimes*numToolsPerRuntime)
}

// TestResolver_Stress_DeepGraph tests performance with deep dependency chains.
func TestResolver_Stress_DeepGraph(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	resolver := NewResolver()

	// Create a deep chain
	depth := 100
	runtime := createRuntime("base", resource.InstallTypeDownload)
	resolver.AddResource(runtime)

	prevName := "base"
	prevKind := "Runtime"
	for i := range depth {
		if i%2 == 0 {
			// Tool
			name := fmt.Sprintf("tool-%d", i)
			var tool *resource.Tool
			if prevKind == "Runtime" {
				tool = createToolWithRuntime(name, prevName)
			} else {
				tool = createToolWithInstaller(name, prevName)
			}
			resolver.AddResource(tool)
			prevName = name
			prevKind = "Tool"
		} else {
			// Installer
			name := fmt.Sprintf("installer-%d", i)
			installer := createInstallerWithTool(name, prevName)
			resolver.AddResource(installer)
			prevName = name
			prevKind = "Installer"
		}
	}

	layers, err := resolver.Resolve()
	require.NoError(t, err)
	assert.Len(t, layers, depth+1)
}

// =============================================================================
// Determinism Tests
// =============================================================================

// TestResolver_Determinism_SameOutput verifies that the resolver produces
// deterministic output for the same input.
func TestResolver_Determinism_SameOutput(t *testing.T) {
	for range 10 {
		resolver1 := NewResolver()
		resolver2 := NewResolver()

		// Add resources in different orders
		resources := []resource.Resource{
			createRuntime("go", resource.InstallTypeDownload),
			createRuntime("rust", resource.InstallTypeDelegation),
			createToolWithRuntime("gopls", "go"),
			createToolWithRuntime("rust-analyzer", "rust"),
			createDownloadInstaller("aqua"),
			createToolWithInstaller("ripgrep", "aqua"),
		}

		// Forward order
		for _, r := range resources {
			resolver1.AddResource(r)
		}

		// Reverse order
		for j := len(resources) - 1; j >= 0; j-- {
			resolver2.AddResource(resources[j])
		}

		layers1, err1 := resolver1.Resolve()
		layers2, err2 := resolver2.Resolve()

		require.NoError(t, err1)
		require.NoError(t, err2)

		// Same number of layers
		require.Len(t, layers2, len(layers1))

		// Same nodes in each layer (order within layer may differ)
		for layerIdx := range layers1 {
			ids1 := layerNodeIDs(layers1[layerIdx])
			ids2 := layerNodeIDs(layers2[layerIdx])

			slices.Sort(ids1)
			slices.Sort(ids2)

			assert.ElementsMatch(t, ids1, ids2,
				"Layer %d should have same nodes regardless of input order", layerIdx)
		}
	}
}

// =============================================================================
// Helper Functions
// =============================================================================

func createRuntime(name string, pattern resource.InstallType) *resource.Runtime {
	return &resource.Runtime{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindRuntime,
			Metadata:     resource.Metadata{Name: name},
		},
		RuntimeSpec: &resource.RuntimeSpec{
			Type:        pattern,
			Version:     "1.0.0",
			ToolBinPath: "~/bin",
		},
	}
}

func createToolWithRuntime(name, runtimeRef string) *resource.Tool {
	return &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: name},
		},
		ToolSpec: &resource.ToolSpec{
			RuntimeRef: runtimeRef,
			Version:    "1.0.0",
		},
	}
}

func createToolWithInstaller(name, installerRef string) *resource.Tool {
	return &resource.Tool{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindTool,
			Metadata:     resource.Metadata{Name: name},
		},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: installerRef,
			Version:      "1.0.0",
		},
	}
}

func createInstallerWithRuntime(name, runtimeRef string) *resource.Installer {
	return &resource.Installer{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindInstaller,
			Metadata:     resource.Metadata{Name: name},
		},
		InstallerSpec: &resource.InstallerSpec{
			Type:       resource.InstallTypeDelegation,
			RuntimeRef: runtimeRef,
			Commands: &resource.CommandsSpec{
				Install: fmt.Sprintf("%s install", name),
			},
		},
	}
}

func createInstallerWithTool(name, toolRef string) *resource.Installer {
	return &resource.Installer{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindInstaller,
			Metadata:     resource.Metadata{Name: name},
		},
		InstallerSpec: &resource.InstallerSpec{
			Type:    resource.InstallTypeDelegation,
			ToolRef: toolRef,
			Commands: &resource.CommandsSpec{
				Install: fmt.Sprintf("%s install", name),
			},
		},
	}
}

func createDownloadInstaller(name string) *resource.Installer {
	return &resource.Installer{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindInstaller,
			Metadata:     resource.Metadata{Name: name},
		},
		InstallerSpec: &resource.InstallerSpec{
			Type: resource.InstallTypeDownload,
		},
	}
}

func countTotalNodes(layers []Layer) int {
	total := 0
	for _, layer := range layers {
		total += len(layer.Nodes)
	}
	return total
}

func flattenLayers(layers []Layer) []NodeID {
	result := make([]NodeID, 0)
	for _, layer := range layers {
		for _, node := range layer.Nodes {
			result = append(result, node.ID)
		}
	}
	return result
}

func layerNodeIDs(layer Layer) []NodeID {
	ids := make([]NodeID, len(layer.Nodes))
	for i, node := range layer.Nodes {
		ids[i] = node.ID
	}
	return ids
}

func containsNodeID(ids []NodeID, target string) bool {
	for _, id := range ids {
		if id.String() == target {
			return true
		}
	}
	return false
}

func assertDependencyOrder(t *testing.T, executionOrder []NodeID, beforeID, afterID string) {
	t.Helper()
	beforeIdx := -1
	afterIdx := -1
	for i, id := range executionOrder {
		if id.String() == beforeID {
			beforeIdx = i
		}
		if id.String() == afterID {
			afterIdx = i
		}
	}
	require.NotEqual(t, -1, beforeIdx, "Node %s not found in execution order", beforeID)
	require.NotEqual(t, -1, afterIdx, "Node %s not found in execution order", afterID)
	assert.Less(t, beforeIdx, afterIdx, "%s should be executed before %s", beforeID, afterID)
}
