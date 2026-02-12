package main

import (
	"fmt"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/terassyi/tomei/internal/config"
	tomeilog "github.com/terassyi/tomei/internal/log"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/ui"
)

var logsListSessions bool
var logsNoColor bool

var logsCmd = &cobra.Command{
	Use:   "logs [kind/name | kind name]",
	Short: "Show installation logs from the last apply",
	Long: `Show installation logs from the last tomei apply session.

Without arguments, lists all failed resources from the most recent session.
With a resource argument, shows the full log for that resource.
Resource can be specified as "kind/name" or "kind name" (case-insensitive).

Examples:
  tomei logs                  # list failed resources from last session
  tomei logs Tool/ripgrep     # show full log for tool/ripgrep
  tomei logs tool/ripgrep     # same (case-insensitive)
  tomei logs tool ripgrep     # same (space-separated)
  tomei logs --list           # list all sessions`,
	RunE: runLogs,
}

func init() {
	logsCmd.Flags().BoolVar(&logsListSessions, "list", false, "List all log sessions")
	logsCmd.Flags().BoolVar(&logsNoColor, "no-color", false, "Disable colored output")
}

func runLogs(cmd *cobra.Command, args []string) error {
	if logsNoColor {
		color.NoColor = true
	}

	logsDir, err := resolveLogsDir()
	if err != nil {
		return err
	}

	if logsListSessions {
		return listSessions(cmd, logsDir)
	}

	if len(args) > 0 {
		return showResourceLogFromArgs(cmd, logsDir, args)
	}

	return showLatestSession(cmd, logsDir)
}

func resolveLogsDir() (string, error) {
	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	paths, err := path.NewFromConfig(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to create paths: %w", err)
	}

	return paths.UserCacheDir() + "/logs", nil
}

func listSessions(cmd *cobra.Command, logsDir string) error {
	style := ui.NewStyle()

	sessions, err := tomeilog.ListSessions(logsDir)
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		cmd.Println("No log sessions found.")
		return nil
	}

	style.Header.Fprintln(cmd.OutOrStdout(), "Log Sessions:")
	for _, s := range sessions {
		logs, err := tomeilog.ReadSessionLogs(s.Dir)
		if err != nil {
			continue
		}
		cmd.Printf("  %s  (%d logs)\n", s.ID, len(logs))
	}

	return nil
}

func showLatestSession(cmd *cobra.Command, logsDir string) error {
	style := ui.NewStyle()

	sessions, err := tomeilog.ListSessions(logsDir)
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		cmd.Println("No log sessions found.")
		return nil
	}

	latest := sessions[0]
	logs, err := tomeilog.ReadSessionLogs(latest.Dir)
	if err != nil {
		return err
	}

	if len(logs) == 0 {
		cmd.Printf("No failure logs in session %s.\n", latest.ID)
		return nil
	}

	style.Header.Fprintf(cmd.OutOrStdout(), "Session: %s\n", latest.ID)
	cmd.Println()

	for _, l := range logs {
		cmd.Printf("  %s %s/%s\n", style.FailMark, l.Kind, l.Name)
	}

	cmd.Println()
	cmd.Println("Use 'tomei logs <kind>/<name>' to see the full log.")

	return nil
}

func showResourceLogFromArgs(cmd *cobra.Command, logsDir string, args []string) error {
	ref, err := resource.ParseRefArgs(args)
	if err != nil {
		return err
	}

	sessions, err := tomeilog.ListSessions(logsDir)
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		cmd.Println("No log sessions found.")
		return nil
	}

	latest := sessions[0]
	content, err := tomeilog.ReadResourceLog(latest.Dir, ref.Kind, ref.Name)
	if err != nil {
		return err
	}

	cmd.Print(content)
	return nil
}
