package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/terassyi/toto/internal/config"
)

var planCmd = &cobra.Command{
	Use:   "plan <files or directories...>",
	Short: "Show the execution plan",
	Long: `Show the execution plan without applying changes.

Displays what actions would be taken:
  - install: New resources to install
  - upgrade: Resources to upgrade
  - reinstall: Resources to reinstall (due to taint)
  - remove: Resources to remove`,
	Args: cobra.MinimumNArgs(1),
	RunE: runPlan,
}

func runPlan(cmd *cobra.Command, args []string) error {
	cmd.Printf("Planning changes for %v\n", args)

	loader := config.NewLoader(nil)
	resources, err := loader.LoadPaths(args)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	cmd.Printf("Found %d resource(s)\n", len(resources))

	// TODO: Load state and diff
	// TODO: Build dependency graph
	// TODO: Show execution plan

	cmd.Println("Plan not yet fully implemented")
	return nil
}
