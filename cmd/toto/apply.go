package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/installer/download"
	"github.com/terassyi/toto/internal/installer/engine"
	"github.com/terassyi/toto/internal/installer/place"
	"github.com/terassyi/toto/internal/installer/runtime"
	"github.com/terassyi/toto/internal/installer/tool"
	"github.com/terassyi/toto/internal/path"
	"github.com/terassyi/toto/internal/registry/aqua"
	"github.com/terassyi/toto/internal/state"
)

var applyCmd = &cobra.Command{
	Use:   "apply <files or directories...>",
	Short: "Apply the configuration",
	Long: `Apply the configuration to install, upgrade, or remove resources.

For user-level resources (Runtime, Tool, ToolSet):
  toto apply .
  toto apply tools.cue runtime.cue
  toto apply ~/.config/toto/

For system-level resources (SystemPackageRepository, SystemPackageSet):
  sudo toto apply --system .`,
	Args: cobra.MinimumNArgs(1),
	RunE: runApply,
}

var (
	syncRegistry  bool
	applyNoColor  bool
	applyNoOutput bool
)

func init() {
	applyCmd.Flags().BoolVar(&syncRegistry, "sync", false, "Sync aqua registry to latest version before apply")
	applyCmd.Flags().BoolVar(&applyNoColor, "no-color", false, "Disable colored output")
	applyCmd.Flags().BoolVar(&applyNoOutput, "quiet", false, "Suppress progress output")
}

func runApply(cmd *cobra.Command, args []string) error {
	if applyNoColor {
		color.NoColor = true
	}

	if systemMode {
		cmd.Printf("Applying system-level resources from %v\n", args)
		// TODO: implement system apply in Phase 4
		cmd.Println("System apply not yet implemented")
		return nil
	}

	cmd.Printf("Applying user-level resources from %v\n", args)
	return runUserApply(cmd.Context(), args, cmd.OutOrStdout())
}

func runUserApply(ctx context.Context, paths []string, w io.Writer) error {
	// Load resources from paths (manifests)
	loader := config.NewLoader(nil)
	resources, err := loader.LoadPaths(paths)
	if err != nil {
		return fmt.Errorf("failed to load resources: %w", err)
	}

	// Load config from fixed path (~/.config/toto/config.cue)
	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup paths from config
	pathConfig, err := path.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize paths: %w", err)
	}

	// Ensure directories exist
	if err := path.EnsureDir(pathConfig.UserDataDir()); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	if err := path.EnsureDir(pathConfig.UserBinDir()); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Create state store
	store, err := state.NewStore[state.UserState](pathConfig.UserDataDir())
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}

	// Sync registry if --sync flag is set
	if syncRegistry {
		if err := aqua.SyncRegistry(ctx, store); err != nil {
			slog.Warn("failed to sync aqua registry", "error", err)
		}
	}

	// Create installers
	downloader := download.NewDownloader()
	toolsDir := pathConfig.UserDataDir() + "/tools"
	runtimesDir := pathConfig.UserDataDir() + "/runtimes"
	binDir := pathConfig.UserBinDir()

	placer := place.NewPlacer(toolsDir, binDir)
	toolInstaller := tool.NewInstaller(downloader, placer)
	runtimeInstaller := runtime.NewInstaller(downloader, runtimesDir)

	// Create engine with event handler for progress display
	eng := engine.NewEngine(toolInstaller, runtimeInstaller, store)

	// Track results for summary
	results := &applyResults{}

	// Create progress manager for download progress bars
	pm := newProgressManager(w)
	defer pm.Wait()

	// Set event handler for progress display
	if !applyNoOutput {
		eng.SetEventHandler(func(event engine.Event) {
			pm.handleEvent(event, results)
		})
	}

	// Set resolver configurer to be called after lock is acquired and state is loaded
	cacheDir := pathConfig.UserCacheDir() + "/registry/aqua"
	eng.SetResolverConfigurer(func(st *state.UserState) error {
		if st.Registry != nil && st.Registry.Aqua != nil {
			resolver := aqua.NewResolver(cacheDir)
			toolInstaller.SetResolver(resolver, aqua.RegistryRef(st.Registry.Aqua.Ref))
			slog.Debug("configured aqua-registry resolver", "ref", st.Registry.Aqua.Ref)
		}
		return nil
	})

	// Run engine
	if err := eng.Apply(ctx, resources); err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}

	// Print summary
	if !applyNoOutput {
		printApplySummary(w, results)
	}

	return nil
}
