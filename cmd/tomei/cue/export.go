package cue

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
)

var exportCmd = &cobra.Command{
	Use:   "export <files or directories...>",
	Short: "Export CUE manifests as JSON with tomei configuration",
	Long: `Export CUE manifests as JSON with tomei's registry and @tag() configuration applied.

Unlike plain "cue export", this command automatically:
  - Configures the OCI registry for tomei module resolution
  - Injects @tag() values (os, arch, headless) from the current platform
  - Excludes config.cue from evaluation

Output is indented JSON.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runExport,
}

func runExport(cmd *cobra.Command, args []string) error {
	loader := config.NewLoader(nil)
	values, err := loader.EvalPaths(args)
	if err != nil {
		return fmt.Errorf("failed to evaluate: %w", err)
	}

	if len(values) == 0 {
		return fmt.Errorf("no CUE files found in the specified paths")
	}

	out := cmd.OutOrStdout()
	for i, value := range values {
		if i > 0 {
			fmt.Fprintln(out, "---")
		}

		jsonBytes, err := value.MarshalJSON()
		if err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}

		var indented bytes.Buffer
		if err := json.Indent(&indented, jsonBytes, "", "    "); err != nil {
			return fmt.Errorf("failed to indent JSON: %w", err)
		}

		fmt.Fprintln(out, indented.String())
	}

	return nil
}
