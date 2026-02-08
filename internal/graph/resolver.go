package graph

import (
	"fmt"

	"github.com/terassyi/tomei/internal/resource"
)

// Edge represents a dependency edge in the graph.
type Edge struct {
	From NodeID // Dependent node
	To   NodeID // Dependency node
}

// Resolver defines the interface for dependency resolution.
type Resolver interface {
	// AddResource adds a resource and its dependencies to the graph.
	AddResource(res resource.Resource)

	// Resolve validates the graph and returns execution layers.
	// Returns an error if circular dependencies are detected.
	Resolve() ([]Layer, error)

	// Validate checks for circular dependencies without computing the full sort.
	Validate() error

	// NodeCount returns the number of nodes in the graph.
	NodeCount() int

	// EdgeCount returns the number of edges in the graph.
	EdgeCount() int

	// GetEdges returns all edges in the graph.
	GetEdges() []Edge

	// GetNodes returns all nodes in the graph.
	GetNodes() []*Node
}

// resolver is the concrete implementation of Resolver interface.
type resolver struct {
	dag *dag
}

// NewResolver creates a new dependency resolver.
func NewResolver() Resolver {
	return &resolver{
		dag: newDAG(),
	}
}

// AddResource adds a resource and its dependencies to the graph.
func (r *resolver) AddResource(res resource.Resource) {
	kind := res.Kind()
	name := res.Name()

	fromNode := r.dag.addNode(kind, name)

	// Add edges for dependencies
	for _, dep := range res.Spec().Dependencies() {
		toNode := r.dag.addNode(dep.Kind, dep.Name)
		r.dag.addEdge(fromNode, toNode)
	}
}

// Resolve validates the graph and returns execution layers.
// Returns an error if circular dependencies are detected.
func (r *resolver) Resolve() ([]Layer, error) {
	return r.dag.topologicalSort()
}

// Validate checks for circular dependencies without computing the full sort.
func (r *resolver) Validate() error {
	if cycle := r.dag.detectCycle(); cycle != nil {
		return fmt.Errorf("circular dependency detected: %v", cycle)
	}
	return nil
}

// NodeCount returns the number of nodes in the graph.
func (r *resolver) NodeCount() int {
	return r.dag.nodeCount()
}

// EdgeCount returns the number of edges in the graph.
func (r *resolver) EdgeCount() int {
	return r.dag.edgeCount()
}

// GetEdges returns all edges in the graph.
func (r *resolver) GetEdges() []Edge {
	var edges []Edge
	for from, deps := range r.dag.edges {
		for to := range deps {
			edges = append(edges, Edge{From: from, To: to})
		}
	}
	return edges
}

// GetNodes returns all nodes in the graph.
func (r *resolver) GetNodes() []*Node {
	nodes := make([]*Node, 0, len(r.dag.nodes))
	for _, node := range r.dag.nodes {
		nodes = append(nodes, node)
	}
	return nodes
}
