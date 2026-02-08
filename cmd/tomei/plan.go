package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/github"
	"github.com/terassyi/tomei/internal/graph"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/registry/aqua"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
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

Resources are shown in dependency order as a tree.
Use --output to change the output format (text, json, yaml).`,
	Args: cobra.MinimumNArgs(1),
	RunE: runPlan,
}

var (
	syncRegistryPlan bool
	outputFormat     string
	noColor          bool
)

func init() {
	planCmd.Flags().BoolVar(&syncRegistryPlan, "sync", false, "Sync aqua registry to latest version before planning")
	planCmd.Flags().StringVarP(&outputFormat, "output", "o", "text", "Output format: text, json, yaml")
	planCmd.Flags().BoolVar(&noColor, "no-color", false, "Disable colored output")
}

func runPlan(cmd *cobra.Command, args []string) error {
	// Disable color if --no-color flag is set
	if noColor {
		color.NoColor = true
	}

	// Sync registry if --sync flag is set
	if syncRegistryPlan {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		if err := syncRegistryForPlan(ctx); err != nil {
			slog.Warn("failed to sync aqua registry", "error", err)
		}
	}

	// Load configuration
	loader := config.NewLoader(nil)
	resources, err := loader.LoadPaths(args)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if len(resources) == 0 {
		cmd.Println("No resources found")
		return nil
	}

	// Expand set resources (ToolSet, etc.) into individual resources
	resources, err = resource.ExpandSets(resources)
	if err != nil {
		return fmt.Errorf("failed to expand sets: %w", err)
	}

	// Build a set of defined resource IDs for filtering
	definedResources := make(map[string]struct{})
	resourceMap := make(map[graph.NodeID]resource.Resource)
	for _, res := range resources {
		id := graph.NewNodeID(res.Kind(), res.Name())
		definedResources[id.String()] = struct{}{}
		resourceMap[id] = res
	}

	// Build dependency graph
	resolver := graph.NewResolver()
	for _, res := range resources {
		resolver.AddResource(res)
	}

	// Validate and get execution layers
	layers, err := resolver.Resolve()
	if err != nil {
		// Return the error as-is - it will be formatted by main.go
		return err
	}

	// Filter layers to only include defined resources
	var filteredLayers []graph.Layer
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
		}
	}

	// Load state to determine actions
	resourceInfo, err := buildResourceInfo(resources, resourceMap)
	if err != nil {
		return err
	}

	// Get edges for tree/export
	edges := resolver.GetEdges()

	// Output based on format
	switch outputFormat {
	case "json":
		exporter := graph.NewExporter(filteredLayers, resourceInfo, edges)
		return exporter.ExportJSON(os.Stdout)
	case "yaml":
		exporter := graph.NewExporter(filteredLayers, resourceInfo, edges)
		return exporter.ExportYAML(os.Stdout)
	case "text":
		fallthrough
	default:
		return printTextPlan(cmd, args, resources, resolver, filteredLayers, resourceInfo)
	}
}

func buildResourceInfo(resources []resource.Resource, _ map[graph.NodeID]resource.Resource) (map[graph.NodeID]graph.ResourceInfo, error) {
	info := make(map[graph.NodeID]graph.ResourceInfo)

	// Load config and sync schema
	var userState *state.UserState
	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err == nil {
		if err := config.SyncSchema(cfg, config.DefaultConfigDir); err != nil {
			return nil, fmt.Errorf("failed to sync schema: %w", err)
		}

		pathConfig, err := path.NewFromConfig(cfg)
		if err == nil {
			store, err := state.NewStore[state.UserState](pathConfig.UserDataDir())
			if err == nil {
				loaded, err := store.LoadReadOnly()
				if err == nil {
					userState = loaded
				}
			}
		}
	}

	for _, res := range resources {
		nodeID := graph.NewNodeID(res.Kind(), res.Name())
		resInfo := graph.ResourceInfo{
			Kind:   res.Kind(),
			Name:   res.Name(),
			Action: graph.ActionInstall, // default to install
		}

		// Get version from spec
		switch res.Kind() {
		case resource.KindRuntime:
			if rt, ok := res.(*resource.Runtime); ok && rt.RuntimeSpec != nil {
				resInfo.Version = rt.RuntimeSpec.Version
			}
		case resource.KindTool:
			if tool, ok := res.(*resource.Tool); ok && tool.ToolSpec != nil {
				resInfo.Version = tool.ToolSpec.Version
			}
		}

		// Determine action by comparing with state
		if userState != nil {
			switch res.Kind() {
			case resource.KindRuntime:
				if rt, ok := userState.Runtimes[res.Name()]; ok {
					if rt.Version == resInfo.Version {
						resInfo.Action = graph.ActionNone
					} else {
						resInfo.Action = graph.ActionUpgrade
					}
				}
			case resource.KindTool:
				if tool, ok := userState.Tools[res.Name()]; ok {
					if tool.Version == resInfo.Version {
						resInfo.Action = graph.ActionNone
					} else {
						resInfo.Action = graph.ActionUpgrade
					}
				}
			case resource.KindInstaller:
				// Installers don't have versions in state typically
				resInfo.Action = graph.ActionNone
			}
		}

		info[nodeID] = resInfo
	}

	return info, nil
}

func printTextPlan(cmd *cobra.Command, args []string, resources []resource.Resource, resolver graph.Resolver, layers []graph.Layer, resourceInfo map[graph.NodeID]graph.ResourceInfo) error {
	cmd.Printf("Planning changes for %v\n\n", args)
	cmd.Printf("Found %d resource(s)\n\n", len(resources))

	// Print dependency tree
	cmd.Println("Dependency Graph:")
	printer := graph.NewTreePrinter(cmd.OutOrStdout(), noColor)
	printer.PrintTree(resolver, resourceInfo)

	// Print execution layers
	printer.PrintLayers(layers, resourceInfo)

	// Print summary
	printer.PrintSummary(resourceInfo)

	return nil
}

// syncRegistryForPlan creates a store and syncs the aqua registry.
func syncRegistryForPlan(ctx context.Context) error {
	// Load config from fixed path (~/.config/tomei/config.cue)
	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup paths from config
	pathConfig, err := path.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize paths: %w", err)
	}

	// Create state store
	store, err := state.NewStore[state.UserState](pathConfig.UserDataDir())
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}

	ghClient := github.NewHTTPClient(github.TokenFromEnv())
	return aqua.SyncRegistry(ctx, store, ghClient)
}
