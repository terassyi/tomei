package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/terassyi/toto/internal/config"
	"github.com/terassyi/toto/internal/doctor"
	"github.com/terassyi/toto/internal/path"
	"github.com/terassyi/toto/internal/state"
	"github.com/terassyi/toto/internal/ui"
)

var doctorNoColor bool

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

func init() {
	doctorCmd.Flags().BoolVar(&doctorNoColor, "no-color", false, "Disable color output")
}

func runDoctor(cmd *cobra.Command, _ []string) error {
	if doctorNoColor {
		color.NoColor = true
	}

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
	style := ui.NewStyle()

	style.Header.Fprintln(cmd.OutOrStdout(), "Environment Health Check")
	cmd.Println()

	if !result.HasIssues() {
		cmd.Printf("%s No issues found. Environment is healthy.\n", style.SuccessMark)
		return
	}

	// Count warnings and conflicts
	warningCount := 0
	conflictCount := len(result.Conflicts)
	stateIssueCount := len(result.StateIssues)

	// Print unmanaged tools by category
	for category, tools := range result.UnmanagedTools {
		if len(tools) == 0 {
			continue
		}

		warningCount += len(tools)

		// Get category path (if available)
		categoryPath := ""
		if len(tools) > 0 && tools[0].Path != "" {
			categoryPath = tools[0].Path
		}

		if categoryPath != "" {
			cmd.Printf("[%s] %s\n", color.New(color.FgYellow).Sprint(category), style.Path.Sprint(categoryPath))
		} else {
			cmd.Printf("[%s]\n", color.New(color.FgYellow).Sprint(category))
		}

		for _, tool := range tools {
			cmd.Printf("  %s %-16s unmanaged\n", style.WarnMark, tool.Name)
		}
		cmd.Println()
	}

	// Print conflicts
	if len(result.Conflicts) > 0 {
		cmd.Printf("[%s]\n", color.New(color.FgRed).Sprint("Conflicts"))
		for _, conflict := range result.Conflicts {
			cmd.Printf("  %s %s: found in %s\n", style.FailMark, conflict.Name, strings.Join(conflict.Locations, ", "))
			if conflict.ResolvedTo != "" {
				cmd.Printf("       PATH resolves to: %s\n", style.Path.Sprint(conflict.ResolvedTo))
			}
		}
		cmd.Println()
	}

	// Print state issues
	if len(result.StateIssues) > 0 {
		cmd.Printf("[%s]\n", color.New(color.FgRed).Sprint("State Issues"))
		for _, issue := range result.StateIssues {
			cmd.Printf("  %s %s: %s\n", style.FailMark, issue.Name, issue.Message())
		}
		cmd.Println()
	}

	// Print summary
	summaryParts := []string{}
	if warningCount > 0 {
		summaryParts = append(summaryParts, color.New(color.FgYellow).Sprintf("%d warnings", warningCount))
	}
	if conflictCount > 0 {
		summaryParts = append(summaryParts, color.New(color.FgRed).Sprintf("%d conflicts", conflictCount))
	}
	if stateIssueCount > 0 {
		summaryParts = append(summaryParts, color.New(color.FgRed).Sprintf("%d state issues", stateIssueCount))
	}
	if len(summaryParts) > 0 {
		cmd.Printf("Summary: %s\n", strings.Join(summaryParts, ", "))
		cmd.Println()
	}

	// Print suggestions
	unmanagedNames := result.UnmanagedToolNames()
	if len(unmanagedNames) > 0 {
		style.Header.Fprintln(cmd.OutOrStdout(), "Suggestions:")
		cmd.Printf("  %s\n", style.Success.Sprintf("toto adopt %s", strings.Join(unmanagedNames, " ")))
	}
}
