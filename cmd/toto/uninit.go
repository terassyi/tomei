package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/path"
)

var uninitCmd = &cobra.Command{
	Use:   "uninit",
	Short: "Remove toto directories and state",
	Long: `Remove toto directories, state files, and managed symlinks.

This command removes:
  - Config directory (~/.config/toto/)
  - Data directory (~/.local/share/toto/)
  - Symlinks in bin directory (~/.local/bin/) that point to toto-managed tools

The bin directory itself is preserved as it may contain non-toto files.

Use --keep-config to preserve the config directory.
Use --dry-run to see what would be removed without actually removing.`,
	RunE: runUninit,
}

var (
	uninitYes        bool
	uninitKeepConfig bool
	uninitDryRun     bool
	uninitNoColor    bool
)

func init() {
	uninitCmd.Flags().BoolVarP(&uninitYes, "yes", "y", false, "Skip confirmation prompt")
	uninitCmd.Flags().BoolVar(&uninitKeepConfig, "keep-config", false, "Preserve config directory")
	uninitCmd.Flags().BoolVar(&uninitDryRun, "dry-run", false, "Show what would be removed without removing")
	uninitCmd.Flags().BoolVar(&uninitNoColor, "no-color", false, "Disable color output")
}

func runUninit(cmd *cobra.Command, _ []string) error {
	if uninitNoColor {
		color.NoColor = true
	}

	style := newOutputStyle()

	// Get config directory
	cfgDir, err := path.Expand(config.DefaultConfigDir)
	if err != nil {
		return fmt.Errorf("failed to expand config directory: %w", err)
	}

	// Check if toto is initialized by looking for state.json
	paths, err := path.New()
	if err != nil {
		return fmt.Errorf("failed to get paths: %w", err)
	}

	stateFile := paths.UserStateFile()
	if _, err := os.Stat(stateFile); os.IsNotExist(err) {
		cmd.Println("toto is not initialized. Nothing to remove.")
		return nil
	}

	// Load config to get actual paths
	cfg, err := config.LoadConfig(cfgDir)
	if err != nil {
		// If config load fails, use defaults
		cfg = config.DefaultConfig()
	}

	paths, err = path.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create paths from config: %w", err)
	}

	dataDir := paths.UserDataDir()
	binDir := paths.UserBinDir()

	// Find managed symlinks
	symlinks, err := findManagedSymlinks(binDir, dataDir)
	if err != nil {
		// Non-fatal: just warn and continue
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: failed to scan symlinks: %v\n", err)
		symlinks = nil
	}

	// Print header
	if uninitDryRun {
		cmd.Println("Uninitializing toto... (dry-run)")
	} else {
		cmd.Println("Uninitializing toto...")
	}
	cmd.Println()

	// Show what will be removed
	if uninitDryRun {
		style.header.Fprintln(cmd.OutOrStdout(), "Would remove:")
	} else {
		style.header.Fprintln(cmd.OutOrStdout(), "This will remove:")
	}

	for _, link := range symlinks {
		target, _ := os.Readlink(link)
		cmd.Printf("  %s -> %s\n", style.path.Sprint(link), target)
	}
	cmd.Printf("  %s\n", style.path.Sprint(dataDir))
	if !uninitKeepConfig {
		cmd.Printf("  %s\n", style.path.Sprint(cfgDir))
	}
	cmd.Println()

	if uninitKeepConfig {
		style.header.Fprintln(cmd.OutOrStdout(), "Config will be preserved:")
		cmd.Printf("  %s\n", style.path.Sprint(cfgDir))
		cmd.Println()
	}

	// Dry-run ends here
	if uninitDryRun {
		cmd.Println("No changes made.")
		return nil
	}

	// Confirmation prompt
	if !uninitYes {
		cmd.Print("Proceed? [y/N]: ")
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

	cmd.Println()
	style.header.Fprintln(cmd.OutOrStdout(), "Removing:")

	// Remove symlinks first
	for _, link := range symlinks {
		target, _ := os.Readlink(link)
		if err := os.Remove(link); err != nil {
			cmd.Printf("  %s %s -> %s (%v)\n", style.failMark, link, target, err)
		} else {
			cmd.Printf("  %s %s -> %s\n", style.successMark, style.path.Sprint(link), target)
		}
	}

	// Remove data directory
	if err := os.RemoveAll(dataDir); err != nil {
		cmd.Printf("  %s %s (%v)\n", style.failMark, dataDir, err)
	} else {
		cmd.Printf("  %s %s\n", style.successMark, style.path.Sprint(dataDir))
	}

	// Remove config directory (unless --keep-config)
	if !uninitKeepConfig {
		if err := os.RemoveAll(cfgDir); err != nil {
			cmd.Printf("  %s %s (%v)\n", style.failMark, cfgDir, err)
		} else {
			cmd.Printf("  %s %s\n", style.successMark, style.path.Sprint(cfgDir))
		}
	}

	cmd.Println()
	style.success.Fprintln(cmd.OutOrStdout(), "Uninitialization complete!")
	cmd.Println()
	cmd.Printf("Note: %s directory was preserved.\n", style.path.Sprint(binDir))

	return nil
}

// findManagedSymlinks finds symlinks in binDir that point to files under dataDir.
func findManagedSymlinks(binDir, dataDir string) ([]string, error) {
	var symlinks []string

	entries, err := os.ReadDir(binDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink == 0 {
			continue
		}

		linkPath := filepath.Join(binDir, entry.Name())
		target, err := os.Readlink(linkPath)
		if err != nil {
			continue
		}

		// Resolve relative symlinks
		if !filepath.IsAbs(target) {
			target = filepath.Join(binDir, target)
		}

		// Check if target points to toto-managed directory
		if strings.HasPrefix(target, dataDir) {
			symlinks = append(symlinks, linkPath)
		}
	}

	return symlinks, nil
}
