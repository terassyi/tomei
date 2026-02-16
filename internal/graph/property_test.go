// Package graph property_test.go
//
// # Property-Based Tests for DAG Dependency Resolution
//
// This file contains property-based tests using the rapid library to verify
// invariants of the dependency resolution system with randomly generated
// manifests.
//
// # File Structure
//
//	property_test.go
//	├── testResolver wrapper      - Provides access to internal DAG for assertions
//	├── Manifest Generators       - Generate random but realistic resources
//	│   ├── manifestGenerator()       - General manifests (Runtime/Installer/Tool)
//	│   ├── toolChainGenerator()      - Tool-as-installer chains
//	│   └── cyclicManifestGenerator() - Manifests that may contain cycles
//	├── Property Tests            - Verify invariants that must always hold
//	│   ├── TopologicalOrder      - Dependencies before dependents
//	│   ├── AllNodesIncluded      - Every resource appears exactly once
//	│   ├── LayerParallelism      - No dependencies within same layer
//	│   ├── ToolChain_ExecutionOrder - Correct order for tool chains
//	│   ├── CycleDetection        - Validate() and Resolve() consistency
//	│   ├── LayerCount            - Layer count bounds
//	│   ├── RuntimesFirst         - Independent runtimes in layer 0
//	│   └── KindOrderWithinLayer  - Runtime -> Installer -> Tool within each layer
//	├── Known Structure Tests     - Specific patterns with expected results
//	└── Helper Functions          - Create actual resource.* structs
//
// # How It Works
//
//  1. Generators create actual resource.Runtime, resource.Installer, and
//     resource.Tool structs with randomized dependencies.
//
//  2. Resources are added via AddResource(), which internally calls
//     Spec().Dependencies() to extract dependency references.
//
//  3. Property tests verify invariants that must hold for ANY valid manifest,
//     regardless of the specific structure.
//
// # Generator Details
//
//	| Generator              | Output                        | Cycles |
//	|------------------------|-------------------------------|--------|
//	| manifestGenerator()    | 0-3 Runtime, 0-5 Installer,   | No     |
//	|                        | 1-10 Tool with random deps    |        |
//	| toolChainGenerator()   | Runtime→Tool→Installer→Tool   | No     |
//	|                        | chains (tool-as-installer)    |        |
//	| cyclicManifestGenerator| Tool↔Installer mutual refs    | Maybe  |
//
// # Tested Invariants
//
//   - Topological Order: For every edge A→B, B appears in an earlier layer than A
//   - Completeness: All nodes appear exactly once in the output layers
//   - Parallelism Safety: Nodes in the same layer have no edges between them
//   - Cycle Consistency: Validate() and Resolve() agree on cycle detection
//   - Layer Bounds: 1 <= len(layers) <= len(nodes) for non-empty graphs
//   - Runtime Priority: Runtimes with no dependencies are always in layer 0
package graph

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
	"pgregory.net/rapid"
)

// =============================================================================
// Property-Based Tests using rapid
// =============================================================================

// testResolver wraps the resolver interface with access to internal DAG for testing.
type testResolver struct {
	Resolver
	dag *dag
}

// newTestResolver creates a resolver with test access to internal DAG.
func newTestResolver() *testResolver {
	r := &resolver{dag: newDAG()}
	return &testResolver{
		Resolver: r,
		dag:      r.dag,
	}
}

// =============================================================================
// Manifest Generators - Generate actual Tool/Installer/Runtime resources
// =============================================================================

