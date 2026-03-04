package cue

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/terassyi/tomei/internal/cuemod"
)

var updateDryRun bool

var updateCmd = &cobra.Command{
	Use:   "update [dir]",
	Short: "Update tomei module dependencies to the latest version",
	Long: `Update tomei module dependencies in cue.mod/module.cue to the latest
published version from the OCI registry.

Scans the deps block for first-party tomei.terassyi.net dependencies and
updates their version to the latest available.

Usage:
  tomei cue update              Update in current directory
  tomei cue update ./manifests  Update in specified directory
  tomei cue update --dry-run    Show what would be updated without writing`,
	Args: cobra.MaximumNArgs(1),
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().BoolVar(&updateDryRun, "dry-run", false, "Show updates without writing changes")
}

func runUpdate(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	cueModDir := filepath.Join(absDir, "cue.mod")

	// Parse existing module.cue
	f, err := cuemod.ParseModuleFile(cueModDir)
	if err != nil {
		return err
	}

	// Resolve latest version from OCI registry
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	latestVersion, err := cuemod.ResolveLatestVersion(ctx)
	if err != nil {
		return fmt.Errorf("failed to resolve latest module version: %w", err)
	}

	// Update deps
	results, err := cuemod.UpdateDeps(f, latestVersion)
	if err != nil {
		return err
	}

	// Display results
	for _, r := range results {
		if r.Updated {
			cmd.Printf("%s: %s -> %s\n", r.ModulePath, r.OldVersion, r.NewVersion)
		} else {
			cmd.Printf("%s: already at latest (%s)\n", r.ModulePath, r.OldVersion)
		}
	}

	if !cuemod.AnyUpdated(results) {
		return nil
	}

	if updateDryRun {
		return nil
	}

	// Format and write
	data, err := cuemod.FormatModuleFile(f)
	if err != nil {
		return err
	}

	if err := cuemod.WriteModuleFileAtomic(cueModDir, data); err != nil {
		return err
	}

	cmd.Printf("\nUpdated %s\n", cuemod.RelativePath(absDir, filepath.Join(cueModDir, "module.cue")))

	// Hint about vendored modules
	if cuemod.HasVendoredModules(absDir) {
		cmd.Println()
		cmd.Println("  Vendored modules detected. Run one of:")
		cmd.Println("    cue mod tidy")
		cmd.Println("    make vendor-cue")
	}

	return nil
}
