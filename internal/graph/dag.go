package graph

import (
	"fmt"
	"maps"
	"slices"

	"github.com/terassyi/tomei/internal/resource"
)

// NodeID is a unique identifier for a node in the dependency graph.
type NodeID string

// NewNodeID creates a unique node identifier from kind and name.
func NewNodeID(kind resource.Kind, name string) NodeID {
	return NodeID(fmt.Sprintf("%s/%s", kind, name))
}

// String returns the string representation of the NodeID.
func (id NodeID) String() string {
	return string(id)
}

// Node represents a resource in the dependency graph.
type Node struct {
	ID   NodeID        // Kind/Name format (e.g., "Runtime/go", "Tool/ripgrep")
	Kind resource.Kind // Resource kind
	Name string        // Resource name
}

// Layer represents a group of nodes that can be executed in parallel.
type Layer struct {
	Nodes []*Node
}

// dag represents a Directed Acyclic Graph for dependency resolution.
type dag struct {
	nodes    map[NodeID]*Node               // ID -> Node
	edges    map[NodeID]map[NodeID]struct{} // ID -> set of dependency IDs (this node depends on these)
	inDegree map[NodeID]int                 // ID -> number of dependencies
}

// newDAG creates a new empty DAG.
func newDAG() *dag {
	return &dag{
		nodes:    make(map[NodeID]*Node),
		edges:    make(map[NodeID]map[NodeID]struct{}),
		inDegree: make(map[NodeID]int),
	}
}

// addNode adds a node to the graph and returns the created node.
// If the node already exists, it returns the existing node.
func (g *dag) addNode(kind resource.Kind, name string) *Node {
	id := NewNodeID(kind, name)
	if node, exists := g.nodes[id]; exists {
		return node
	}
	node := &Node{ID: id, Kind: kind, Name: name}
	g.nodes[id] = node
	g.inDegree[id] = 0
	return node
}

// addEdge adds a directed edge from -> to (from depends on to).
// Both nodes must exist in the graph; if not, this method panics.
// This ensures type safety by requiring nodes to be added first.
func (g *dag) addEdge(from, to *Node) {
	if from == nil || to == nil {
		panic("graph: addEdge called with nil node")
	}
	if _, exists := g.nodes[from.ID]; !exists {
		panic(fmt.Sprintf("graph: node %s does not exist", from.ID))
	}
	if _, exists := g.nodes[to.ID]; !exists {
		panic(fmt.Sprintf("graph: node %s does not exist", to.ID))
	}

	if g.edges[from.ID] == nil {
		g.edges[from.ID] = make(map[NodeID]struct{})
	}
	// O(1) duplicate check with map
	if _, exists := g.edges[from.ID][to.ID]; !exists {
		g.edges[from.ID][to.ID] = struct{}{}
		g.inDegree[from.ID]++
	}
}

// nodeColor represents the state of a node during DFS traversal.
type nodeColor int

const (
	white nodeColor = iota // unvisited
	gray                   // visiting (in current path)
	black                  // visited (finished)
)

// detectCycle returns a cycle path if one exists, nil otherwise.
// Uses DFS with three-color marking for cycle detection.
func (g *dag) detectCycle() []NodeID {
	color := make(map[NodeID]nodeColor, len(g.nodes))
	parent := make(map[NodeID]NodeID, len(g.nodes))

	var cycle []NodeID

	var dfs func(node NodeID) bool
	dfs = func(node NodeID) bool {
		color[node] = gray

		for dep := range g.edges[node] {
			if color[dep] == gray {
				// Found cycle - reconstruct path
				cycle = []NodeID{dep}
				for curr := node; curr != dep; curr = parent[curr] {
					cycle = append(cycle, curr)
				}
				cycle = append(cycle, dep)
				slices.Reverse(cycle)
				return true
			}
			if color[dep] == white {
				parent[dep] = node
				if dfs(dep) {
					return true
				}
			}
		}

		color[node] = black
		return false
	}

	for id := range g.nodes {
		if color[id] == white {
			if dfs(id) {
				return cycle
			}
		}
	}

	return nil
}

// kindPriority returns the priority of a resource kind.
// Lower values are processed first within the same layer.
// Values are spaced apart to allow future insertions between existing kinds.
// Order: Runtime (100) -> Installer (200) -> InstallerRepository (250) -> Tool (300) -> others (1000)
func kindPriority(kind resource.Kind) int {
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

// sortNodesByKind sorts nodes by Kind priority (Runtime -> Installer -> Tool).
// Within the same Kind, nodes are sorted by name for deterministic output.
func sortNodesByKind(nodes []*Node) {
	slices.SortFunc(nodes, func(a, b *Node) int {
		// First, sort by Kind priority
		priorityA := kindPriority(a.Kind)
		priorityB := kindPriority(b.Kind)
		if priorityA != priorityB {
			return priorityA - priorityB
		}
		// Within same Kind, sort by name for determinism
		if a.Name < b.Name {
			return -1
		}
		if a.Name > b.Name {
			return 1
		}
		return 0
	})
}

// topologicalSort returns execution layers using Kahn's algorithm.
// Nodes in the same layer have no dependencies between them.
// Within each layer, nodes are sorted by Kind priority: Runtime -> Installer -> Tool.
func (g *dag) topologicalSort() ([]Layer, error) {
	if cycle := g.detectCycle(); cycle != nil {
		return nil, NewCycleError(cycle)
	}

	// Clone inDegree for modification
	inDegree := make(map[NodeID]int, len(g.inDegree))
	maps.Copy(inDegree, g.inDegree)

	// Build reverse edges: dependency -> dependents
	reverseEdges := make(map[NodeID][]NodeID, len(g.nodes))
	for from, deps := range g.edges {
		for dep := range deps {
			reverseEdges[dep] = append(reverseEdges[dep], from)
		}
	}

	// Pre-allocate layers slice with estimated capacity
	layers := make([]Layer, 0, len(g.nodes))

	// Find all nodes with inDegree 0 (no dependencies)
	queue := make([]NodeID, 0, len(g.nodes))
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}

	for len(queue) > 0 {
		layer := Layer{Nodes: make([]*Node, 0, len(queue))}
		nextQueue := make([]NodeID, 0, len(g.nodes))

		for _, id := range queue {
			layer.Nodes = append(layer.Nodes, g.nodes[id])

			// Reduce inDegree of dependents
			for _, dependent := range reverseEdges[id] {
				inDegree[dependent]--
				if inDegree[dependent] == 0 {
					nextQueue = append(nextQueue, dependent)
				}
			}
		}

		// Sort nodes within layer by Kind priority
		sortNodesByKind(layer.Nodes)

		layers = append(layers, layer)
		queue = nextQueue
	}

	return layers, nil
}

// nodeCount returns the number of nodes in the graph.
func (g *dag) nodeCount() int {
	return len(g.nodes)
}

// edgeCount returns the number of edges in the graph.
func (g *dag) edgeCount() int {
	count := 0
	for _, deps := range g.edges {
		count += len(deps)
	}
	return count
}
