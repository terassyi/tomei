package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

const (
	defaultModuleName = "manifests.local@v0"
	defaultModuleVer  = "v0.0.1"

	cueLanguageVersion = "v0.9.0"
	tomeiModulePath    = "tomei.terassyi.net@v0"
)

var (
	cueInitModuleName string
	cueInitForce      bool
)

var cueInitCmd = &cobra.Command{
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
	RunE: runCueInit,
}

func init() {
	cueInitCmd.Flags().StringVar(&cueInitModuleName, "module-name", defaultModuleName, "CUE module name")
	cueInitCmd.Flags().BoolVar(&cueInitForce, "force", false, "Overwrite existing files")
}

func runCueInit(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Ensure the target directory exists
	if err := os.MkdirAll(absDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate cue.mod/module.cue
	cueModDir := filepath.Join(absDir, "cue.mod")
	moduleCuePath := filepath.Join(cueModDir, "module.cue")

	if err := writeFileIfAllowed(moduleCuePath, generateModuleCUE(cueInitModuleName), cueInitForce); err != nil {
		return err
	}
	cmd.Printf("Created %s\n", relativePath(absDir, moduleCuePath))

	// Generate tomei_platform.cue
	platformCuePath := filepath.Join(absDir, "tomei_platform.cue")
	if err := writeFileIfAllowed(platformCuePath, generatePlatformCUE(), cueInitForce); err != nil {
		return err
	}
	cmd.Printf("Created %s\n", relativePath(absDir, platformCuePath))

	cmd.Println()
	cmd.Println("  CUE tooling (cue eval, LSP) requires:")
	cmd.Println("    eval $(tomei env)")

	return nil
}

// generateModuleCUE generates the cue.mod/module.cue content.
func generateModuleCUE(moduleName string) []byte {
	content := fmt.Sprintf(`module: %q
language: version: %q
deps: {
	%q: v: %q
}
`, moduleName, cueLanguageVersion, tomeiModulePath, defaultModuleVer)
	return []byte(content)
}

// generatePlatformCUE generates the tomei_platform.cue content.
func generatePlatformCUE() []byte {
	content := `package tomei

// Platform values resolved by tomei apply.
// For cue eval: cue eval -t os=linux -t arch=amd64
_os:       string @tag(os)
_arch:     string @tag(arch)
_headless: bool | *false @tag(headless,type=bool)
`
	return []byte(content)
}

// writeFileIfAllowed writes content to path, creating parent directories.
// Returns an error if the file exists and force is false.
func writeFileIfAllowed(path string, content []byte, force bool) error {
	if !force {
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("failed to create directory for %s: %w", path, err)
	}

	if err := os.WriteFile(path, content, 0644); err != nil {
		return fmt.Errorf("failed to write %s: %w", path, err)
	}

	return nil
}

// relativePath returns the relative path from base to target, or the absolute path on error.
func relativePath(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}
	return rel
}
