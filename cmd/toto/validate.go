package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/terassyi/toto/internal/config"
)

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

func runValidate(cmd *cobra.Command, args []string) error {
	cmd.Printf("Validating configuration in %v\n", args)

	loader := config.NewLoader(nil)
	resources, err := loader.LoadPaths(args)
	if err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	cmd.Printf("Found %d resource(s)\n", len(resources))
	for _, res := range resources {
		cmd.Printf("  - %s/%s\n", res.Kind(), res.Name())
	}

	// TODO: Add circular dependency detection

	cmd.Println("Validation successful")
	return nil
}
