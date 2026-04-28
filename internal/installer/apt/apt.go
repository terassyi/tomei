// Package apt provides APT package manager integration for system package management.
//
// Currently implements InstallerInstaller for the SystemInstaller resource,
// which detects APT availability and records the version.
package apt

import (
	"context"
	"fmt"
	"strings"

	"github.com/terassyi/tomei/internal/installer/command"
)

// commandRunner abstracts shell command execution for testability.
// This is a subset of command.Executor's methods.
type commandRunner interface {
	ExecuteCapture(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) (string, error)
}

// parseAptVersion extracts the version string from apt-get --version output.
// Example input: "apt 2.4.12 (amd64)\nUsage: apt-get [options] command\n..."
// Returns: "2.4.12"
func parseAptVersion(output string) (string, error) {
	firstLine := strings.SplitN(strings.TrimSpace(output), "\n", 2)[0]
	fields := strings.Fields(firstLine)
	if len(fields) < 2 {
		return "", fmt.Errorf("unexpected apt-get --version output: %q", output)
	}
	return fields[1], nil
}
