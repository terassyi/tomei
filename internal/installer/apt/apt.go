// Package apt provides APT package manager integration for system package management.
package apt

import (
	"context"
	"fmt"
	"strings"

	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/system"
)

// CommandRunner abstracts shell command execution for testability.
// This is a subset of command.Executor's methods.
type CommandRunner interface {
	ExecuteCapture(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) (string, error)
}

// VersionFunc returns a system.VersionFunc that runs "apt-get --version"
// and extracts the version string.
func VersionFunc(runner CommandRunner) system.VersionFunc {
	return func(ctx context.Context) (string, error) {
		output, err := runner.ExecuteCapture(ctx, []string{"apt-get --version"}, command.Vars{}, nil)
		if err != nil {
			return "", fmt.Errorf("failed to run apt-get --version: %w", err)
		}
		return parseAptVersion(output)
	}
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

// GetInstall installs the given packages via "sudo -n apt-get install -y".
// Pure helper — wiring into the SystemPackageSet installer happens in #198.
// Returns an error if packages is empty (defense-in-depth; schema validation
// at the resource layer should reject this earlier).
func GetInstall(ctx context.Context, runner CommandRunner, packages []string) error {
	if len(packages) == 0 {
		return fmt.Errorf("GetInstall: at least one package is required")
	}
	cmd := "sudo -n apt-get install -y " + strings.Join(packages, " ")
	if _, err := runner.ExecuteCapture(ctx, []string{cmd}, command.Vars{}, nil); err != nil {
		return fmt.Errorf("apt-get install %v: %w", packages, err)
	}
	return nil
}