// manifestGenerator generates a random but valid manifest structure.
// It creates actual resources with proper dependencies via Spec.Dependencies().
func manifestGenerator() *rapid.Generator[*testResolver] {
	return rapid.Custom(func(t *rapid.T) *testResolver {
		tr := newTestResolver()

		// Generate runtimes (0-3)
		numRuntimes := rapid.IntRange(0, 3).Draw(t, "numRuntimes")
		runtimeNames := make([]string, numRuntimes)
		for i := range numRuntimes {
			name := fmt.Sprintf("runtime-%d", i)
			runtimeNames[i] = name
			pattern := resource.InstallTypeDownload
			if rapid.Bool().Draw(t, fmt.Sprintf("runtime_%d_delegation", i)) {
				pattern = resource.InstallTypeDelegation
			}
			tr.AddResource(createRuntimeWithPattern(name, pattern))
		}

		// Generate installers (0-5)
		numInstallers := rapid.IntRange(0, 5).Draw(t, "numInstallers")
		installerNames := make([]string, numInstallers)
		for i := range numInstallers {
			name := fmt.Sprintf("installer-%d", i)
			installerNames[i] = name

			// Decide dependency type: none, runtimeRef, or toolRef
			depType := rapid.IntRange(0, 2).Draw(t, fmt.Sprintf("installer_%d_depType", i))
			switch depType {
			case 0:
				// Download pattern - no dependencies
				tr.AddResource(createDownloadInstaller(name))
			case 1:
				// Delegation with runtimeRef
				if len(runtimeNames) > 0 {
					runtimeIdx := rapid.IntRange(0, len(runtimeNames)-1).Draw(t, fmt.Sprintf("installer_%d_runtimeIdx", i))
					tr.AddResource(createInstallerWithRuntime(name, runtimeNames[runtimeIdx]))
				} else {
					tr.AddResource(createDownloadInstaller(name))
				}
			case 2:
				// Delegation with toolRef - will be linked later if tools exist
				// For now, create as download to avoid forward reference
				tr.AddResource(createDownloadInstaller(name))
			}
		}

		// Generate tools (1-10)
		numTools := rapid.IntRange(1, 10).Draw(t, "numTools")
		for i := range numTools {
			name := fmt.Sprintf("tool-%d", i)

			// Decide dependency type: installerRef or runtimeRef
			useRuntime := rapid.Bool().Draw(t, fmt.Sprintf("tool_%d_useRuntime", i))
			if useRuntime && len(runtimeNames) > 0 {
				runtimeIdx := rapid.IntRange(0, len(runtimeNames)-1).Draw(t, fmt.Sprintf("tool_%d_runtimeIdx", i))
				tr.AddResource(createToolWithRuntime(name, runtimeNames[runtimeIdx]))
			} else if len(installerNames) > 0 {
				installerIdx := rapid.IntRange(0, len(installerNames)-1).Draw(t, fmt.Sprintf("tool_%d_installerIdx", i))
				tr.AddResource(createToolWithInstaller(name, installerNames[installerIdx]))
			} else if len(runtimeNames) > 0 {
				runtimeIdx := rapid.IntRange(0, len(runtimeNames)-1).Draw(t, fmt.Sprintf("tool_%d_fallbackRuntimeIdx", i))
				tr.AddResource(createToolWithRuntime(name, runtimeNames[runtimeIdx]))
			} else {
				// No runtime or installer - create a download installer for this tool
				installerName := fmt.Sprintf("auto-installer-%d", i)
				tr.AddResource(createDownloadInstaller(installerName))
				tr.AddResource(createToolWithInstaller(name, installerName))
			}
		}

		return tr
	})
}

// toolChainGenerator generates realistic tool chains like:
// Runtime -> Tool -> Installer -> Tool (tool-as-installer pattern)
func toolChainGenerator() *rapid.Generator[*testResolver] {
	return rapid.Custom(func(t *rapid.T) *testResolver {
		tr := newTestResolver()

		// Always start with a runtime
		runtimeName := "base-runtime"
		tr.AddResource(createRuntimeWithPattern(runtimeName, resource.InstallTypeDownload))

		// Generate a chain of alternating tools and installers
		chainLength := rapid.IntRange(1, 5).Draw(t, "chainLength")
		prevResourceType := "runtime"
		prevResourceName := runtimeName

		for i := range chainLength {
			if prevResourceType == "runtime" || prevResourceType == "installer" {
				// Add a tool
				toolName := fmt.Sprintf("tool-%d", i)
				if prevResourceType == "runtime" {
					tr.AddResource(createToolWithRuntime(toolName, prevResourceName))
				} else {
					tr.AddResource(createToolWithInstaller(toolName, prevResourceName))
				}
				prevResourceType = "tool"
				prevResourceName = toolName
			} else {
				// Add an installer that depends on the previous tool
				installerName := fmt.Sprintf("installer-%d", i)
				tr.AddResource(createInstallerWithTool(installerName, prevResourceName))
				prevResourceType = "installer"
				prevResourceName = installerName
			}
		}

		// Add some leaf tools that depend on the last installer
		if prevResourceType == "installer" {
			numLeafTools := rapid.IntRange(1, 5).Draw(t, "numLeafTools")
			for i := range numLeafTools {
				toolName := fmt.Sprintf("leaf-tool-%d", i)
				tr.AddResource(createToolWithInstaller(toolName, prevResourceName))
			}
		}

		return tr
	})
}

