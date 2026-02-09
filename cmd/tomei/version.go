package main

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

// VersionInfo contains version information for the binary.
type VersionInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	Platform  string `json:"platform"`
}

var versionFormat string

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version",
	RunE: func(cmd *cobra.Command, _ []string) error {
		info := VersionInfo{
			Version:   version,
			Commit:    commit,
			BuildDate: buildDate,
			GoVersion: runtime.Version(),
			Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		}

		switch versionFormat {
		case outputJSON:
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(info)
		default:
			cmd.Printf("tomei version %s\n", info.Version)
			cmd.Printf("  commit:    %s\n", info.Commit)
			cmd.Printf("  built:     %s\n", info.BuildDate)
			cmd.Printf("  go:        %s\n", info.GoVersion)
			cmd.Printf("  platform:  %s\n", info.Platform)
			return nil
		}
	},
}

func init() {
	versionCmd.Flags().StringVarP(&versionFormat, "output", "o", "text", "Output format (text, json)")
}
