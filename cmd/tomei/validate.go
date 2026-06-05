package main

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/graph"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/ui"
)

var validateNoColor bool

var validateCmd = &cobra.Command{
	Use:   "validate <files or directories...>",
	Short: "Validate the configuration",
	Long: `Validate CUE manifests without applying changes.

Checks for:
  - CUE syntax errors and schema conformance (types, required fields)
  - Spec-level validation (required fields, mutual exclusivity, basic structure)
  - Circular dependency detection in the resource DAG`,
	Args: cobra.MinimumNArgs(1),
	RunE: runValidate,
}

func init() {
	validateCmd.Flags().BoolVar(&validateNoColor, "no-color", false, "Disable colored output")
}

func runValidate(cmd *cobra.Command, args []string) error {
	if validateNoColor {
		color.NoColor = true
	}

	style := ui.NewStyle()

	cmd.Println("Validating configuration...")
	cmd.Println()

	loader := config.NewLoader(nil)
	resources, err := loader.LoadPaths(args)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Expand set resources (ToolSet, etc.) into individual resources
	resources, err = resource.ExpandSets(resources)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Reject manifests where the same OS package is declared by more
	// than one SystemPackageSet. Without this gate, dropping one of the
	// overlapping sets would uninstall the package while the other
	// set's state still recorded it as installed — see
	// resource.ValidateSystemPackageSetOverlap for the full rationale.
	if err := resource.ValidateSystemPackageSetOverlap(resources); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Reject user-declared Installer/aqua or Installer/download whose
	// spec.type differs from the builtin's hard-coded type. Catches a
	// silent install-mechanism swap that AppendBuiltinInstallers would
	// otherwise allow via override-by-name.
	if err := resource.ValidateBuiltinInstallerOverrides(resources); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Validate each resource's spec
	cmd.Println("Resources:")
	validationFailed := false
	for _, res := range resources {
		if err := res.Spec().Validate(); err != nil {
			cmd.Printf("  %s %s - %v\n", style.FailMark, style.Path.Sprintf("%s/%s", res.Kind(), res.Name()), err)
			validationFailed = true
		} else {
			cmd.Printf("  %s %s\n", style.SuccessMark, style.Path.Sprintf("%s/%s", res.Kind(), res.Name()))
		}
	}
	cmd.Println()

	if validationFailed {
		cmd.Printf("%s Validation failed\n", style.FailMark)
		return fmt.Errorf("validation failed")
	}

	// Check for circular dependencies
	resolver := graph.NewResolver()
	for _, res := range resources {
		resolver.AddResource(res)
	}

	cmd.Println("Dependencies:")
	if err := resolver.Validate(); err != nil {
		cmd.Printf("  %s %v\n", style.FailMark, err)
		cmd.Println()
		cmd.Printf("%s Validation failed\n", style.FailMark)
		return err
	}
	cmd.Printf("  %s No circular dependencies\n", style.SuccessMark)
	cmd.Println()

	cmd.Printf("%s Validation successful (%d resources)\n", style.SuccessMark, len(resources))
	return nil
}