// cyclicManifestGenerator generates manifests that may contain cycles.
// Used to test cycle detection with actual resource structures.
func cyclicManifestGenerator() *rapid.Generator[*testResolver] {
	return rapid.Custom(func(t *rapid.T) *testResolver {
		tr := newTestResolver()

		// Generate tools and installers with potential circular dependencies
		numResources := rapid.IntRange(2, 6).Draw(t, "numResources")

		toolNames := make([]string, 0)
		installerNames := make([]string, 0)

		for i := range numResources {
			isInstaller := rapid.Bool().Draw(t, fmt.Sprintf("isInstaller_%d", i))
			if isInstaller {
				name := fmt.Sprintf("installer-%d", i)
				installerNames = append(installerNames, name)

				// May reference a tool (potentially creating a cycle)
				if len(toolNames) > 0 && rapid.Bool().Draw(t, fmt.Sprintf("installer_%d_hasToolRef", i)) {
					toolIdx := rapid.IntRange(0, len(toolNames)-1).Draw(t, fmt.Sprintf("installer_%d_toolIdx", i))
					tr.AddResource(createInstallerWithTool(name, toolNames[toolIdx]))
				} else {
					tr.AddResource(createDownloadInstaller(name))
				}
			} else {
				name := fmt.Sprintf("tool-%d", i)
				toolNames = append(toolNames, name)

				// May reference an installer (potentially creating a cycle)
				if len(installerNames) > 0 {
					installerIdx := rapid.IntRange(0, len(installerNames)-1).Draw(t, fmt.Sprintf("tool_%d_installerIdx", i))
					tr.AddResource(createToolWithInstaller(name, installerNames[installerIdx]))
				} else {
					// Create a temporary installer
					tempInstaller := fmt.Sprintf("temp-installer-%d", i)
					tr.AddResource(createDownloadInstaller(tempInstaller))
					tr.AddResource(createToolWithInstaller(name, tempInstaller))
				}
			}
		}

		return tr
	})
}

// =============================================================================
// Property Tests with Actual Manifests
// =============================================================================

// TestProperty_Manifest_TopologicalOrder verifies that dependencies are always
// resolved before their dependents when using actual manifest structures.
func TestProperty_Manifest_TopologicalOrder(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tr := manifestGenerator().Draw(t, "manifest")

		layers, err := tr.Resolve()
		require.NoError(t, err)

		// Build node -> layer index map
		nodeLayer := make(map[NodeID]int)
		for layerIdx, layer := range layers {
			for _, node := range layer.Nodes {
				nodeLayer[node.ID] = layerIdx
			}
		}

		// Verify: all dependencies appear in earlier layers
		for _, layer := range layers {
			for _, node := range layer.Nodes {
				deps := tr.dag.edges[node.ID]
				for dep := range deps {
					depLayer, ok := nodeLayer[dep]
					if !ok {
						t.Fatalf("Dependency %s not found in layers", dep)
					}
					if depLayer >= nodeLayer[node.ID] {
						t.Fatalf("Dependency %s (layer %d) should be before %s (layer %d)",
							dep, depLayer, node.ID, nodeLayer[node.ID])
					}
				}
			}
		}
	})
}

// TestProperty_Manifest_AllNodesIncluded verifies that all resources appear
// exactly once in the execution plan.
func TestProperty_Manifest_AllNodesIncluded(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tr := manifestGenerator().Draw(t, "manifest")

		layers, err := tr.Resolve()
		require.NoError(t, err)

		// Count nodes in layers
		seenNodes := make(map[NodeID]int)
		for _, layer := range layers {
			for _, node := range layer.Nodes {
				seenNodes[node.ID]++
			}
		}

		// Verify each node appears exactly once
		for nodeID := range tr.dag.nodes {
			count, ok := seenNodes[nodeID]
			if !ok {
				t.Fatalf("Node %s not found in layers", nodeID)
			}
			if count != 1 {
				t.Fatalf("Node %s appears %d times (expected 1)", nodeID, count)
			}
		}
	})
}

// TestProperty_Manifest_LayerParallelism verifies that resources in the same
// layer have no dependencies between them (safe for parallel execution).
func TestProperty_Manifest_LayerParallelism(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tr := manifestGenerator().Draw(t, "manifest")

		layers, err := tr.Resolve()
		require.NoError(t, err)

		for layerIdx, layer := range layers {
			layerNodeSet := make(map[NodeID]bool)
			for _, node := range layer.Nodes {
				layerNodeSet[node.ID] = true
			}

			for _, node := range layer.Nodes {
				deps := tr.dag.edges[node.ID]
				for dep := range deps {
					if layerNodeSet[dep] {
						t.Fatalf("Layer %d: node %s depends on %s in same layer",
							layerIdx, node.ID, dep)
					}
				}
			}
		}
	})
}

