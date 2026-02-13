package cue

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/terassyi/tomei/internal/cuemod"
)

var (
	initModuleName string
	initForce      bool
)

var initCmd = &cobra.Command{
	Use:   "init [dir]",
	Short: "Initialize a CUE module for tomei manifests",
	Long: `Initialize a CUE module directory structure for use with tomei.

Creates:
  cue.mod/module.cue    - CUE module declaration with tomei dependency
  tomei_platform.cue    - Platform @tag() declarations for tomei apply

The generated module enables:
  - CUE LSP and cue eval to resolve tomei imports
  - tomei apply to resolve imports via OCI registry
  - Standard CUE module ecosystem integration

Usage:
  tomei cue init              Initialize in current directory
  tomei cue init ./manifests  Initialize in specified directory`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringVar(&initModuleName, "module-name", cuemod.DefaultModuleName, "CUE module name")
	initCmd.Flags().BoolVar(&initForce, "force", false, "Overwrite existing files")
}

func runInit(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	if err := os.MkdirAll(absDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate cue.mod/module.cue
	moduleCue, err := cuemod.GenerateModuleCUE(initModuleName)
	if err != nil {
		return err
	}
	cueModDir := filepath.Join(absDir, "cue.mod")
	moduleCuePath := filepath.Join(cueModDir, "module.cue")
	if err := cuemod.WriteFileIfAllowed(moduleCuePath, moduleCue, initForce); err != nil {
		return err
	}
	cmd.Printf("Created %s\n", cuemod.RelativePath(absDir, moduleCuePath))

	// Generate tomei_platform.cue
	platformCue, err := cuemod.GeneratePlatformCUE()
	if err != nil {
		return err
	}
	platformCuePath := filepath.Join(absDir, "tomei_platform.cue")
	if err := cuemod.WriteFileIfAllowed(platformCuePath, platformCue, initForce); err != nil {
		return err
	}
	cmd.Printf("Created %s\n", cuemod.RelativePath(absDir, platformCuePath))

	cmd.Println()
	cmd.Println("  CUE tooling (cue eval, LSP) requires:")
	cmd.Println("    eval $(tomei env)")

	return nil
}
