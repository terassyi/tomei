package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func TestNewNodeID(t *testing.T) {
	tests := []struct {
		kind     resource.Kind
		name     string
		expected NodeID
	}{
		{resource.KindRuntime, "go", "Runtime/go"},
		{resource.KindTool, "ripgrep", "Tool/ripgrep"},
		{resource.KindInstaller, "aqua", "Installer/aqua"},
	}

	for _, tt := range tests {
		t.Run(tt.expected.String(), func(t *testing.T) {
			got := NewNodeID(tt.kind, tt.name)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDAG_AddNode(t *testing.T) {
	d := newDAG()

	d.addNode(resource.KindRuntime, "go")
	assert.Equal(t, 1, d.nodeCount())

	// Adding same node again should not increase count
	d.addNode(resource.KindRuntime, "go")
	assert.Equal(t, 1, d.nodeCount())

	d.addNode(resource.KindTool, "ripgrep")
	assert.Equal(t, 2, d.nodeCount())
}

func TestDAG_AddEdge(t *testing.T) {
	d := newDAG()

	goplsNode := d.addNode(resource.KindTool, "gopls")
	goNode := d.addNode(resource.KindRuntime, "go")

	d.addEdge(goplsNode, goNode)
	assert.Equal(t, 1, d.edgeCount())

	// Adding same edge again should not increase count
	d.addEdge(goplsNode, goNode)
	assert.Equal(t, 1, d.edgeCount())
}

func TestDAG_AddEdge_PanicOnNilNode(t *testing.T) {
	d := newDAG()
	node := d.addNode(resource.KindTool, "test")

	assert.Panics(t, func() {
		d.addEdge(nil, node)
	})

	assert.Panics(t, func() {
		d.addEdge(node, nil)
	})
}

func TestDAG_AddEdge_PanicOnNonExistentNode(t *testing.T) {
	d := newDAG()
	node := d.addNode(resource.KindTool, "test")
	fakeNode := &Node{ID: "Tool/fake", Kind: resource.KindTool, Name: "fake"}

	assert.Panics(t, func() {
		d.addEdge(node, fakeNode)
	})
}

func TestDAG_DetectCycle_NoCycle(t *testing.T) {
	d := newDAG()

	// Runtime -> Installer -> Tool (no cycle)
	goRuntime := d.addNode(resource.KindRuntime, "go")
	goInstaller := d.addNode(resource.KindInstaller, "go")
	gopls := d.addNode(resource.KindTool, "gopls")

	d.addEdge(goInstaller, goRuntime)
	d.addEdge(gopls, goInstaller)

	cycle := d.detectCycle()
	assert.Nil(t, cycle)
}

func TestDAG_DetectCycle_SimpleCycle(t *testing.T) {
	d := newDAG()

	// A -> B -> A (cycle)
	a := d.addNode(resource.KindTool, "a")
	b := d.addNode(resource.KindTool, "b")

	d.addEdge(a, b)
	d.addEdge(b, a)

	cycle := d.detectCycle()
	require.NotNil(t, cycle)
	assert.Len(t, cycle, 3) // a -> b -> a
}

func TestDAG_DetectCycle_ComplexCycle(t *testing.T) {
	d := newDAG()

	// A -> B -> C -> A (3-node cycle)
	a := d.addNode(resource.KindTool, "a")
	b := d.addNode(resource.KindTool, "b")
	c := d.addNode(resource.KindTool, "c")

	d.addEdge(a, b)
	d.addEdge(b, c)
	d.addEdge(c, a)

	cycle := d.detectCycle()
	require.NotNil(t, cycle)
	assert.GreaterOrEqual(t, len(cycle), 3)
}

func TestDAG_TopologicalSort_Simple(t *testing.T) {
	d := newDAG()

	// Runtime <- Installer <- Tool
	goRuntime := d.addNode(resource.KindRuntime, "go")
	goInstaller := d.addNode(resource.KindInstaller, "go")
	gopls := d.addNode(resource.KindTool, "gopls")

	d.addEdge(goInstaller, goRuntime)
	d.addEdge(gopls, goInstaller)

	layers, err := d.topologicalSort()
	require.NoError(t, err)
	require.Len(t, layers, 3)

	// Layer 0: Runtime (no dependencies)
	assert.Len(t, layers[0].Nodes, 1)
	assert.Equal(t, NodeID("Runtime/go"), layers[0].Nodes[0].ID)

	// Layer 1: Installer (depends on Runtime)
	assert.Len(t, layers[1].Nodes, 1)
	assert.Equal(t, NodeID("Installer/go"), layers[1].Nodes[0].ID)

	// Layer 2: Tool (depends on Installer)
	assert.Len(t, layers[2].Nodes, 1)
	assert.Equal(t, NodeID("Tool/gopls"), layers[2].Nodes[0].ID)
}

func TestDAG_TopologicalSort_Diamond(t *testing.T) {
	d := newDAG()

	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	a := d.addNode(resource.KindTool, "a")
	b := d.addNode(resource.KindTool, "b")
	c := d.addNode(resource.KindTool, "c")
	dd := d.addNode(resource.KindTool, "d")

	d.addEdge(b, a)
	d.addEdge(c, a)
	d.addEdge(dd, b)
	d.addEdge(dd, c)

	layers, err := d.topologicalSort()
	require.NoError(t, err)
	require.Len(t, layers, 3)

	// Layer 0: A (no dependencies)
	assert.Len(t, layers[0].Nodes, 1)
	assert.Equal(t, NodeID("Tool/a"), layers[0].Nodes[0].ID)

	// Layer 1: B and C (both depend only on A)
	assert.Len(t, layers[1].Nodes, 2)
	ids := []NodeID{layers[1].Nodes[0].ID, layers[1].Nodes[1].ID}
	assert.Contains(t, ids, NodeID("Tool/b"))
	assert.Contains(t, ids, NodeID("Tool/c"))

	// Layer 2: D (depends on B and C)
	assert.Len(t, layers[2].Nodes, 1)
	assert.Equal(t, NodeID("Tool/d"), layers[2].Nodes[0].ID)
}

func TestDAG_TopologicalSort_MultiLayer(t *testing.T) {
	d := newDAG()

	// Tool chain: Runtime(rust) <- Installer(cargo) <- Tool(cargo-binstall) <- Installer(binstall) <- Tool(ripgrep)
	rustRuntime := d.addNode(resource.KindRuntime, "rust")
	cargoInstaller := d.addNode(resource.KindInstaller, "cargo")
	binstallTool := d.addNode(resource.KindTool, "cargo-binstall")
	binstallInstaller := d.addNode(resource.KindInstaller, "binstall")
	ripgrep := d.addNode(resource.KindTool, "ripgrep")

	d.addEdge(cargoInstaller, rustRuntime)
	d.addEdge(binstallTool, cargoInstaller)
	d.addEdge(binstallInstaller, binstallTool)
	d.addEdge(ripgrep, binstallInstaller)

	layers, err := d.topologicalSort()
	require.NoError(t, err)
	require.Len(t, layers, 5)

	// Verify execution order
	assert.Equal(t, NodeID("Runtime/rust"), layers[0].Nodes[0].ID)
	assert.Equal(t, NodeID("Installer/cargo"), layers[1].Nodes[0].ID)
	assert.Equal(t, NodeID("Tool/cargo-binstall"), layers[2].Nodes[0].ID)
	assert.Equal(t, NodeID("Installer/binstall"), layers[3].Nodes[0].ID)
	assert.Equal(t, NodeID("Tool/ripgrep"), layers[4].Nodes[0].ID)
}

func TestDAG_TopologicalSort_WithCycle(t *testing.T) {
	d := newDAG()

	a := d.addNode(resource.KindTool, "a")
	b := d.addNode(resource.KindTool, "b")

	d.addEdge(a, b)
	d.addEdge(b, a)

	layers, err := d.topologicalSort()
	require.Error(t, err)
	assert.Nil(t, layers)
	assert.Contains(t, err.Error(), "circular dependency")
}

func TestDAG_TopologicalSort_ParallelNodes(t *testing.T) {
	d := newDAG()

	// Multiple independent tools
	ripgrep := d.addNode(resource.KindTool, "ripgrep")
	fd := d.addNode(resource.KindTool, "fd")
	bat := d.addNode(resource.KindTool, "bat")
	aqua := d.addNode(resource.KindInstaller, "aqua")

	// All tools depend on aqua installer
	d.addEdge(ripgrep, aqua)
	d.addEdge(fd, aqua)
	d.addEdge(bat, aqua)

	layers, err := d.topologicalSort()
	require.NoError(t, err)
	require.Len(t, layers, 2)

	// Layer 0: aqua installer
	assert.Len(t, layers[0].Nodes, 1)
	assert.Equal(t, NodeID("Installer/aqua"), layers[0].Nodes[0].ID)

	// Layer 1: all tools (can be executed in parallel)
	assert.Len(t, layers[1].Nodes, 3)
}

func TestDAG_TopologicalSort_KindPriority(t *testing.T) {
	t.Run("same layer sorted by kind priority", func(t *testing.T) {
		d := newDAG()

		// All independent nodes (no dependencies) - should be in same layer
		// Add in random order
		d.addNode(resource.KindTool, "ripgrep")
		d.addNode(resource.KindRuntime, "go")
		d.addNode(resource.KindInstaller, "aqua")
		d.addNode(resource.KindTool, "fd")
		d.addNode(resource.KindRuntime, "rust")
		d.addNode(resource.KindInstaller, "brew")

		layers, err := d.topologicalSort()
		require.NoError(t, err)
		require.Len(t, layers, 1)
		require.Len(t, layers[0].Nodes, 6)

		// Verify order: Runtime -> Installer -> Tool (alphabetically within each kind)
		expected := []NodeID{
			"Runtime/go",
			"Runtime/rust",
			"Installer/aqua",
			"Installer/brew",
			"Tool/fd",
			"Tool/ripgrep",
		}
		for i, node := range layers[0].Nodes {
			assert.Equal(t, expected[i], node.ID, "node at index %d", i)
		}
	})

	t.Run("mixed layer with dependencies", func(t *testing.T) {
		d := newDAG()

		// Layer 0: go runtime and aqua installer (independent)
		goRuntime := d.addNode(resource.KindRuntime, "go")
		aqua := d.addNode(resource.KindInstaller, "aqua")

		// Layer 1: tools that depend on layer 0
		gopls := d.addNode(resource.KindTool, "gopls")
		ripgrep := d.addNode(resource.KindTool, "ripgrep")

		d.addEdge(gopls, goRuntime)
		d.addEdge(ripgrep, aqua)

		layers, err := d.topologicalSort()
		require.NoError(t, err)
		require.Len(t, layers, 2)

		// Layer 0: Runtime first, then Installer
		require.Len(t, layers[0].Nodes, 2)
		assert.Equal(t, NodeID("Runtime/go"), layers[0].Nodes[0].ID)
		assert.Equal(t, NodeID("Installer/aqua"), layers[0].Nodes[1].ID)

		// Layer 1: Tools sorted alphabetically
		require.Len(t, layers[1].Nodes, 2)
		assert.Equal(t, NodeID("Tool/gopls"), layers[1].Nodes[0].ID)
		assert.Equal(t, NodeID("Tool/ripgrep"), layers[1].Nodes[1].ID)
	})

	t.Run("installer with tool dependency in same potential layer", func(t *testing.T) {
		d := newDAG()

		// Independent nodes that would be in layer 0
		goRuntime := d.addNode(resource.KindRuntime, "go")
		d.addNode(resource.KindInstaller, "aqua") // aqua has no dependencies

		// pnpm tool depends on go runtime
		pnpm := d.addNode(resource.KindTool, "pnpm")
		d.addEdge(pnpm, goRuntime)

		// npm installer depends on pnpm tool (tool-as-installer pattern)
		npm := d.addNode(resource.KindInstaller, "npm")
		d.addEdge(npm, pnpm)

		// vite tool depends on npm installer
		vite := d.addNode(resource.KindTool, "vite")
		d.addEdge(vite, npm)

		layers, err := d.topologicalSort()
		require.NoError(t, err)
		require.Len(t, layers, 4)

		// Layer 0: go runtime and aqua installer (sorted: Runtime first)
		require.Len(t, layers[0].Nodes, 2)
		assert.Equal(t, NodeID("Runtime/go"), layers[0].Nodes[0].ID)
		assert.Equal(t, NodeID("Installer/aqua"), layers[0].Nodes[1].ID)

		// Layer 1: pnpm tool
		require.Len(t, layers[1].Nodes, 1)
		assert.Equal(t, NodeID("Tool/pnpm"), layers[1].Nodes[0].ID)

		// Layer 2: npm installer
		require.Len(t, layers[2].Nodes, 1)
		assert.Equal(t, NodeID("Installer/npm"), layers[2].Nodes[0].ID)

		// Layer 3: vite tool
		require.Len(t, layers[3].Nodes, 1)
		assert.Equal(t, NodeID("Tool/vite"), layers[3].Nodes[0].ID)
	})
}

func TestKindPriority(t *testing.T) {
	tests := []struct {
		kind     resource.Kind
		expected int
	}{
		{resource.KindRuntime, 100},
		{resource.KindInstaller, 200},
		{resource.KindInstallerRepository, 250},
		{resource.KindTool, 300},
		{resource.Kind("Unknown"), 1000},
	}

	for _, tt := range tests {
		t.Run(string(tt.kind), func(t *testing.T) {
			assert.Equal(t, tt.expected, kindPriority(tt.kind))
		})
	}

	// Verify ordering is maintained (lower value = higher priority)
	assert.Less(t, kindPriority(resource.KindRuntime), kindPriority(resource.KindInstaller))
	assert.Less(t, kindPriority(resource.KindInstaller), kindPriority(resource.KindInstallerRepository))
	assert.Less(t, kindPriority(resource.KindInstallerRepository), kindPriority(resource.KindTool))
	assert.Less(t, kindPriority(resource.KindTool), kindPriority(resource.Kind("Unknown")))
}

func TestSortNodesByKind(t *testing.T) {
	nodes := []*Node{
		{ID: "Tool/ripgrep", Kind: resource.KindTool, Name: "ripgrep"},
		{ID: "Runtime/go", Kind: resource.KindRuntime, Name: "go"},
		{ID: "Installer/aqua", Kind: resource.KindInstaller, Name: "aqua"},
		{ID: "Tool/fd", Kind: resource.KindTool, Name: "fd"},
		{ID: "Runtime/rust", Kind: resource.KindRuntime, Name: "rust"},
		{ID: "Installer/brew", Kind: resource.KindInstaller, Name: "brew"},
	}

	sortNodesByKind(nodes)

	expected := []NodeID{
		"Runtime/go",
		"Runtime/rust",
		"Installer/aqua",
		"Installer/brew",
		"Tool/fd",
		"Tool/ripgrep",
	}

	for i, node := range nodes {
		assert.Equal(t, expected[i], node.ID, "node at index %d", i)
	}
}
