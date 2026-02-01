package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/doctor"
	"github.com/terassyi/toto/internal/path"
	"github.com/terassyi/toto/internal/state"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose the environment",
	Long: `Diagnose the environment for potential issues.

Checks for:
  - Unmanaged tools in runtime bin paths (~/go/bin, ~/.cargo/bin)
  - Conflicts between toto-managed and unmanaged tools
  - State file integrity`,
	RunE: runDoctor,
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	ctx := context.Background()

	// Load config
	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Create paths
	paths, err := path.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create paths: %w", err)
	}

	// Load state
	store, err := state.NewStore[state.UserState](paths.UserDataDir())
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}

	if err := store.Lock(); err != nil {
		return fmt.Errorf("failed to lock state: %w", err)
	}
	defer func() { _ = store.Unlock() }()

	userState, err := store.Load()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	// Run doctor
	doc, err := doctor.New(paths, userState)
	if err != nil {
		return fmt.Errorf("failed to create doctor: %w", err)
	}
	result, err := doc.Check(ctx)
	if err != nil {
		return fmt.Errorf("doctor check failed: %w", err)
	}

	// Print results
	printDoctorResult(cmd, result)

	return nil
}

func printDoctorResult(cmd *cobra.Command, result *doctor.Result) {
	if !result.HasIssues() {
		cmd.Println("No issues found. Environment is healthy.")
		return
	}

	// Print unmanaged tools by category
	for category, tools := range result.UnmanagedTools {
		if len(tools) == 0 {
			continue
		}

		cmd.Printf("[%s]\n", category)
		for _, tool := range tools {
			cmd.Printf("  %-20s unmanaged\n", tool.Name)
		}
		cmd.Println()
	}

	// Print conflicts
	if len(result.Conflicts) > 0 {
		cmd.Println("[Conflicts]")
		for _, conflict := range result.Conflicts {
			cmd.Printf("  %s: found in %s\n", conflict.Name, strings.Join(conflict.Locations, ", "))
			if conflict.ResolvedTo != "" {
				cmd.Printf("         PATH resolves to: %s\n", conflict.ResolvedTo)
			}
		}
		cmd.Println()
	}

	// Print state issues
	if len(result.StateIssues) > 0 {
		cmd.Println("[State Issues]")
		for _, issue := range result.StateIssues {
			cmd.Printf("  %s: %s\n", issue.Name, issue.Message())
		}
		cmd.Println()
	}

	// Print suggestions
	unmanagedNames := result.UnmanagedToolNames()
	if len(unmanagedNames) > 0 {
		cmd.Println("[Suggestions]")
		cmd.Printf("  toto adopt %s\n", strings.Join(unmanagedNames, " "))
	}
}
