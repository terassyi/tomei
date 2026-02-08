package state

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/path"
	internalstate "github.com/terassyi/toto/internal/state"
	"github.com/terassyi/toto/internal/ui"
)

var (
	diffOutput  string
	diffNoColor bool
)

var diffCmd = &cobra.Command{
	Use:   "diff",
	Short: "Show changes since last apply",
	Long: `Compare current state with the backup created before the last apply.

A backup is automatically created at the start of each "toto apply".
This command shows what changed during the most recent apply.`,
	RunE: runDiff,
}

func init() {
	diffCmd.Flags().StringVarP(&diffOutput, "output", "o", "text", "Output format: text, json")
	diffCmd.Flags().BoolVar(&diffNoColor, "no-color", false, "Disable colored output")
}

func runDiff(cmd *cobra.Command, _ []string) error {
	if diffNoColor {
		color.NoColor = true
	}

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

	// Load backup state
	backup, err := internalstate.LoadBackup[internalstate.UserState](store.StatePath())
	if err != nil {
		return fmt.Errorf("failed to load backup state: %w", err)
	}
	if backup == nil {
		cmd.Println("No backup found. Run 'toto apply' first.")
		return nil
	}

	// Compute diff
	diff := internalstate.DiffUserStates(backup, current)

	// Output
	switch diffOutput {
	case "json":
		return printDiffJSON(cmd, diff)
	case "text":
		fallthrough
	default:
		printDiffText(cmd, diff)
		return nil
	}
}

func printDiffJSON(cmd *cobra.Command, diff *internalstate.Diff) error {
	data, err := json.MarshalIndent(diff, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal diff: %w", err)
	}
	cmd.Println(string(data))
	return nil
}

func printDiffText(cmd *cobra.Command, diff *internalstate.Diff) {
	if !diff.HasChanges() {
		cmd.Println("No changes since last apply.")
		return
	}

	style := ui.NewStyle()

	style.Header.Fprintln(cmd.OutOrStdout(), "State changes (last apply):")
	cmd.Println()

	// Group changes by kind
	groups := map[string][]internalstate.ResourceDiff{}
	for _, c := range diff.Changes {
		groups[c.Kind] = append(groups[c.Kind], c)
	}

	// Print in order: registry, runtimes, tools, installerRepositories
	kindOrder := []string{"registry", "runtime", "tool", "installerRepository"}
	kindLabels := map[string]string{
		"registry":            "Registry",
		"runtime":             "Runtimes",
		"tool":                "Tools",
		"installerRepository": "Installer Repositories",
	}

	for _, kind := range kindOrder {
		changes, ok := groups[kind]
		if !ok {
			continue
		}

		cmd.Printf("  %s:\n", kindLabels[kind])
		for _, c := range changes {
			printResourceDiff(cmd, style, c)
		}
		cmd.Println()
	}

	// Summary
	added, modified, removed := diff.Summary()
	var parts []string
	if added > 0 {
		parts = append(parts, color.New(color.FgGreen).Sprintf("%d added", added))
	}
	if modified > 0 {
		parts = append(parts, color.New(color.FgYellow).Sprintf("%d modified", modified))
	}
	if removed > 0 {
		parts = append(parts, color.New(color.FgRed).Sprintf("%d removed", removed))
	}
	cmd.Printf("Summary: %s\n", strings.Join(parts, ", "))
}

func printResourceDiff(cmd *cobra.Command, style *ui.Style, c internalstate.ResourceDiff) {
	switch c.Type {
	case internalstate.DiffAdded:
		cmd.Printf("    %s %s %s\n",
			color.New(color.FgGreen).Sprint("+"),
			c.Name,
			c.NewVersion,
		)
	case internalstate.DiffRemoved:
		cmd.Printf("    %s %s %s\n",
			style.RemoveMark,
			c.Name,
			c.OldVersion,
		)
	case internalstate.DiffModified:
		if c.OldVersion != "" && c.NewVersion != "" && c.OldVersion != c.NewVersion {
			cmd.Printf("    %s %s %s → %s\n",
				style.UpgradeMark,
				c.Name,
				c.OldVersion,
				c.NewVersion,
			)
		} else {
			cmd.Printf("    %s %s (modified)\n",
				style.UpgradeMark,
				c.Name,
			)
		}
	}
}