// TestProperty_ToolChain_ExecutionOrder verifies execution order for
// tool-as-installer patterns (e.g., cargo-binstall -> binstall -> ripgrep).
func TestProperty_ToolChain_ExecutionOrder(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tr := toolChainGenerator().Draw(t, "toolChain")

		layers, err := tr.Resolve()
		require.NoError(t, err)

		// Flatten to execution order
		executionOrder := make([]NodeID, 0)
		for _, layer := range layers {
			for _, node := range layer.Nodes {
				executionOrder = append(executionOrder, node.ID)
			}
		}

		// Build execution index map
		executionIdx := make(map[NodeID]int)
		for i, nodeID := range executionOrder {
			executionIdx[nodeID] = i
		}

		// Verify: all dependencies executed before dependents
		for nodeID, deps := range tr.dag.edges {
			nodeIdx := executionIdx[nodeID]
			for dep := range deps {
				depIdx := executionIdx[dep]
				if depIdx >= nodeIdx {
					t.Fatalf("Dependency %s (idx %d) should be before %s (idx %d)",
						dep, depIdx, nodeID, nodeIdx)
				}
			}
		}
	})
}

// TestProperty_CycleDetection_Consistency verifies that Validate() and Resolve()
// are consistent in cycle detection when using actual manifests.
func TestProperty_CycleDetection_Consistency(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tr := cyclicManifestGenerator().Draw(t, "manifest")

		validateErr := tr.Validate()
		_, resolveErr := tr.Resolve()

		if validateErr != nil {
			if resolveErr == nil {
				t.Fatal("Validate() found cycle but Resolve() succeeded")
			}
		}
		if resolveErr != nil && strings.Contains(resolveErr.Error(), "circular dependency") {
			if validateErr == nil {
				t.Fatal("Resolve() found cycle but Validate() succeeded")
			}
		}
	})
}

// TestProperty_Manifest_LayerCount verifies layer count bounds.
func TestProperty_Manifest_LayerCount(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tr := manifestGenerator().Draw(t, "manifest")

		layers, err := tr.Resolve()
		require.NoError(t, err)

		numNodes := len(tr.dag.nodes)

		if numNodes == 0 {
			if len(layers) != 0 {
				t.Fatalf("Expected 0 layers for empty manifest, got %d", len(layers))
			}
		} else {
			// Layer count should be between 1 and numNodes
			if len(layers) < 1 || len(layers) > numNodes {
				t.Fatalf("Layer count %d out of bounds [1, %d]", len(layers), numNodes)
			}
		}
	})
}

// TestProperty_Manifest_RuntimesFirst verifies that runtimes with no dependencies
// are always in the first layer.
func TestProperty_Manifest_RuntimesFirst(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tr := manifestGenerator().Draw(t, "manifest")

		layers, err := tr.Resolve()
		require.NoError(t, err)

		if len(layers) == 0 {
			return
		}

		// Find all runtimes with no dependencies
		for nodeID, node := range tr.dag.nodes {
			if node.Kind == resource.KindRuntime {
				deps := tr.dag.edges[nodeID]
				if len(deps) == 0 {
					// This runtime should be in layer 0
					found := false
					for _, n := range layers[0].Nodes {
						if n.ID == nodeID {
							found = true
							break
						}
					}
					if !found {
						t.Fatalf("Runtime %s with no dependencies should be in layer 0", nodeID)
					}
				}
			}
		}
	})
}

// TestProperty_Manifest_KindOrderWithinLayer verifies that nodes within each layer
// are sorted by Kind priority: Runtime -> Installer -> Tool.
func TestProperty_Manifest_KindOrderWithinLayer(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(t *rapid.T) {
		tr := manifestGenerator().Draw(t, "manifest")

		layers, err := tr.Resolve()
		require.NoError(t, err)

		for layerIdx, layer := range layers {
			for i := 1; i < len(layer.Nodes); i++ {
				prevKind := layer.Nodes[i-1].Kind
				currKind := layer.Nodes[i].Kind

				prevPriority := kindPriorityForTest(prevKind)
				currPriority := kindPriorityForTest(currKind)

				if prevPriority > currPriority {
					t.Fatalf("Layer %d: Kind order violation - %s (%s) should not come before %s (%s)",
						layerIdx,
						layer.Nodes[i-1].ID, prevKind,
						layer.Nodes[i].ID, currKind)
				}

				// Within same Kind, should be sorted by name
				if prevPriority == currPriority {
					if layer.Nodes[i-1].Name > layer.Nodes[i].Name {
						t.Fatalf("Layer %d: Name order violation within same Kind - %s should not come before %s",
							layerIdx, layer.Nodes[i-1].ID, layer.Nodes[i].ID)
					}
				}
			}
		}
	})
}

