package cue

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/cuemod"
)

var (
	presetsVersion    string
	presetsOutput     string
	presetsPreRelease bool
	presetsDefinition string
)

var presetsCmd = &cobra.Command{
	Use:   "presets [name]",
	Short: "List available preset manifests from the OCI registry",
	Long: `Fetches the tomei CUE module from the OCI registry and lists available
preset packages with their exported definitions.

When a preset name is given, only that preset is shown.
Use --definition (-d) to further narrow to a single definition.

Output formats:
  table  Tabular summary with preset name, import path, and definitions (default)
  json   JSON array of preset objects
  cue    Raw CUE source of each preset, separated by "---"
         When --definition is used, only the definition snippet is shown
         (without the package clause or imports).`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: completePresetNames,
	RunE:              runPresets,
}

func init() {
	presetsCmd.Flags().StringVar(&presetsVersion, "version", "", "Module version to inspect (default: latest)")
	presetsCmd.Flags().StringVarP(&presetsOutput, "output", "o", "table", "Output format: table, cue, json")
	presetsCmd.Flags().BoolVar(&presetsPreRelease, "pre", false, "Include pre-release versions")
	presetsCmd.Flags().StringVarP(&presetsDefinition, "definition", "d", "", "Filter by definition name (e.g. GoRuntime or #GoRuntime)")

	_ = presetsCmd.RegisterFlagCompletionFunc("output", func(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
		return []string{"table", "cue", "json"}, cobra.ShellCompDirectiveNoFileComp
	})
}

func runPresets(cmd *cobra.Command, args []string) error {
	switch presetsOutput {
	case "table", "cue", "json":
	default:
		return fmt.Errorf("unsupported output format %q (must be table, cue, or json)", presetsOutput)
	}

	if presetsDefinition != "" && len(args) == 0 {
		return fmt.Errorf("--definition requires a preset name argument (e.g., tomei cue presets go -d GoRuntime)")
	}

	var opts []cuemod.ResolveOption
	if presetsPreRelease {
		opts = append(opts, cuemod.WithPreRelease())
	}

	presets, version, err := cuemod.FetchPresets(cmd.Context(), presetsVersion, opts...)
	if err != nil {
		return err
	}

	// Filter by preset name if specified.
	if len(args) > 0 {
		name := args[0]
		var filtered []cuemod.PresetInfo
		for _, p := range presets {
			if p.Name == name {
				filtered = append(filtered, p)
			}
		}
		if len(filtered) == 0 {
			available := make([]string, len(presets))
			for i, p := range presets {
				available[i] = p.Name
			}
			return fmt.Errorf("preset %q not found (available: %s)", name, strings.Join(available, ", "))
		}
		presets = filtered
	}

	// Normalize definition name to include "#" prefix.
	defName := presetsDefinition
	if defName != "" && !strings.HasPrefix(defName, "#") {
		defName = "#" + defName
	}

	// Filter by definition name if specified.
	if defName != "" {
		p := presets[0] // guaranteed single preset by name filter above
		if !slices.Contains(p.Definitions, defName) {
			return fmt.Errorf("definition %s not found in preset %q (available: %s)",
				defName, p.Name, strings.Join(p.Definitions, ", "))
		}
		p.Definitions = []string{defName}
		// Narrow source to the requested definition block.
		defSource, err := cuemod.ExtractDefinition(p.Source, defName)
		if err != nil {
			return fmt.Errorf("failed to extract definition %s from preset %q: %w", defName, p.Name, err)
		}
		p.Source = defSource
		presets = []cuemod.PresetInfo{p}
	}

	out := cmd.OutOrStdout()

	switch presetsOutput {
	case "table":
		fmt.Fprintf(out, "Presets from %s (%s)\n\n", config.TomeiModulePath, version)
		w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(w, "PRESET\tIMPORT_PATH\tDEFINITIONS")
		for _, p := range presets {
			fmt.Fprintf(w, "%s\t%s\t%s\n", p.Name, p.ImportPath, strings.Join(p.Definitions, ", "))
		}
		if err := w.Flush(); err != nil {
			return fmt.Errorf("failed to flush output: %w", err)
		}

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
			if defName != "" {
				fmt.Fprintf(out, "// definition: %s from %s (%s)\n", defName, p.Name, p.ImportPath)
			} else {
				fmt.Fprintf(out, "// preset: %s (%s)\n", p.Name, p.ImportPath)
			}
			fmt.Fprintln(out, p.Source)
		}
	}

	return nil
}

func completePresetNames(_ *cobra.Command, _ []string, _ string) ([]string, cobra.ShellCompDirective) {
	return cuemod.KnownPresetNames(), cobra.ShellCompDirectiveNoFileComp
}
