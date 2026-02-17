package cue

import (
	"github.com/spf13/cobra"
	"github.com/terassyi/tomei/internal/cuemod"
)

var scaffoldBare bool

var scaffoldCmd = &cobra.Command{
	Use:   "scaffold <kind>",
	Short: "Generate a CUE manifest scaffold for a resource kind",
	Long: `Generate a CUE manifest scaffold with schema import and placeholder values.

Supported kinds: tool, runtime, installer, installer-repository, toolset

The output includes an import of "tomei.terassyi.net/schema" and type
constraints (e.g., schema.#Tool &) by default. Use --bare to output
plain CUE without schema imports (for use without cue.mod/).

Examples:
  tomei cue scaffold tool                  # With schema import
  tomei cue scaffold runtime --bare        # Without schema import
  tomei cue scaffold tool > tools.cue      # Redirect to file`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeScaffoldKind,
	RunE:              runScaffold,
}

func init() {
	scaffoldCmd.Flags().BoolVar(&scaffoldBare, "bare", false, "Output without schema import (for use without cue.mod/)")
}

func completeScaffoldKind(_ *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return cuemod.SupportedScaffoldKinds(), cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

func runScaffold(cmd *cobra.Command, args []string) error {
	out, err := cuemod.Scaffold(args[0], cuemod.ScaffoldParams{Bare: scaffoldBare})
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(out)
	return err
}
