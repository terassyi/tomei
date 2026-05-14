package main

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/github"
	"github.com/terassyi/tomei/internal/graph"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/installer/reconciler"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/registry/aqua"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

var planCmd = &cobra.Command{
	Use:   "plan <files or directories...>",
	Short: "Show the execution plan",
	Long: `Show the execution plan without applying changes.

Compares CUE manifests (desired state) with the current state and shows
what actions would be taken by "tomei apply":
  - install: New resources to install
  - upgrade: Resources with version changes
  - reinstall: Tainted resources to reinstall (e.g., runtime upgrade, update flags)
  - remove: Resources in state but not in manifests
  - skip: Resources disabled via enabled: false

Resources are shown in dependency order as a tree. Execution layers
show which resources run in parallel.

Use --output json or --output yaml for machine-readable output
(suitable for scripting and programmatic consumption).`,
	Args: cobra.MinimumNArgs(1),
	RunE: runPlan,
}

// planConfig holds configuration for the plan command.
type planConfig struct {
	loadConfig
	outputFormat string
}

var planCfg planConfig

func init() {
	planCfg.registerFlags(planCmd)
	planCmd.Flags().StringVarP(&planCfg.outputFormat, "output", "o", outputText, "Output format: text, json, yaml")
	_ = planCmd.RegisterFlagCompletionFunc("output", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{outputText, outputJSON, "yaml"}, cobra.ShellCompDirectiveNoFileComp
	})
}

func runPlan(cmd *cobra.Command, args []string) error {
	// Disable color if --no-color flag is set
	if planCfg.noColor {
		color.NoColor = true
	}

	// Sync registry if --sync or --update-tools/--update-all flag is set
	if planCfg.syncRegistry || planCfg.updateTools || planCfg.updateAll {
		ctx := cmd.Context()
		if ctx == nil {
			ctx = context.Background()
		}
		if err := syncRegistryForPlan(ctx); err != nil {
			slog.Warn("failed to sync aqua registry", "error", err)
		}
	}

	// Load configuration
	loader := config.NewLoader(nil, planCfg.verifierOpts()...)
	resources, err := loader.LoadPaths(args)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	if len(resources) == 0 {
		cmd.Println("No resources found")
		return nil
	}

	// Collect disabled resources before expansion (for plan display)
	disabledResources := resource.CollectDisabled(resources)

	// Expand set resources (ToolSet, etc.) into individual resources
	resources, err = resource.ExpandSets(resources)
	if err != nil {
		return fmt.Errorf("failed to expand sets: %w", err)
	}

	updateCfg := engine.UpdateConfig{
		SyncMode:       planCfg.syncRegistry,
		UpdateTools:    planCfg.updateTools || planCfg.updateAll,
		UpdateRuntimes: planCfg.updateRuntimes || planCfg.updateAll,
	}
	result, err := resolvePlan(resources, updateCfg, systemMode)
	if err != nil {
		return err
	}

	// Inject disabled resource info into the plan
	addDisabledResourceInfo(result.resourceInfo, disabledResources)

	// Output based on format
	switch planCfg.outputFormat {
	case outputJSON:
		exporter := graph.NewExporter(result.filteredLayers, result.resourceInfo, result.edges)
		return exporter.ExportJSON(os.Stdout)
	case "yaml":
		exporter := graph.NewExporter(result.filteredLayers, result.resourceInfo, result.edges)
		return exporter.ExportYAML(os.Stdout)
	case outputText:
		fallthrough
	default:
		return printTextPlan(cmd, args, resources, result)
	}
}

