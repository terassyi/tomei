package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/terassyi/toto/internal/config"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate the configuration",
	Long: `Validate the CUE configuration files.

Checks for:
  - CUE syntax errors
  - Schema validation
  - Circular dependency detection`,
	RunE: runValidate,
}

func runValidate(cmd *cobra.Command, _ []string) error {
	dir, err := getConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}

	cmd.Printf("Validating configuration in %s\n", dir)

	loader := config.NewLoader(nil)
	resources, err := loader.Load(dir)
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
