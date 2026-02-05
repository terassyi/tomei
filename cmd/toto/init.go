package main

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/path"
	"github.com/terassyi/toto/internal/registry/aqua"
	"github.com/terassyi/toto/internal/state"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize toto directories and state",
	Long: `Initialize toto directories and state file.

Creates the following directories:
  - Config directory (~/.config/toto/)
  - Data directory (~/.local/share/toto/)
  - Tools directory (~/.local/share/toto/tools/)
  - Runtimes directory (~/.local/share/toto/runtimes/)
  - Bin directory (~/.local/bin/)

Also initializes an empty state.json file.

If config.cue does not exist, prompts to create one with default values.
Use --yes to skip the prompt and create config.cue automatically.`,
	RunE: runInit,
}

var (
	forceInit bool
	yesInit   bool
)

func init() {
	initCmd.Flags().BoolVar(&forceInit, "force", false, "Force reinitialization (resets state.json)")
	initCmd.Flags().BoolVarP(&yesInit, "yes", "y", false, "Skip confirmation prompt and create config.cue with defaults")
}

func runInit(cmd *cobra.Command, _ []string) error {
	// Get config directory (fixed to ~/.config/toto)
	cfgDir, err := path.Expand(config.DefaultConfigDir)
	if err != nil {
		return fmt.Errorf("failed to expand config directory: %w", err)
	}

	// Create config directory if it doesn't exist
	if err := path.EnsureDir(cfgDir); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	cmd.Printf("Config directory: %s\n", cfgDir)

	// Check if config.cue exists
	configFile := filepath.Join(cfgDir, "config.cue")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		// config.cue does not exist, prompt to create
		if !yesInit {
			cmd.Printf("config.cue not found. Create with default values? [y/N]: ")
			reader := bufio.NewReader(os.Stdin)
			answer, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read input: %w", err)
			}
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				cmd.Println("Aborted.")
				return nil
			}
		}

		// Write default config.cue
		cueContent, err := config.DefaultConfig().ToCue()
		if err != nil {
			return fmt.Errorf("failed to generate config.cue: %w", err)
		}
		if err := os.WriteFile(configFile, cueContent, 0644); err != nil {
			return fmt.Errorf("failed to write config.cue: %w", err)
		}
		cmd.Printf("Created: %s\n", configFile)
	}

	// Load config
	cfg, err := config.LoadConfig(cfgDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create paths from config
	paths, err := path.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create paths: %w", err)
	}

	cmd.Printf("Data directory: %s\n", paths.UserDataDir())
	cmd.Printf("Bin directory: %s\n", paths.UserBinDir())

	// Check if already initialized
	stateFile := paths.UserStateFile()
	if _, err := os.Stat(stateFile); err == nil && !forceInit {
		cmd.Println("Already initialized. Use --force to reinitialize.")
		return nil
	}

	// Create directories
	dirs := []string{
		paths.UserDataDir(),
		filepath.Join(paths.UserDataDir(), "tools"),
		filepath.Join(paths.UserDataDir(), "runtimes"),
		paths.UserBinDir(),
	}

	for _, dir := range dirs {
		if err := path.EnsureDir(dir); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
		cmd.Printf("Created: %s\n", dir)
	}

	// Initialize state.json
	store, err := state.NewStore[state.UserState](paths.UserDataDir())
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}

	if err := store.Lock(); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	defer func() { _ = store.Unlock() }()

	initialState := state.NewUserState()

	// Initialize aqua registry
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	if err := initRegistry(ctx, initialState); err != nil {
		// Log warning but don't fail init if registry initialization fails
		slog.Warn("failed to initialize aqua registry", "error", err)
		cmd.Printf("Warning: failed to initialize aqua registry: %v\n", err)
	} else {
		cmd.Printf("Aqua registry: %s\n", initialState.Registry.Aqua.Ref)
	}

	if err := store.Save(initialState); err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}
	cmd.Printf("Initialized: %s\n", stateFile)

	cmd.Println("Initialization complete.")
	return nil
}

// initRegistry initializes the aqua-registry state by fetching the latest ref.
func initRegistry(ctx context.Context, st *state.UserState) error {
	client := aqua.NewVersionClient()

	ref, err := client.GetLatestRef(ctx)
	if err != nil {
		return fmt.Errorf("failed to get latest aqua registry ref: %w", err)
	}

	st.Registry = &state.RegistryState{
		Aqua: &state.AquaRegistryState{
			Ref:       ref,
			UpdatedAt: time.Now(),
		},
	}

	slog.Info("initialized aqua registry", "ref", ref)
	return nil
}
