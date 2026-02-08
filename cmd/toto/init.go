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

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/github"
	"github.com/terassyi/toto/internal/path"
	"github.com/terassyi/toto/internal/registry/aqua"
	"github.com/terassyi/toto/internal/state"
	"github.com/terassyi/toto/internal/ui"
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
	forceInit   bool
	yesInit     bool
	initNoColor bool
)

func init() {
	initCmd.Flags().BoolVar(&forceInit, "force", false, "Force reinitialization (resets state.json)")
	initCmd.Flags().BoolVarP(&yesInit, "yes", "y", false, "Skip confirmation prompt and create config.cue with defaults")
	initCmd.Flags().BoolVar(&initNoColor, "no-color", false, "Disable color output")
}

func runInit(cmd *cobra.Command, _ []string) error {
	if initNoColor {
		color.NoColor = true
	}

	style := ui.NewStyle()

	cmd.Println("Initializing toto...")
	cmd.Println()

	// Get config directory (fixed to ~/.config/toto)
	cfgDir, err := path.Expand(config.DefaultConfigDir)
	if err != nil {
		return fmt.Errorf("failed to expand config directory: %w", err)
	}

	// Create config directory if it doesn't exist
	if err := path.EnsureDir(cfgDir); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

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

	// Check if already initialized
	stateFile := paths.UserStateFile()
	if _, err := os.Stat(stateFile); err == nil && !forceInit {
		cmd.Println("Already initialized. Use --force to reinitialize.")
		return nil
	}

	// Print directories section
	style.Header.Fprintln(cmd.OutOrStdout(), "Directories:")

	// Create directories
	dirs := []struct {
		path string
		name string
	}{
		{cfgDir, "config"},
		{paths.UserDataDir(), "data"},
		{filepath.Join(paths.UserDataDir(), "tools"), "tools"},
		{filepath.Join(paths.UserDataDir(), "runtimes"), "runtimes"},
		{paths.UserBinDir(), "bin"},
		{paths.EnvDir(), "env"},
	}

	for _, dir := range dirs {
		if err := path.EnsureDir(dir.path); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir.path, err)
		}
		cmd.Printf("  %s %s\n", style.SuccessMark, style.Path.Sprint(dir.path))
	}
	cmd.Println()

	// Initialize state.json
	style.Header.Fprintln(cmd.OutOrStdout(), "State:")
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

	cmd.Printf("  %s %s\n", style.SuccessMark, style.Path.Sprint(stateFile))
	cmd.Println()

	// Registry section
	style.Header.Fprintln(cmd.OutOrStdout(), "Registry:")
	if err := initRegistry(ctx, initialState); err != nil {
		// Log warning but don't fail init if registry initialization fails
		slog.Warn("failed to initialize aqua registry", "error", err)
		cmd.Printf("  %s aqua-registry (failed to fetch)\n", style.WarnMark)
	} else {
		cmd.Printf("  %s aqua-registry %s\n", style.SuccessMark, style.Path.Sprint(initialState.Registry.Aqua.Ref))
	}
	cmd.Println()

	if err := store.Save(initialState); err != nil {
		return fmt.Errorf("failed to initialize state: %w", err)
	}

	style.Success.Fprintln(cmd.OutOrStdout(), "Initialization complete!")
	cmd.Println()

	// Next steps
	style.Header.Fprintln(cmd.OutOrStdout(), "Next steps:")
	cmd.Printf("  %s Add %s to your PATH\n", style.Step.Sprint("1."), style.Path.Sprint(paths.UserBinDir()))
	cmd.Printf("  %s Create manifest files (tools.cue, runtime.cue)\n", style.Step.Sprint("2."))
	cmd.Printf("  %s Run '%s' to install\n", style.Step.Sprint("3."), style.Path.Sprint("toto apply ."))

	return nil
}

// initRegistry initializes the aqua-registry state by fetching the latest ref.
func initRegistry(ctx context.Context, st *state.UserState) error {
	ghClient := github.NewHTTPClient(github.TokenFromEnv())
	client := aqua.NewVersionClient(ghClient)

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

	slog.Debug("initialized aqua registry", "ref", ref)
	return nil
}
