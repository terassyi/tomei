package cue

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/cuemod"
)

var (
	presetsVersion    string
	presetsOutput     string
	presetsPreRelease bool
)

var presetsCmd = &cobra.Command{
	Use:   "presets",
	Short: "List available preset manifests from the OCI registry",
	Long: `Fetches the tomei CUE module from the OCI registry and lists available
preset packages with their exported definitions.

Output formats:
  table  Tabular summary with preset name, import path, and definitions (default)
  json   JSON array of preset objects
  cue    Raw CUE source of each preset, separated by "---"`,
	Args: cobra.NoArgs,
	RunE: runPresets,
}

func init() {
	presetsCmd.Flags().StringVar(&presetsVersion, "version", "", "Module version to inspect (default: latest)")
	presetsCmd.Flags().StringVarP(&presetsOutput, "output", "o", "table", "Output format: table, cue, json")
	presetsCmd.Flags().BoolVar(&presetsPreRelease, "pre", false, "Include pre-release versions")

	_ = presetsCmd.RegisterFlagCompletionFunc("output", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"table", "cue", "json"}, cobra.ShellCompDirectiveNoFileComp
	})
}

func runPresets(cmd *cobra.Command, _ []string) error {
	switch presetsOutput {
	case "table", "cue", "json":
	default:
		return fmt.Errorf("unsupported output format %q (must be table, cue, or json)", presetsOutput)
	}

	var opts []cuemod.ResolveOption
	if presetsPreRelease {
		opts = append(opts, cuemod.WithPreRelease())
	}

	presets, version, err := cuemod.FetchPresets(cmd.Context(), presetsVersion, opts...)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()

	switch presetsOutput {
	case "table":
		fmt.Fprintf(out, "Presets from tomei.terassyi.net@v0 (%s)\n\n", version)
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PRESET\tIMPORT_PATH\tDEFINITIONS")
		for _, p := range presets {
			fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.ImportPath, strings.Join(p.Definitions, ", "))
		}
		w.Flush()

	case "json":
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(presets); err != nil {
			return fmt.Errorf("failed to encode JSON: %w", err)
		}

	case "cue":
		for i, p := range presets {
			if i > 0 {
				fmt.Fprintln(out, "---")
			}
			fmt.Fprintf(out, "// preset: %s (%s)\n", p.Name, p.ImportPath)
			fmt.Fprintln(out, p.Source)
		}
	}

	return nil
}
