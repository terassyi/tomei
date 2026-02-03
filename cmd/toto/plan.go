package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/graph"
)

var planCmd = &cobra.Command{
	Use:   "plan <files or directories...>",
	Short: "Show the execution plan",
	Long: `Show the execution plan without applying changes.

Displays what actions would be taken:
  - install: New resources to install
  - upgrade: Resources to upgrade
  - reinstall: Resources to reinstall (due to taint)
  - remove: Resources to remove

Resources are shown in dependency order, grouped by execution layer.
Resources within the same layer can be executed in parallel.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runPlan,
}

func runPlan(cmd *cobra.Command, args []string) error {
	cmd.Printf("Planning changes for %v\n\n", args)

	loader := config.NewLoader(nil)
	resources, err := loader.LoadPaths(args)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if len(resources) == 0 {
		cmd.Println("No resources found")
		return nil
	}

	cmd.Printf("Found %d resource(s)\n\n", len(resources))

	// Build a set of defined resource IDs for filtering
	definedResources := make(map[string]struct{})
	for _, res := range resources {
		id := graph.NewNodeID(res.Kind(), res.Name()).String()
		definedResources[id] = struct{}{}
	}

	// Build dependency graph
	resolver := graph.NewResolver()
	for _, res := range resources {
		resolver.AddResource(res)
	}

	// Validate and get execution layers
	layers, err := resolver.Resolve()
	if err != nil {
		return fmt.Errorf("failed to resolve dependencies: %w", err)
	}

	// Filter layers to only include defined resources
	var filteredLayers []graph.Layer
	totalResources := 0
	for _, layer := range layers {
		var filteredNodes []*graph.Node
		for _, node := range layer.Nodes {
			id := graph.NewNodeID(node.Kind, node.Name).String()
			if _, ok := definedResources[id]; ok {
				filteredNodes = append(filteredNodes, node)
			}
		}
		if len(filteredNodes) > 0 {
			filteredLayers = append(filteredLayers, graph.Layer{Nodes: filteredNodes})
			totalResources += len(filteredNodes)
		}
	}

	// Display execution plan
	cmd.Printf("Execution Plan (%d layers):\n", len(filteredLayers))
	cmd.Println("================================")

	for i, layer := range filteredLayers {
		cmd.Printf("\nLayer %d", i+1)
		if len(layer.Nodes) > 1 {
			cmd.Printf(" (parallel: %d resources)", len(layer.Nodes))
		}
		cmd.Println(":")

		for _, node := range layer.Nodes {
			cmd.Printf("  - %s/%s\n", node.Kind, node.Name)
		}
	}

	cmd.Println()

	// Show dependency information
	cmd.Println("Dependencies:")
	for _, res := range resources {
		deps := res.Spec().Dependencies()
		if len(deps) > 0 {
			cmd.Printf("  %s/%s depends on:\n", res.Kind(), res.Name())
			for _, dep := range deps {
				status := "(defined)"
				depID := graph.NewNodeID(dep.Kind, dep.Name).String()
				if _, ok := definedResources[depID]; !ok {
					status = "(builtin/external)"
				}
				cmd.Printf("    - %s/%s %s\n", dep.Kind, dep.Name, status)
			}
		}
	}

	cmd.Println()

	// TODO: Load state and diff to show actual actions (install/upgrade/remove)
	// For now, just show the execution order

	cmd.Printf("Total: %d resources in %d layers\n", totalResources, len(filteredLayers))

	return nil
}
