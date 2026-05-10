// Package apt provides APT package manager integration for system package management.
package apt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/executor"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/system"
)

// CommandRunner abstracts shell command execution for testability.
// This is a subset of command.Executor's methods.
type CommandRunner interface {
	ExecuteCapture(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) (string, error)
	ExecuteWithOutput(ctx context.Context, cmds []string, vars command.Vars, env map[string]string, callback command.OutputCallback) error
}

// errEmptyPackagesInstall is returned when the package list is empty.
var errEmptyPackagesInstall = errors.New("apt: install requires at least one package")

// errEmptyPackagesRemove is returned when the package list is empty.
var errEmptyPackagesRemove = errors.New("apt: remove requires at least one package")

// disallowedInPackageName covers whitespace, shell metacharacters, and
// shell-expansion characters (globs, tilde, comment) that would either
// split a package name across argv slots, allow injection through the
// sh -c command form, or be expanded by the shell against the cwd. The
// schema layer also rejects whitespace; the apt layer guards independently
// as defense-in-depth for non-CUE callers. As a side effect, apt's regex
// package syntax (e.g. "linux-image-*") is also rejected — argv-form
// executor migration is tracked separately and would close this entire
// surface.
const disallowedInPackageName = " \t\n\r;|&`$<>(){}*?[]~#\\\"'"

// debianFrontendNoninteractive is prepended to the apt-get install command
// via `env`, not passed via the runner's env map. sudo strips most parent
// env vars by default (env_reset in /etc/sudoers), so a plain `env` map on
// the runner side would not reach apt-get; running `sudo env VAR=value
// apt-get …` is sudoers-independent and works on minimal CI images.
const debianFrontendNoninteractive = "env DEBIAN_FRONTEND=noninteractive"

// Client wraps a CommandRunner with apt-get / dpkg integration. It is the
// shared entry point: callers obtain adapters for specific system resources
// (VersionFunc for SystemInstaller, PackageSetInstaller for SystemPackageSet)
// from a single Client so the same runner is reused across all apt operations.
type Client struct {
	runner CommandRunner
}

// New returns a Client bound to the given runner.
func New(runner CommandRunner) *Client {
	return &Client{runner: runner}
}

// VersionFunc returns a system.VersionFunc that runs "apt-get --version"
// and extracts the version string. Used by the SystemInstaller validator.
func (c *Client) VersionFunc() system.VersionFunc {
	return func(ctx context.Context) (string, error) {
		output, err := c.runner.ExecuteCapture(ctx, []string{"apt-get --version"}, command.Vars{}, nil)
		if err != nil {
			return "", fmt.Errorf("failed to run apt-get --version: %w", err)
		}
		return parseAptVersion(output)
	}
}

// PackageSetInstaller returns the executor.Installer adapter for
// SystemPackageSet resources backed by apt-get.
func (c *Client) PackageSetInstaller() *PackageSetInstaller {
	return &PackageSetInstaller{client: c}
}

// Update runs "apt-get update" to refresh the local APT package index.
// It does NOT upgrade installed packages — that would be "apt-get
// upgrade", which tomei does not perform automatically. Callers (e.g.
// the future repository installer at #195, integration tests guarding
// against stale indexes) invoke Update before Install when freshness
// matters.
//
// The shell command executed is:
//
//	sudo -n env DEBIAN_FRONTEND=noninteractive apt-get update
//
// stdout/stderr are drained (discarded, not buffered in memory). On
// failure the returned error wraps the runner's error (typically
// `command failed: <expanded>: <cause>` from command.Executor, where
// <cause> may be an exit-status, start, expand, or context error);
// callers needing diagnostic output should re-run "apt-get update"
// manually.
//
// Note: apt-get update treats partial mirror failures (404 / DNS) as
// success (exit 0, only a stderr `W: Failed to fetch` warning).
// Update inherits that behavior — a subsequent Install failure with
// "Unable to locate package" may indicate a stale or unreachable
// source even when Update returned nil.
//
// The flags Install/Remove use are intentionally omitted here:
//   - `-y`: apt-get update issues no y/N prompts.
//   - `--`: there are no operands, so an option/operand separator is
//     unnecessary, and consequently no package-list validation is
//     needed (Install/Remove guard against shell-meaningful chars in
//     package names; Update has no such surface).
//   - lock-timeout flags: apt-get update primarily takes the apt
//     cache/list locks (covered by `Acquire::Lock::Timeout`) rather
//     than the dpkg-frontend lock that `DPkg::Lock::Timeout` covers;
//     the uniform timeout pass across all helpers is being tracked
//     in a separate follow-up.
func (c *Client) Update(ctx context.Context) error {
	cmd := "sudo -n " + debianFrontendNoninteractive + " apt-get update"
	// nil callback drains stdout/stderr to io.Discard rather than
	// buffering in memory; consistent with Install/Remove, we never
	// surface apt-get's human-oriented output to callers.
	if err := c.runner.ExecuteWithOutput(ctx, []string{cmd}, command.Vars{}, nil, nil); err != nil {
		return fmt.Errorf("apt: update: %w", err)
	}
	return nil
}

// PackageSetInstaller installs and removes SystemPackageSet resources via
// apt-get. It satisfies executor.Installer[*resource.SystemPackageSet,
// *resource.SystemPackageSetState].
type PackageSetInstaller struct {
	client *Client
}

