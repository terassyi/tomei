package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/printer"
	"github.com/terassyi/tomei/internal/state"
)

var getOutput string

var getCmd = &cobra.Command{
	Use:   "get <resource-type> [name]",
	Short: "Display installed resources",
	Long: `Display installed resources from state.

Resource types:
  tools, tool                                    Tools
  runtimes, runtime, rt                          Runtimes
  installers, installer, inst                    Installers
  installerrepositories, installerrepository, instrepo  InstallerRepositories

Examples:
  tomei get tools
  tomei get tools ripgrep
  tomei get runtimes -o wide
  tomei get tools -o json`,
	Args:              cobra.RangeArgs(1, 2),
	ValidArgsFunction: completeResourceType,
	RunE:              runGet,
}

func init() {
	getCmd.Flags().StringVarP(&getOutput, "output", "o", "table", "Output format: table, wide, json")
	_ = getCmd.RegisterFlagCompletionFunc("output", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"table", "wide", "json"}, cobra.ShellCompDirectiveNoFileComp
	})
}

func completeResourceType(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return []string{"tools", "runtimes", "installers", "installerrepositories"}, cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func runGet(cmd *cobra.Command, args []string) error {
	resType, err := printer.ResolveResourceType(args[0])
	if err != nil {
		return err
	}

	var name string
	if len(args) > 1 {
		name = args[1]
	}

	cfg, err := config.LoadConfig(config.DefaultConfigDir)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	paths, err := path.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create paths: %w", err)
	}

	store, err := state.NewStore[state.UserState](paths.UserDataDir())
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}

	userState, err := store.LoadReadOnly()
	if err != nil {
		return fmt.Errorf("failed to load state: %w", err)
	}

	wide := getOutput == "wide"
	jsonOut := getOutput == outputJSON

	return printer.Run(cmd.OutOrStdout(), userState, resType, name, wide, jsonOut)
}
