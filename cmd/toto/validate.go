package main

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/graph"
)

var validateNoColor bool

var validateCmd = &cobra.Command{
	Use:   "validate <files or directories...>",
	Short: "Validate the configuration",
	Long: `Validate the CUE configuration files.

Checks for:
  - CUE syntax errors
  - Schema validation
  - Circular dependency detection`,
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

	style := newOutputStyle()

	cmd.Println("Validating configuration...")
	cmd.Println()

	loader := config.NewLoader(nil)
	resources, err := loader.LoadPaths(args)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	// Validate each resource's spec
	cmd.Println("Resources:")
	validationFailed := false
	for _, res := range resources {
		if err := res.Spec().Validate(); err != nil {
			cmd.Printf("  %s %s - %v\n", style.failMark, style.path.Sprintf("%s/%s", res.Kind(), res.Name()), err)
			validationFailed = true
		} else {
			cmd.Printf("  %s %s\n", style.successMark, style.path.Sprintf("%s/%s", res.Kind(), res.Name()))
		}
	}
	cmd.Println()

	if validationFailed {
		cmd.Printf("%s Validation failed\n", style.failMark)
		return fmt.Errorf("validation failed")
	}

	// Check for circular dependencies
	resolver := graph.NewResolver()
	for _, res := range resources {
		resolver.AddResource(res)
	}

	cmd.Println("Dependencies:")
	if err := resolver.Validate(); err != nil {
		cmd.Printf("  %s %v\n", style.failMark, err)
		cmd.Println()
		cmd.Printf("%s Validation failed\n", style.failMark)
		return err
	}
	cmd.Printf("  %s No circular dependencies\n", style.successMark)
	cmd.Println()

	cmd.Printf("%s Validation successful (%d resources)\n", style.successMark, len(resources))
	return nil
}