func buildResourceInfo(resources []resource.Resource, updCfg engine.UpdateConfig, system bool) map[graph.NodeID]graph.ResourceInfo {
	info := make(map[graph.NodeID]graph.ResourceInfo)

	// Load config and state
	var userState *state.UserState
	var pathConfig *path.Paths
	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err == nil {
		pc, err := path.NewFromConfig(cfg)
		if err == nil {
			pathConfig = pc
			store, err := state.NewStore[state.UserState](pathConfig.UserDataDir())
			if err == nil {
				loaded, err := store.LoadReadOnly()
				if err == nil {
					userState = loaded
				}
			}
		}
	}

	// Fall back to default paths when config is unavailable, so system
	// resource plans can still compute SystemDataDir accurately.
	if pathConfig == nil {
		if pc, err := path.New(); err == nil {
			pathConfig = pc
		}
	}

	if userState == nil {
		fmt.Fprintln(os.Stderr, "Warning: tomei is not initialized. Run 'tomei init' for accurate state comparison.")
	}

	// Apply taint logic based on update flags (for plan preview)
	if userState != nil {
		engine.ApplyUpdateTaints(userState, updCfg)
	}

	for _, res := range resources {
		nodeID := graph.NewNodeID(res.Kind(), res.Name())

		// System resources are added separately via addSystemResourceInfo using
		// the read-only store and reconcilers, so skip them in this per-resource loop.
		if resource.IsSystemKind(res.Kind()) {
			continue
		}

		resInfo := graph.ResourceInfo{
			Kind:   res.Kind(),
			Name:   res.Name(),
			Action: resource.ActionInstall, // default to install
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
					if rt.IsTainted() {
						resInfo.Action = resource.ActionReinstall
					} else if rt.Version == resInfo.Version {
						resInfo.Action = resource.ActionNone
					} else {
						resInfo.Action = resource.ActionUpgrade
					}
				}
			case resource.KindTool:
				if tool, ok := userState.Tools[res.Name()]; ok {
					if tool.IsTainted() {
						resInfo.Action = resource.ActionReinstall
					} else if tool.Version == resInfo.Version {
						resInfo.Action = resource.ActionNone
					} else {
						resInfo.Action = resource.ActionUpgrade
					}
				}
			case resource.KindInstaller:
				// Installers don't have versions in state typically
				resInfo.Action = resource.ActionNone
			}
		}

		// Mark privileged tools as skip when --system is not set
		if !system && resource.IsPrivileged(res) {
			resInfo.Action = resource.ActionSkip
		}

		info[nodeID] = resInfo
	}

	// Handle system resources
	if system && pathConfig != nil {
		addSystemResourceInfo(info, resources, pathConfig)
	} else {
		// Without --system, mark all system resources as skip
		for _, res := range resources {
			if resource.IsSystemKind(res.Kind()) {
				nodeID := graph.NewNodeID(res.Kind(), res.Name())
				info[nodeID] = graph.ResourceInfo{
					Kind:   res.Kind(),
					Name:   res.Name(),
					Action: resource.ActionSkip,
				}
			}
		}
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
					Action:  resource.ActionRemove,
				}
			}
		}
		for name, tool := range userState.Tools {
			nodeID := graph.NewNodeID(resource.KindTool, name)
			if _, exists := info[nodeID]; !exists {
				action := resource.ActionRemove
				// Privileged tool removals require --system; without it, skip.
				if !system && tool.Privileged {
					action = resource.ActionSkip
				}
				info[nodeID] = graph.ResourceInfo{
					Kind:    resource.KindTool,
					Name:    name,
					Version: tool.Version,
					Action:  action,
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
				if ri, ok := info[nodeID]; ok && (ri.Action == resource.ActionUpgrade || ri.Action == resource.ActionReinstall) {
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
				if ri, ok := info[nodeID]; ok && ri.Action == resource.ActionNone {
					ri.Action = resource.ActionReinstall
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
	printer := graph.NewTreePrinter(cmd.OutOrStdout(), planCfg.noColor)
	printer.PrintTree(result.resolver, result.resourceInfo)

	// Print execution layers
	printer.PrintLayers(result.filteredLayers, result.resourceInfo)

	// Print disabled resources
	disabledInfos := collectSkipInfos(result.resourceInfo)
	if len(disabledInfos) > 0 {
		printer.PrintDisabled(disabledInfos)
	}

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
func resolvePlan(resources []resource.Resource, updateCfg engine.UpdateConfig, system bool) (*planResult, error) {
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

	resourceInfo := buildResourceInfo(resources, updateCfg, system)

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
func planForResources(w io.Writer, resources []resource.Resource, disableColor bool, updateCfg engine.UpdateConfig, system bool) (bool, error) {
	result, err := resolvePlan(resources, updateCfg, system)
	if err != nil {
		return false, err
	}

	// Count only real change actions; ActionNone and ActionSkip don't run
	// anything, so they shouldn't trigger a confirmation prompt in apply.
	hasChanges := false
	for _, info := range result.resourceInfo {
		switch info.Action {
		case resource.ActionInstall, resource.ActionUpgrade, resource.ActionReinstall, resource.ActionRemove:
			hasChanges = true
		}
		if hasChanges {
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

// addDisabledResourceInfo injects disabled resources into the resource info map.
// Resources that already have an entry (e.g., ActionRemove for previously installed) are not overwritten.
func addDisabledResourceInfo(info map[graph.NodeID]graph.ResourceInfo, disabled []resource.Resource) {
	for _, res := range disabled {
		nodeID := graph.NewNodeID(res.Kind(), res.Name())
		// Do not overwrite existing entries (e.g., ActionRemove)
		if _, exists := info[nodeID]; exists {
			continue
		}
		ri := graph.ResourceInfo{
			Kind:   res.Kind(),
			Name:   res.Name(),
			Action: resource.ActionSkip,
		}
		if tool, ok := res.(*resource.Tool); ok && tool.ToolSpec != nil {
			ri.Version = tool.ToolSpec.Version
		}
		info[nodeID] = ri
	}
}

// collectSkipInfos extracts ActionSkip entries from resourceInfo, sorted by kind then name.
func collectSkipInfos(resourceInfo map[graph.NodeID]graph.ResourceInfo) []graph.ResourceInfo {
	var infos []graph.ResourceInfo
	for _, info := range resourceInfo {
		if info.Action == resource.ActionSkip {
			infos = append(infos, info)
		}
	}
	slices.SortFunc(infos, func(a, b graph.ResourceInfo) int {
		if c := cmp.Compare(string(a.Kind), string(b.Kind)); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return infos
}

// addSystemResourceInfo computes accurate actions for system resources
// using reconcilers and LoadReadOnly (no lock, no filesystem side effects)
// and merges them into the info map.
func addSystemResourceInfo(info map[graph.NodeID]graph.ResourceInfo, resources []resource.Resource, pathConfig *path.Paths) {
	// Check if system state directory exists before creating a store.
	// plan is a read-only command and should not create directories.
	systemDir := pathConfig.SystemDataDir()
	if _, err := os.Stat(systemDir); os.IsNotExist(err) {
		// No system state yet (first run) — use install fallback for supported
		// system installers and skip unsupported system resource kinds.
		markAllSystemAsInstall(info, resources)
		return
	}

	store, err := state.NewStore[state.SystemState](systemDir)
	if err != nil {
		slog.Warn("failed to create system state store for plan", "error", err)
		markAllSystemAsInstall(info, resources)
		return
	}

	// Use LoadReadOnly to avoid acquiring the state lock.
	// plan should have no filesystem side effects.
	st, err := store.LoadReadOnly()
	if err != nil {
		slog.Warn("failed to load system state for plan", "error", err)
		markAllSystemAsInstall(info, resources)
		return
	}

	// Extract system resources by kind
	var installers []*resource.SystemInstaller
	var repos []*resource.SystemPackageRepository
	var packages []*resource.SystemPackageSet
	for _, res := range resources {
		switch r := res.(type) {
		case *resource.SystemInstaller:
			installers = append(installers, r)
		case *resource.SystemPackageRepository:
			repos = append(repos, r)
		case *resource.SystemPackageSet:
			packages = append(packages, r)
		}
	}

	// Reconcile each resource type directly (reconcilers are stateless)
	installerActions := reconciler.NewSystemInstallerReconciler().Reconcile(installers, st.SystemInstallers)
	repoActions := reconciler.NewSystemPackageRepositoryReconciler().Reconcile(repos, st.SystemPackageRepositories)
	pkgActions := reconciler.NewSystemPackageSetReconciler().Reconcile(packages, st.SystemPackages)

	convertActions[*resource.SystemInstaller, *resource.SystemInstallerState](info, resource.KindSystemInstaller, installerActions)
	convertActions[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState](info, resource.KindSystemPackageRepository, repoActions)
	convertActions[*resource.SystemPackageSet, *resource.SystemPackageSetState](info, resource.KindSystemPackageSet, pkgActions)

	// Mark SystemPackageSet resources as skip when they would require actions,
	// since the concrete installer is not yet implemented (#198).
	// SystemPackageRepository now has a concrete installer (#196), so it falls
	// through with the reconciler-determined action.
	for nodeID, ri := range info {
		if ri.Kind == resource.KindSystemPackageSet {
			if ri.Action != resource.ActionNone {
				ri.Action = resource.ActionSkip
				info[nodeID] = ri
			}
		}
	}
}

// markAllSystemAsInstall marks all system resources as ActionInstall (first-run fallback).
func markAllSystemAsInstall(info map[graph.NodeID]graph.ResourceInfo, resources []resource.Resource) {
	for _, res := range resources {
		if resource.IsSystemKind(res.Kind()) {
			nodeID := graph.NewNodeID(res.Kind(), res.Name())
			action := resource.ActionInstall
			// SystemPackageSet not yet implemented (#198) — skip.
			// SystemInstaller and SystemPackageRepository have concrete installers.
			if res.Kind() == resource.KindSystemPackageSet {
				action = resource.ActionSkip
			}
			info[nodeID] = graph.ResourceInfo{
				Kind:   res.Kind(),
				Name:   res.Name(),
				Action: action,
			}
		}
	}
}

// convertActions converts reconciler actions to graph.ResourceInfo entries.
func convertActions[R resource.Resource, S resource.State](
	info map[graph.NodeID]graph.ResourceInfo,
	kind resource.Kind,
	actions []reconciler.Action[R, S],
) {
	for _, action := range actions {
		nodeID := graph.NewNodeID(kind, action.Name)
		info[nodeID] = graph.ResourceInfo{
			Kind:   kind,
			Name:   action.Name,
			Action: action.Type,
		}
	}
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
