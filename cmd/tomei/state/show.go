package state

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/path"
	internalstate "github.com/terassyi/tomei/internal/state"
)

var showCmd = &cobra.Command{
	Use:   "show",
	Short: "Display the current state",
	Long: `Display the raw state.json content.

Shows all installed resources, runtimes, tools, and registries
currently tracked by tomei.`,
	RunE: runShow,
}

func runShow(cmd *cobra.Command, _ []string) error {
	// Load config
	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create paths
	paths, err := path.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create paths: %w", err)
	}

	// Create store
	store, err := internalstate.NewStore[internalstate.UserState](paths.UserDataDir())
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}

	// Load current state (read-only, no lock)
	current, err := store.LoadReadOnly()
	if err != nil {
		return fmt.Errorf("failed to load current state: %w", err)
	}

	// Print state file path to stderr so JSON output stays clean for piping
	cmd.PrintErrln("State file:", store.StatePath())

	data, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}
	cmd.Println(string(data))
	return nil
}
