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

// errEmptyPackages is returned by Install when called with no packages.
var errEmptyPackages = errors.New("apt: install requires at least one package")

// disallowedInPackageName covers whitespace, shell metacharacters, and
// shell-expansion characters (globs, tilde, comment) that would either
// split a package name across argv slots, allow injection through the
// sh -c command form, or be expanded by the shell against the cwd. The
// schema layer also rejects whitespace; Install guards independently
// as defense-in-depth for non-CUE callers. As a side effect, apt's regex
// package syntax (e.g. "linux-image-*") is also rejected — argv-form
// executor migration is tracked separately and would close this entire
// surface.
const disallowedInPackageName = " \t\n\r;|&`$<>(){}*?[]~#\\\"'"

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

// Install installs the given packages by running, under the configured
// runner: "sudo -n apt-get install -y -o DPkg::Lock::Timeout=60 -- <packages>"
// with DEBIAN_FRONTEND=noninteractive in the environment. stdout/stderr are
// drained; the install action is silent on success.
//
// Returns errEmptyPackages if packages is empty. Each package name is
// rejected if it contains whitespace or shell metacharacters, since the
// executor uses sh -c (full argv-form is tracked separately).
//
// Callers are responsible for ensuring a recent "apt-get update" has run
// when stale package indexes would cause 404s.
func (c *Client) Install(ctx context.Context, packages []string) error {
	if len(packages) == 0 {
		return errEmptyPackages
	}
	for _, p := range packages {
		if p == "" {
			return errors.New("apt: empty package name in install list")
		}
		if strings.ContainsAny(p, disallowedInPackageName) {
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
