package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"
	"github.com/spf13/cobra"

	"github.com/terassyi/tomei/internal/github"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/upgrade"
)

const upgradeTimeout = 5 * time.Minute

var (
	upgradeCfg upgrade.Config
	upgradeDry bool
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade tomei to the latest version",
	Args:  cobra.NoArgs,
	RunE:  runUpgrade,
}

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeDry, "dry-run", false, "Check for updates without installing")
	upgradeCmd.Flags().BoolVar(&upgradeCfg.Force, "force", false, "Allow upgrade from development builds")
	upgradeCmd.Flags().StringVar(&upgradeCfg.TargetVersion, "version", "", "Install a specific version (e.g., 0.1.3)")
}

// formatVersion returns "v1.2.3" for semver strings and the raw string for non-semver (e.g., "dev").
func formatVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	if _, err := semver.NewVersion(v); err == nil {
		return "v" + v
	}
	return v
}

func runUpgrade(cmd *cobra.Command, _ []string) error {
	ctx, cancel := context.WithTimeout(cmd.Context(), upgradeTimeout)
	defer cancel()

	token := github.TokenFromEnv()
	apiClient := github.NewHTTPClient(token)
	dlClient := &http.Client{
		Transport: github.WrapTransport(token, download.DefaultTransport()),
	}

	u := upgrade.NewUpdater(apiClient, dlClient, version)

	// Check for updates
	cmd.Println("Checking for updates...")
	check, err := u.Check(ctx, upgradeCfg)
	if err != nil {
		return err
	}

	// Print version info
	cur := formatVersion(check.CurrentVersion)
	lat := formatVersion(check.LatestVersion)
	cmd.Printf("  Current: %s\n", cur)
	cmd.Printf("  Latest:  %s\n", lat)

	// Already up to date
	if check.UpToDate {
		cmd.Println()
		cmd.Printf("tomei %s is already up to date.\n", cur)
		return nil
	}

	// Dry run
	if upgradeDry {
		cmd.Println()
		cmd.Printf("Update available: %s → %s\n", cur, lat)
		hint := "tomei upgrade"
		if upgradeCfg.TargetVersion != "" {
			hint += " --version " + upgradeCfg.TargetVersion
		}
		if upgradeCfg.Force {
			hint += " --force"
		}
		cmd.Printf("To upgrade, run: %s\n", hint)
		return nil
	}

	cmd.Println()

	// Perform upgrade
	// pendingOK tracks whether the previous inline stage needs an "ok" before the next stage.
	pendingOK := false
	err = u.Upgrade(ctx, check, func(stage, detail string) {
		if pendingOK {
			cmd.Println("ok")
			pendingOK = false
		}
		switch stage {
		case upgrade.StageDownloading:
			cmd.Printf("Downloading %s\n", detail)
		case upgrade.StageChecksum:
			fmt.Fprint(cmd.OutOrStdout(), "Verifying checksum... ")
			pendingOK = true
		case upgrade.StageReplacing:
			fmt.Fprint(cmd.OutOrStdout(), "Replacing binary... ")
			pendingOK = true
		case upgrade.StageVerifying:
			fmt.Fprint(cmd.OutOrStdout(), "Verifying installation... ")
			pendingOK = true
		default:
			if detail != "" {
				cmd.Printf("%s %s\n", stage, detail)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "%s... ", stage)
				pendingOK = true
			}
		}
	})
	if err != nil {
		cmd.Println()
		return err
	}
	if pendingOK {
		cmd.Println("ok")
	}

	cmd.Println()
	cmd.Printf("Successfully upgraded tomei: %s → %s\n", cur, lat)
	return nil
}