// Compile-time assertion that *PackageSetInstaller satisfies the executor
// installer interface for SystemPackageSet.
var _ executor.Installer[*resource.SystemPackageSet, *resource.SystemPackageSetState] = (*PackageSetInstaller)(nil)

// Install runs the apt-get install for the resource's packages and returns
// the new state. The InstalledVersions field is left empty until the
// dpkg-query helper lands; the install action itself is complete.
//
// The shell command executed is:
//
//	sudo -n env DEBIAN_FRONTEND=noninteractive apt-get install -y -o DPkg::Lock::Timeout=60 -- <packages>
//
// stdout/stderr are drained (discarded, not buffered in memory).
//
// Callers are responsible for ensuring a recent "apt-get update" has run
// when stale package indexes would cause 404s.
func (p *PackageSetInstaller) Install(ctx context.Context, res *resource.SystemPackageSet, _ string) (*resource.SystemPackageSetState, error) {
	spec := res.SystemPackageSetSpec
	if err := p.runInstall(ctx, spec.Packages); err != nil {
		return nil, err
	}
	return &resource.SystemPackageSetState{
		InstallerRef:      spec.InstallerRef,
		RepositoryRef:     spec.RepositoryRef,
		Packages:          append([]string(nil), spec.Packages...),
		InstalledVersions: map[string]string{},
		UpdatedAt:         time.Now(),
	}, nil
}

// Remove runs the apt-get remove for the state's packages. state.Packages
// (populated by Install) is the source of truth; the resource spec is not
// consulted because Remove may run after the spec was deleted from the
// manifest.
//
// A nil state is treated as an error (defensive against corrupted state
// files), but an empty Packages list is treated as a no-op so the
// executor can still proceed to delete the state file. This diverges
// from Install (which rejects empty input as a manifest mistake) — for
// Remove, "nothing left to do" is the correct idempotent outcome.
//
// The shell command executed is:
//
//	sudo -n env DEBIAN_FRONTEND=noninteractive apt-get remove -y -o DPkg::Lock::Timeout=60 -- <packages>
//
// stdout/stderr are drained (discarded, not buffered in memory).
//
// `--purge` and `--auto-remove` are intentionally omitted: plain
// `apt-get remove` keeps the operation reversible (config files retained)
// and avoids cascading removal of dependencies that other
// SystemPackageSet resources may need.
func (p *PackageSetInstaller) Remove(ctx context.Context, state *resource.SystemPackageSetState, name string) error {
	if state == nil {
		return fmt.Errorf("apt: package set %q: nil state", name)
	}
	if len(state.Packages) == 0 {
		return nil
	}
	return p.runRemove(ctx, state.Packages)
}

// runInstall executes "sudo -n env DEBIAN_FRONTEND=noninteractive apt-get
// install -y -o DPkg::Lock::Timeout=60 -- <packages>". Returns
// errEmptyPackagesInstall if packages is empty. Each name is rejected if it is
// empty or contains whitespace / shell metacharacters / shell-expansion
// characters.
func (p *PackageSetInstaller) runInstall(ctx context.Context, packages []string) error {
	if len(packages) == 0 {
		return errEmptyPackagesInstall
	}
	for _, pkg := range packages {
		if pkg == "" {
			return errors.New("apt: empty package name in install list")
		}
		if strings.ContainsAny(pkg, disallowedInPackageName) {
			return fmt.Errorf("apt: package %q contains disallowed characters", pkg)
		}
	}
	cmd := "sudo -n " + debianFrontendNoninteractive +
		" apt-get install -y -o DPkg::Lock::Timeout=60 -- " +
		strings.Join(packages, " ")
	// nil callback drains stdout/stderr to io.Discard rather than buffering
	// in memory; apt-get install output can be large.
	if err := p.client.runner.ExecuteWithOutput(ctx, []string{cmd}, command.Vars{}, nil, nil); err != nil {
		return fmt.Errorf("apt: install %q: %w", packages, err)
	}
	return nil
}

// runRemove executes "sudo -n env DEBIAN_FRONTEND=noninteractive apt-get
// remove -y -o DPkg::Lock::Timeout=60 -- <packages>". Returns
// errEmptyPackagesRemove if packages is empty. Each name is rejected if
// it is empty or contains whitespace / shell metacharacters / shell-
// expansion characters.
func (p *PackageSetInstaller) runRemove(ctx context.Context, packages []string) error {
	if len(packages) == 0 {
		return errEmptyPackagesRemove
	}
	for _, pkg := range packages {
		if pkg == "" {
			return errors.New("apt: empty package name in remove list")
		}
		if strings.ContainsAny(pkg, disallowedInPackageName) {
			return fmt.Errorf("apt: package %q contains disallowed characters", pkg)
		}
	}
	cmd := "sudo -n " + debianFrontendNoninteractive +
		" apt-get remove -y -o DPkg::Lock::Timeout=60 -- " +
		strings.Join(packages, " ")
	// nil callback drains stdout/stderr to io.Discard rather than buffering
	// in memory; apt-get remove output (especially dependency hints) can be large.
	if err := p.client.runner.ExecuteWithOutput(ctx, []string{cmd}, command.Vars{}, nil, nil); err != nil {
		return fmt.Errorf("apt: remove %q: %w", packages, err)
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
