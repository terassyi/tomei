package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/installer/download"
	"github.com/terassyi/toto/internal/installer/engine"
	"github.com/terassyi/toto/internal/installer/place"
	"github.com/terassyi/toto/internal/installer/runtime"
	"github.com/terassyi/toto/internal/installer/tool"
	"github.com/terassyi/toto/internal/path"
	"github.com/terassyi/toto/internal/state"
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply the configuration",
	Long: `Apply the configuration to install, upgrade, or remove resources.

For user-level resources (Runtime, Tool, ToolSet):
  toto apply

For system-level resources (SystemPackageRepository, SystemPackageSet):
  sudo toto apply --system

It is recommended to run system-level apply first, then user-level.`,
	RunE: runApply,
}

func runApply(cmd *cobra.Command, _ []string) error {
	dir, err := getConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	if systemMode {
		cmd.Printf("Applying system-level resources from %s\n", dir)
		// TODO: implement system apply in Phase 4
		cmd.Println("System apply not yet implemented")
		return nil
	}

	cmd.Printf("Applying user-level resources from %s\n", dir)
	return runUserApply(cmd.Context(), dir)
}

func runUserApply(ctx context.Context, configDir string) error {
	// Load config
	cfg, err := config.LoadConfig(configDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Setup paths from config
	paths, err := path.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize paths: %w", err)
	}

	// Ensure directories exist
	if err := path.EnsureDir(paths.UserDataDir()); err != nil {
		return fmt.Errorf("failed to create data directory: %w", err)
	}
	if err := path.EnsureDir(paths.UserBinDir()); err != nil {
		return fmt.Errorf("failed to create bin directory: %w", err)
	}

	// Create state store
	store, err := state.NewStore[state.UserState](paths.UserDataDir())
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}

	// Create installers
	downloader := download.NewDownloader()
	toolsDir := paths.UserDataDir() + "/tools"
	runtimesDir := paths.UserDataDir() + "/runtimes"
	binDir := paths.UserBinDir()

	placer := place.NewPlacer(toolsDir, binDir)
	toolInstaller := tool.NewInstaller(downloader, placer)
	runtimeInstaller := runtime.NewInstaller(downloader, runtimesDir, binDir)

	// Create and run engine with runtime support
	eng := engine.NewEngine(toolInstaller, runtimeInstaller, store)
	if err := eng.Apply(ctx, configDir); err != nil {
		return fmt.Errorf("apply failed: %w", err)
	}

	return nil
}