// kindPriorityForTest mirrors the kindPriority function for test assertions.
// Runtime (100) -> Installer (200) -> InstallerRepository (250) -> Tool (300) -> others (1000)
func kindPriorityForTest(kind resource.Kind) int {
	switch kind {
	case resource.KindRuntime:
		return 100
	case resource.KindInstaller:
		return 200
	case resource.KindInstallerRepository:
		return 250
	case resource.KindTool:
		return 300
	default:
		return 1000
	}
}

// =============================================================================
// Specific Property Tests for Known Structures
// =============================================================================

// TestProperty_KnownStructures tests layer count for known manifest structures.
func TestProperty_KnownStructures(t *testing.T) {
	t.Parallel()
	t.Run("single runtime", func(t *testing.T) {
		t.Parallel()
		resolver := NewResolver()
		resolver.AddResource(createRuntimeWithPattern("go", resource.InstallTypeDownload))

		layers, err := resolver.Resolve()
		require.NoError(t, err)
		assert.Len(t, layers, 1)
	})

	t.Run("runtime with tools", func(t *testing.T) {
		t.Parallel()
		resolver := NewResolver()
		resolver.AddResource(createRuntimeWithPattern("go", resource.InstallTypeDownload))
		resolver.AddResource(createToolWithRuntime("gopls", "go"))
		resolver.AddResource(createToolWithRuntime("golangci-lint", "go"))

		layers, err := resolver.Resolve()
		require.NoError(t, err)
		assert.Len(t, layers, 2)
		assert.Len(t, layers[0].Nodes, 1) // runtime
		assert.Len(t, layers[1].Nodes, 2) // tools (parallel)
	})

	t.Run("tool chain", func(t *testing.T) {
		t.Parallel()
		resolver := NewResolver()
		// Runtime -> Tool -> Installer -> Tool
		resolver.AddResource(createRuntimeWithPattern("rust", resource.InstallTypeDelegation))
		resolver.AddResource(createToolWithRuntime("cargo-binstall", "rust"))
		resolver.AddResource(createInstallerWithTool("binstall", "cargo-binstall"))
		resolver.AddResource(createToolWithInstaller("ripgrep", "binstall"))

		layers, err := resolver.Resolve()
		require.NoError(t, err)
		assert.Len(t, layers, 4)
	})

	t.Run("multiple independent chains", func(t *testing.T) {
		t.Parallel()
		resolver := NewResolver()
		// Go chain
		resolver.AddResource(createRuntimeWithPattern("go", resource.InstallTypeDownload))
		resolver.AddResource(createToolWithRuntime("gopls", "go"))
		// Rust chain
		resolver.AddResource(createRuntimeWithPattern("rust", resource.InstallTypeDelegation))
		resolver.AddResource(createToolWithRuntime("rust-analyzer", "rust"))
		// Download installer
		resolver.AddResource(createDownloadInstaller("aqua"))
		resolver.AddResource(createToolWithInstaller("jq", "aqua"))

		layers, err := resolver.Resolve()
		require.NoError(t, err)
		assert.Len(t, layers, 2)
		// Layer 0: go, rust, aqua (3 independent roots)
		assert.Len(t, layers[0].Nodes, 3)
		// Layer 1: gopls, rust-analyzer, jq (3 tools)
		assert.Len(t, layers[1].Nodes, 3)
	})
}

// =============================================================================
// Helper Functions
// =============================================================================

func createRuntimeWithPattern(name string, pattern resource.InstallType) *resource.Runtime {
	spec := &resource.RuntimeSpec{
		Type:        pattern,
		Version:     "1.0.0",
		ToolBinPath: "~/bin",
	}
	if pattern == resource.InstallTypeDelegation {
		spec.Bootstrap = &resource.RuntimeBootstrapSpec{
			CommandSet: resource.CommandSet{
				Install: []string{"install-" + name},
				Check:   []string{"check-" + name},
			},
		}
	}
	return &resource.Runtime{
		BaseResource: resource.BaseResource{
			APIVersion:   resource.GroupVersion,
			ResourceKind: resource.KindRuntime,
			Metadata:     resource.Metadata{Name: name},
		},
		RuntimeSpec: spec,
	}
}
