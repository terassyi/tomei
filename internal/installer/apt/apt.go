// Package apt provides APT package manager integration for system package management.
package apt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/system"
)

// CommandRunner abstracts shell command execution for testability.
// This is a subset of command.Executor's methods.
type CommandRunner interface {
	ExecuteCapture(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) (string, error)
	ExecuteWithOutput(ctx context.Context, cmds []string, vars command.Vars, env map[string]string, callback command.OutputCallback) error
}

// errEmptyPackages is returned by GetInstall when called with no packages.
var errEmptyPackages = errors.New("apt: install requires at least one package")

// shellMetachars are characters that, if present in a package name, would
// allow shell injection via the sh -c command form. The schema layer rejects
// whitespace; this layer rejects the remaining shell metacharacters as
// defense-in-depth until the executor moves to argv form.
const shellMetachars = ";|&`$<>(){}\\\"'"

// aptEnv is the environment for every apt-get invocation. DEBIAN_FRONTEND is
// load-bearing: without it, packages with debconf prompts (tzdata,
// keyboard-configuration, etc.) hang indefinitely under "apt-get -y".
var aptEnv = map[string]string{"DEBIAN_FRONTEND": "noninteractive"}

// Client groups apt-get / dpkg helpers behind a single CommandRunner.
// Used by the SystemInstaller validator and the SystemPackageRepository /
// SystemPackageSet installers.
type Client struct {
	runner CommandRunner
}

// New returns a Client bound to the given runner.
func New(runner CommandRunner) *Client {
	return &Client{runner: runner}
}

// VersionFunc returns a system.VersionFunc that runs "apt-get --version"
// and extracts the version string.
func (c *Client) VersionFunc() system.VersionFunc {
	return func(ctx context.Context) (string, error) {
		output, err := c.runner.ExecuteCapture(ctx, []string{"apt-get --version"}, command.Vars{}, aptEnv)
		if err != nil {
			return "", fmt.Errorf("failed to run apt-get --version: %w", err)
		}
		return parseAptVersion(output)
	}
}

// GetInstall installs the given packages via "sudo -n apt-get install -y".
// Returns errEmptyPackages if packages is empty. Each package name is
// rejected if it contains shell metacharacters, since the executor uses
// sh -c (full argv-form is tracked separately).
//
// Callers are responsible for ensuring a recent "apt-get update" has run
// when stale package indexes would cause 404s.
func (c *Client) GetInstall(ctx context.Context, packages []string) error {
	if len(packages) == 0 {
		return errEmptyPackages
	}
	for _, p := range packages {
		if strings.ContainsAny(p, shellMetachars) {
			return fmt.Errorf("apt: package %q contains disallowed characters", p)
		}
	}
	cmd := "sudo -n apt-get install -y -o DPkg::Lock::Timeout=60 -- " + strings.Join(packages, " ")
	// nil callback drains stdout/stderr to io.Discard rather than buffering
	// in memory; apt-get install output can be large.
	if err := c.runner.ExecuteWithOutput(ctx, []string{cmd}, command.Vars{}, aptEnv, nil); err != nil {
		return fmt.Errorf("apt: install %q: %w", packages, err)
	}
	return nil
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
