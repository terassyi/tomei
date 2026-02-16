package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/github"
	"github.com/terassyi/tomei/internal/graph"
	"github.com/terassyi/tomei/internal/installer/engine"
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
	_ = planCmd.RegisterFlagCompletionFunc("output", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"text", "json", "yaml"}, cobra.ShellCompDirectiveNoFileComp
	})
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

	result, err := resolvePlan(resources)
	if err != nil {
		return err
	}

	// Output based on format
	switch outputFormat {
	case outputJSON:
		exporter := graph.NewExporter(result.filteredLayers, result.resourceInfo, result.edges)
		return exporter.ExportJSON(os.Stdout)
	case "yaml":
		exporter := graph.NewExporter(result.filteredLayers, result.resourceInfo, result.edges)
		return exporter.ExportYAML(os.Stdout)
	case "text":
		fallthrough
	default:
		return printTextPlan(cmd, args, resources, result)
	}
}

func buildResourceInfo(resources []resource.Resource) map[graph.NodeID]graph.ResourceInfo {
	info := make(map[graph.NodeID]graph.ResourceInfo)

	// Load config and state
	var userState *state.UserState
	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err == nil {
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

	if userState == nil {
		fmt.Fprintln(os.Stderr, "Warning: tomei is not initialized. Run 'tomei init' for accurate state comparison.")
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

	// Detect removals: resources in state but not in manifests
	if userState != nil {
		for name, rt := range userState.Runtimes {
			nodeID := graph.NewNodeID(resource.KindRuntime, name)
			if _, exists := info[nodeID]; !exists {
				info[nodeID] = graph.ResourceInfo{
					Kind:    resource.KindRuntime,
					Name:    name,
					Version: rt.Version,
					Action:  graph.ActionRemove,
				}
			}
		}
		for name, tool := range userState.Tools {
			nodeID := graph.NewNodeID(resource.KindTool, name)
			if _, exists := info[nodeID]; !exists {
				info[nodeID] = graph.ResourceInfo{
					Kind:    resource.KindTool,
					Name:    name,
					Version: tool.Version,
					Action:  graph.ActionRemove,
				}
			}
		}

		// Predict taint reinstalls: if a runtime with TaintOnUpgrade is being upgraded,
		// tools that depend on it (via RuntimeRef) will be reinstalled.
		// Build a map of runtime specs to check TaintOnUpgrade from the manifest.
		runtimeSpecs := map[string]*resource.RuntimeSpec{}
		upgradedRuntimes := map[string]bool{}
		for _, res := range resources {
			if res.Kind() == resource.KindRuntime {
				if rt, ok := res.(*resource.Runtime); ok && rt.RuntimeSpec != nil {
					runtimeSpecs[res.Name()] = rt.RuntimeSpec
				}
				nodeID := graph.NewNodeID(res.Kind(), res.Name())
				if ri, ok := info[nodeID]; ok && ri.Action == graph.ActionUpgrade {
					upgradedRuntimes[res.Name()] = true
				}
			}
		}
		if len(upgradedRuntimes) > 0 {
			for name, toolState := range userState.Tools {
				if toolState.RuntimeRef == "" || !upgradedRuntimes[toolState.RuntimeRef] {
					continue
				}
				// Check TaintOnUpgrade from the runtime spec in the manifest
				spec, ok := runtimeSpecs[toolState.RuntimeRef]
				if !ok || !spec.TaintOnUpgrade {
					continue
				}
				nodeID := graph.NewNodeID(resource.KindTool, name)
				if ri, ok := info[nodeID]; ok && ri.Action == graph.ActionNone {
					ri.Action = graph.ActionReinstall
					info[nodeID] = ri
				}
			}
		}
	}

	return info
}

func printTextPlan(cmd *cobra.Command, args []string, resources []resource.Resource, result *planResult) error {
	cmd.Printf("Planning changes for %v\n\n", args)
	cmd.Printf("Found %d resource(s)\n\n", len(resources))

	// Print dependency tree
	cmd.Println("Dependency Graph:")
	printer := graph.NewTreePrinter(cmd.OutOrStdout(), noColor)
	printer.PrintTree(result.resolver, result.resourceInfo)

	// Print execution layers
	printer.PrintLayers(result.filteredLayers, result.resourceInfo)

	// Print summary
	printer.PrintSummary(result.resourceInfo)

	return nil
}

// planResult holds the resolved plan state.
type planResult struct {
	resolver       graph.Resolver
	filteredLayers []graph.Layer
	resourceInfo   map[graph.NodeID]graph.ResourceInfo
	edges          []graph.Edge
}

// resolvePlan builds the dependency graph, resolves execution layers, and
// computes resource actions from the current state.
func resolvePlan(resources []resource.Resource) (*planResult, error) {
	definedResources := make(map[string]struct{})
	for _, res := range resources {
		id := graph.NewNodeID(res.Kind(), res.Name())
		definedResources[id.String()] = struct{}{}
	}

	// Inject builtin installers into the resolver only so that dependency
	// nodes like "Installer/aqua" are properly resolved.
	resolver := graph.NewResolver()
	for _, res := range engine.AppendBuiltinInstallers(resources) {
		resolver.AddResource(res)
	}

	layers, err := resolver.Resolve()
	if err != nil {
		return nil, err
	}

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

	resourceInfo := buildResourceInfo(resources)

	return &planResult{
		resolver:       resolver,
		filteredLayers: filteredLayers,
		resourceInfo:   resourceInfo,
		edges:          resolver.GetEdges(),
	}, nil
}

// planForResources runs the plan logic on already-loaded resources and
// writes the text plan to w. It returns true if there are any changes
// (install, upgrade, reinstall, or remove).
func planForResources(w io.Writer, resources []resource.Resource, disableColor bool) (bool, error) {
	result, err := resolvePlan(resources)
	if err != nil {
		return false, err
	}

	hasChanges := false
	for _, info := range result.resourceInfo {
		if info.Action != graph.ActionNone {
			hasChanges = true
			break
		}
	}

	fmt.Fprintf(w, "Found %d resource(s)\n\n", len(resources))
	printer := graph.NewTreePrinter(w, disableColor)
	printer.PrintTree(result.resolver, result.resourceInfo)
	printer.PrintLayers(result.filteredLayers, result.resourceInfo)
	printer.PrintSummary(result.resourceInfo)

	return hasChanges, nil
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
