package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/graph"
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

	// Validate each resource's spec
	for _, res := range resources {
		if err := res.Spec().Validate(); err != nil {
			return fmt.Errorf("validation failed for %s/%s: %w", res.Kind(), res.Name(), err)
		}
	}

	// Check for circular dependencies
	resolver := graph.NewResolver()
	for _, res := range resources {
		resolver.AddResource(res)
	}

	if err := resolver.Validate(); err != nil {
		return fmt.Errorf("dependency validation failed: %w", err)
	}

	cmd.Println("Validation successful")
	return nil
}
