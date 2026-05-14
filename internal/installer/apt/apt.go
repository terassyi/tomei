// Package apt provides APT package manager integration for system
// package and third-party repository management on Debian-family
// distributions. It exposes a shared Client over a CommandRunner and
// per-resource installers (PackageSetInstaller for SystemPackageSet,
// PackageRepositoryInstaller for SystemPackageRepository) plus
// read-only probes (IsInstalled, PackageVersion, VersionFunc).
package apt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/download"
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

// aptGetEnvPrefix is the `env VAR=value ...` prefix prepended to every
// apt-get invocation in this package. It is passed via `env` (not the
// runner's env map) because sudo strips most parent env vars by default
// (env_reset in /etc/sudoers), so a plain env map on the runner side
// would not reach apt-get; running `sudo env VAR=value apt-get …` is
// sudoers-independent and works on minimal CI images.
//
// The prefix bundles three pins:
//   - DEBIAN_FRONTEND=noninteractive — keeps apt-get from prompting on
//     conffile / debconf questions when invoked over sudo.
//   - LC_ALL=C LANGUAGE=C — load-bearing for the repository installer's
//     partial-fetch detector: apt translates warning prefixes per locale
//     (e.g. `W: ` becomes `警告: ` under ja_JP.UTF-8), and the
//     `^W: Failed to fetch` regex would silently miss a real fetch
//     failure on non-C locales — leaving a broken sources.list installed.
const aptGetEnvPrefix = "env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C"

// dpkgStatusInstalled is the literal third sub-field of dpkg's Status field
// for an installed package — what `dpkg-query -W -f='${db:Status-Status}'`
// emits per matching package when it is installed and configured.
// IsInstalled compares each output line against this exact value.
const dpkgStatusInstalled = "installed"

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

// PackageRepositoryInstaller returns the executor.Installer adapter for
// SystemPackageRepository resources. Unlike PackageSetInstaller it takes
// a download.Downloader because adding a repository involves fetching the
// armored GPG key over HTTPS — keeping the dependency on the per-resource
// factory rather than on Client itself means other future installers that
// do not need network access (e.g. local-only package sets) are not forced
// to thread an unused dependency through.
func (c *Client) PackageRepositoryInstaller(d download.Downloader) *PackageRepositoryInstaller {
	return &PackageRepositoryInstaller{client: c, downloader: d}
}

// IsInstalled reports whether dpkg currently considers pkg to be installed.
// It is a read-only probe — no apt or dpkg state is modified — used by
// the SystemPackageSet reconciler (#198) to compare desired vs actual
// state on each package.
//
// The shell command executed is:
//
//	dpkg-query -W -f='${db:Status-Status}\n' -- <pkg>
//
// dpkg-query's `${db:Status-Status}` format directive returns just the
// third sub-field of the dpkg Status triple ("installed", "not-installed",
// "config-files", "half-installed", etc.) — one literal word per line.
// This avoids parsing the human-formatted "<want> <eflag> <status>" triple
// and crucially handles two important edge cases correctly:
//
//   - hold: `apt-mark hold pkg` produces a Status of "hold ok installed".
//     The third field is still "installed", so a held package is correctly
//     reported as installed (rather than triggering an unnecessary
//     re-install by the reconciler).
//   - multi-arch ambiguity: if multiple architectures of the same package
//     are installed (e.g. libc6:amd64 + libc6:i386), dpkg-query emits one
//     line per match. This helper reports true if any match has status
//     "installed", which matches the reconciler's intent.
//
// dpkg-query (rather than `dpkg -l`) and the `${db:Status-Status}` directive
// (rather than the full `${Status}` triple) require dpkg 1.17.11+ (Debian 8
// jessie / Ubuntu 16.04 LTS and later) per `man dpkg-query`; tomei does not
// target older releases.
//
// Return values:
//   - exit 0, any line equal to "installed" → (true, nil)
//   - exit 0, no line equal to "installed" (e.g. only "config-files",
//     "not-installed", "half-installed") → (false, nil)
//   - exit 1 (dpkg-query: "no packages found matching <pkg>") → (false, nil)
//   - exit ≥ 2 or a non-ExitError (dpkg-query missing, permission denied,
//     signal, ctx cancellation, runner-side template error) → wrapped error
//
// Caller contract: when err is non-nil the bool return is meaningless
// (always false) and MUST NOT be interpreted as "package is not
// installed" — a runner-side failure does not imply absence. Callers
// MUST check err before consuming the bool.
//
// Strict semantic: only the literal status "installed" is reported as
// installed. "config-files" (a.k.a. `rc` in `dpkg -l` output) is NOT
// installed — this matches the integration-test convention of asserting
// on "ii  <pkg>" in `dpkg -l` output. Intermediate states ("half-installed",
// "unpacked", "half-configured") are also treated as not-installed; the
// reconciler will trigger a re-install, which is the correct recovery
// action for a broken dpkg state.
//
// pkg is rejected if it is empty or contains shell-meaningful characters
// (whitespace, metacharacters, expansion characters). The same
// disallowedInPackageName guard used by Install/Remove applies; ":" is
// intentionally permitted to support Debian multi-arch package syntax
// (e.g. "libc6:amd64"). ASCII control (other than NUL) and high-bit
// chars not on the disallowed list are caught downstream by dpkg-query
// (exit 1 → false). A NUL byte in pkg is rejected by os/exec when
// constructing the subprocess argv ("exec: argument contains NUL") and
// surfaces as a wrapped error rather than reaching dpkg-query.
//
// Trust model: pkg is assumed to come from a trusted source (a CUE
// manifest under the user's control, or another in-process caller) per
// command/executor.go's package-level Security Model. Callers exposing
// IsInstalled to untrusted input (e.g. an HTTP API) MUST add their own
// sanitization layer.
//
// Concurrency: dpkg-query reads /var/lib/dpkg/status (world-readable) and
// does not take the dpkg-frontend lock. dpkg uses atomic rename to update
// the status file, so dpkg-query can never observe a torn write — but a
// probe performed concurrently with apt-get install/remove may observe
// either the pre- or post-transaction state. Callers should consume the
// result idempotently.
//
// No sudo, no DEBIAN_FRONTEND: dpkg-query is unprivileged and never prompts.
func (c *Client) IsInstalled(ctx context.Context, pkg string) (bool, error) {
	if pkg == "" {
		return false, errors.New("apt: empty package name")
	}
	if strings.ContainsAny(pkg, disallowedInPackageName) {
		return false, fmt.Errorf("apt: package %q contains disallowed characters", pkg)
	}

	// IMPORTANT: the format string is single-quoted in the shell argument
	// so sh leaves "${db:Status-Status}" as a literal for dpkg-query to
	// interpret. Changing this to double quotes would let sh expand
	// "${db:Status-Status}" against the host environment (almost certainly
	// to ""), silently breaking the helper. Keep it single-quoted.
	// `--` operand separator guards against any future widening of allowed
	// characters that could permit a leading `-`.
	cmd := `dpkg-query -W -f='${db:Status-Status}\n' -- ` + pkg
	output, err := c.runner.ExecuteCapture(ctx, []string{cmd}, command.Vars{}, nil)
	if err != nil {
		// ctx cancellation/timeout surfaces as a signal-kill on dpkg-query
		// whose *exec.ExitError reports ExitCode() == -1. Without an
		// explicit ctx check the cancellation would silently fall through
		// to the generic wrap, leaving callers unable to detect it via
		// errors.Is(err, context.Canceled / context.DeadlineExceeded).
		// Check ctx.Err() first so the cancellation reason is preserved
		// in the chain.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, fmt.Errorf("apt: status %q: %w", pkg, ctxErr)
		}
		// Unknown package: exit 1. command.IsExitCode walks the wrap
		// chain (Executor.ExecuteCapture wraps cmd.Run()'s error via %w,
		// preserving *exec.ExitError) so we can distinguish it from
		// exit ≥ 2 (genuine failure).
		if command.IsExitCode(err, 1) {
			return false, nil
		}
		return false, fmt.Errorf("apt: status %q: %w", pkg, err)
	}

	// dpkg-query emits one line per matched package (multi-arch can yield
	// >1 line). Return true if any matching package's status sub-field is
	// exactly dpkgStatusInstalled (this includes "hold ok installed"
	// because we only extract the status sub-field via ${db:Status-Status}).
	for line := range strings.SplitSeq(output, "\n") {
		if strings.TrimSpace(line) == dpkgStatusInstalled {
			return true, nil
		}
	}
	return false, nil
}

// PackageVersion returns the dpkg-recorded version of an installed package.
// It is a read-only probe used by the SystemPackageSet reconciler (#198)
// to populate SystemPackageSetState.InstalledVersions for each managed
// package. NOTE: this is the version of the installed package per the
// dpkg database — NOT the version of apt-get itself (see VersionFunc).
//
// The shell command executed is:
//
//	dpkg-query -W -f='${db:Status-Status} ${Version}\n' -- <pkg>
//
// The combined `${db:Status-Status} ${Version}` format directive is
// deliberate: by emitting both the third Status sub-field and the
// Version on the same line, the helper can filter to only those
// packages whose status is exactly "installed". This protects against
// the "stale version" pitfall where `apt-get remove pkg` (without
// --purge) leaves an entry in dpkg db with status "config-files" and
// the prior Version field intact. A naive `${Version}`-only query would
// return that stale version even though the package is no longer
// installed — PackageVersion treats such cases as not-installed.
// Held packages (`apt-mark hold pkg` produces a Status of "hold ok
// installed") are correctly reported as installed because only the
// third Status sub-field is extracted; the helper behaves identically
// to IsInstalled in this respect.
//
// `${db:Status-Status}` requires dpkg 1.17.11+ (Debian 8 jessie / Ubuntu
// 16.04 LTS and later); tomei does not target older releases.
//
// Return values:
//   - exit 0, exactly one line whose status sub-field is "installed"
//     → (version, nil)
//   - exit 0, no line with status "installed" (e.g. only "config-files",
//     "not-installed", "half-installed") → ("", error
//     `apt: package %q is not installed`).
//   - exit 0, two or more lines with status "installed" (multi-arch
//     ambiguity, e.g. libc6:amd64 + libc6:i386) → ("", error
//     `apt: package %q is installed for multiple architectures;
//     specify arch using <pkg>:<arch> syntax`). A reconciler must
//     record version against an unambiguous identity to detect drift;
//     emitting a non-deterministic first-line would create flaky state.
//   - exit 1 (dpkg-query: "no packages found matching <pkg>") → ("",
//     wrapped `apt: package %q is not installed: %w`). Callers needing
//     to distinguish from genuine failures can use
//     command.IsExitCode(err, 1).
//   - exit ≥ 2 or a non-ExitError (dpkg-query missing, permission
//     denied, signal, ctx cancellation, runner-side template error)
//     → ("", wrapped `apt: version %q: %w`).
//
// Caller contract: when err is non-nil the returned version string is
// always "" and MUST NOT be interpreted as data. Callers MUST check
// err before consuming the version.
//
// pkg validation: rejected if empty or contains shell-meaningful
// characters per disallowedInPackageName (whitespace, metacharacters,
// expansion characters). The CUE layer's `=~"^\\S+$"` constraint
// (cuemodule/schema/schema.cue) is the upstream allow-list; this guard
// is defense-in-depth for non-CUE callers. ":" is intentionally
// permitted to support Debian multi-arch syntax (e.g. "libc6:amd64").
// A NUL byte in pkg is rejected by os/exec when constructing the
// subprocess argv ("exec: argument contains NUL") and surfaces as a
// wrapped error rather than reaching dpkg-query.
//
// Trust model: pkg is assumed to come from a trusted source (CUE
// manifest under the user's control, or another in-process caller) per
// command/executor.go's package-level Security Model. Callers exposing
// PackageVersion to untrusted input (e.g. an HTTP API) MUST add their
// own sanitization layer.
//
// Concurrency: dpkg-query reads /var/lib/dpkg/status (world-readable)
// and does not take the dpkg-frontend lock. dpkg uses atomic rename to
// update the status file, so dpkg-query can never observe a torn write
// — but a probe performed concurrently with apt-get install/remove may
// observe either the pre- or post-transaction state. Callers should
// consume the result idempotently.
//
// No sudo, no DEBIAN_FRONTEND: dpkg-query is unprivileged and never
// prompts.
func (c *Client) PackageVersion(ctx context.Context, pkg string) (string, error) {
	if pkg == "" {
		return "", errors.New("apt: empty package name")
	}
	if strings.ContainsAny(pkg, disallowedInPackageName) {
		return "", fmt.Errorf("apt: package %q contains disallowed characters", pkg)
	}

	// IMPORTANT: the format string is single-quoted in the shell argument
	// so sh leaves "${db:Status-Status}" and "${Version}" as literals for
	// dpkg-query to interpret. Changing this to double quotes would let
	// sh expand them against the host environment (almost certainly to
	// ""), silently breaking the helper. Keep it single-quoted.
	// `--` operand separator guards against any future widening of
	// allowed characters that could permit a leading `-`.
	cmd := `dpkg-query -W -f='${db:Status-Status} ${Version}\n' -- ` + pkg
	output, err := c.runner.ExecuteCapture(ctx, []string{cmd}, command.Vars{}, nil)
	if err != nil {
		// ctx cancellation/timeout surfaces as a signal-kill on dpkg-query
		// whose *exec.ExitError reports ExitCode() == -1. Without an
		// explicit ctx check the cancellation would silently fall through
		// to the generic wrap or be misclassified as exit 1 ("not
		// installed"), leaving callers unable to detect it via
		// errors.Is(err, context.Canceled / context.DeadlineExceeded).
		// Check ctx.Err() first so the cancellation reason is preserved
		// in the chain.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", fmt.Errorf("apt: version %q: %w", pkg, ctxErr)
		}
		// Unknown package: exit 1. command.IsExitCode walks the wrap
		// chain (Executor.ExecuteCapture wraps cmd.Run()'s error via
		// %w, preserving *exec.ExitError) so we can distinguish it
		// from exit ≥ 2 (genuine failure).
		if command.IsExitCode(err, 1) {
			return "", fmt.Errorf("apt: package %q is not installed: %w", pkg, err)
		}
		return "", fmt.Errorf("apt: version %q: %w", pkg, err)
	}

	// dpkg-query emits one line per matched package (multi-arch can
	// yield >1 line). Each line is "<status> <version>" — split with
	// strings.Fields to be robust to surrounding whitespace, and
	// retain only the lines whose status is dpkgStatusInstalled.
	var installedVersions []string
	for line := range strings.SplitSeq(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == dpkgStatusInstalled {
			installedVersions = append(installedVersions, fields[1])
		}
	}
	switch len(installedVersions) {
	case 0:
		return "", fmt.Errorf("apt: package %q is not installed", pkg)
	case 1:
		return installedVersions[0], nil
	default:
		return "", fmt.Errorf(
			"apt: package %q is installed for multiple architectures; specify arch using <pkg>:<arch> syntax",
			pkg,
		)
	}
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
//	sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get update
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
	cmd := "sudo -n " + aptGetEnvPrefix + " apt-get update"
	// nil callback drains stdout/stderr to io.Discard rather than
	// buffering in memory; consistent with Install/Remove, we never
	// surface apt-get's human-oriented output to callers.
	if err := c.runner.ExecuteWithOutput(ctx, []string{cmd}, command.Vars{}, nil, nil); err != nil {
		return fmt.Errorf("apt: update: %w", err)
	}
	return nil
}

// SimulateRemoveCascade returns the full list of packages apt-get would
// remove if asked to remove `pkgs`, including transitive cascades that
// apt's reverse-dependency handling pulls in. Used to pre-flight an
// upgrade drain so a cascade that would remove a package the manifest
// still relies on surfaces as a clear error before any host mutation.
//
// The shell command executed is:
//
//	sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get -s remove -y -o DPkg::Lock::Timeout=60 -- <pkgs>
//
// LC_ALL=C is load-bearing: apt prefixes each removal line with the
// literal "Remv " in C locale; non-C locales translate the prefix and
// would silently break the parser.
//
// The returned list is sorted and deduplicated. Empty input returns
// (nil, nil) without invoking apt-get.
func (c *Client) SimulateRemoveCascade(ctx context.Context, pkgs []string) ([]string, error) {
	if len(pkgs) == 0 {
		return nil, nil
	}
	for _, pkg := range pkgs {
		if pkg == "" {
			return nil, errors.New("apt: empty package name in simulate-remove list")
		}
		if strings.ContainsAny(pkg, disallowedInPackageName) {
			return nil, fmt.Errorf("apt: package %q contains disallowed characters", pkg)
		}
	}
	cmd := "sudo -n " + aptGetEnvPrefix +
		" apt-get -s remove -y -o DPkg::Lock::Timeout=60 -- " +
		strings.Join(pkgs, " ")
	output, err := c.runner.ExecuteCapture(ctx, []string{cmd}, command.Vars{}, nil)
	if err != nil {
		return nil, fmt.Errorf("apt: simulate-remove %q: %w", pkgs, err)
	}
	seen := make(map[string]bool)
	var removed []string
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		// apt's simulate-mode emits one "Remv PKGNAME [VERSION]" line per
		// package it would actually uninstall (target + cascades). Any
		// other apt status line (Inst, Conf, Reading, etc.) is ignored.
		if !strings.HasPrefix(line, "Remv ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		pkg := fields[1]
		if !seen[pkg] {
			seen[pkg] = true
			removed = append(removed, pkg)
		}
	}
	sort.Strings(removed)
	return removed, nil
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
// the new state. InstalledVersions is populated via dpkg-query per package
// after the install completes; a probe failure aborts the Install. A
// successful Install for which we cannot read versions is a state-store
// consistency bug, so we surface it rather than degrade to an empty map.
//
// The shell command executed is:
//
//	sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get install -y -o DPkg::Lock::Timeout=60 -- <packages>
//
// stdout/stderr are drained (discarded, not buffered in memory).
//
// Callers are responsible for ensuring a recent "apt-get update" has run
// when stale package indexes would cause 404s.
func (p *PackageSetInstaller) Install(ctx context.Context, res *resource.SystemPackageSet, name string) (*resource.SystemPackageSetState, error) {
	if res == nil || res.SystemPackageSetSpec == nil {
		return nil, fmt.Errorf("apt: package set %q: nil spec", name)
	}
	spec := res.SystemPackageSetSpec
	// Validate at the apt boundary as defense-in-depth: the CUE schema
	// and the spec's own Validate are upstream layers, but the apt
	// installer must not assume callers ran them. Mirrors the
	// PackageRepository installer's `apt: repository %q: %w` wrap.
	if err := spec.Validate(); err != nil {
		return nil, fmt.Errorf("apt: package set %q: %w", name, err)
	}
	// Pin the installer to APT at the boundary: SystemPackageSetSpec.Validate
	// only checks that installerRef is non-empty (the CUE schema layer
	// constrains the literal value upstream). Without this check, a non-
	// CUE caller — or a future selectPackageInstaller mis-routing — could
	// land a SystemPackageSet whose installerRef is "dnf" / "zypper" here
	// and tomei would happily run apt-get on it, then persist the non-APT
	// installerRef into state.
	if spec.InstallerRef != resource.InstallerRefApt {
		return nil, fmt.Errorf("apt: package set %q: installerRef %q is not %q", name, spec.InstallerRef, resource.InstallerRefApt)
	}
	// Snapshot pre-install state so a later rollback removes ONLY the
	// packages this Install action placed. Without this snapshot, a
	// probe failure for one package would uninstall every package in
	// the spec — including ones the user (or another SystemPackageSet)
	// had on the host before this apply. apt-get install no-ops on an
	// already-installed package, so the post-install state alone cannot
	// distinguish "this Install placed it" from "it was already here."
	wasInstalled := make(map[string]bool, len(spec.Packages))
	for _, pkg := range spec.Packages {
		installed, err := p.client.IsInstalled(ctx, pkg)
		if err != nil {
			return nil, fmt.Errorf("apt: package set %q: snapshot pre-install state of %q: %w", name, pkg, err)
		}
		wasInstalled[pkg] = installed
	}
	if err := p.runInstall(ctx, spec.Packages); err != nil {
		return nil, err
	}
	versions := make(map[string]string, len(spec.Packages))
	for _, pkg := range spec.Packages {
		v, err := p.client.PackageVersion(ctx, pkg)
		if err != nil {
			// runInstall placed the packages on the host, but the
			// post-install probe cannot read a consistent version map
			// — Install must not return a state that lies about what
			// is on the host. Mirror PackageRepositoryInstaller's
			// best-effort rollback (repository.go) and undo only the
			// packages this Install action newly placed.
			//
			// The rollback uses a fresh ctx with a bounded timeout
			// (not the caller's ctx) so a caller cancellation that
			// triggered the probe failure does not cascade into the
			// undo. The deadline is strictly GREATER than apt-get's own
			// DPkg::Lock::Timeout=60 so that under lock contention the
			// child apt-get reaches its lock-timeout error and returns
			// a meaningful exit status before this ctx kills it from
			// outside (which would surface only as ctx.DeadlineExceeded
			// and lose the lock-contention root cause). 90s = 60s lock
			// wait + 30s headroom for the rollback's own dpkg
			// transaction (worst-case ≪ install transaction).
			var toRollback []string
			for _, p := range spec.Packages {
				if !wasInstalled[p] {
					toRollback = append(toRollback, p)
				}
			}
			if len(toRollback) > 0 {
				rbCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
				if rbErr := p.runRemove(rbCtx, toRollback); rbErr != nil {
					slog.Warn("apt: package set rollback failed; host may have orphaned packages",
						"reason", "after post-install version probe failure",
						"name", name, "packages", toRollback, "rollback_error", rbErr, "trigger_error", err)
				}
				cancel()
			}
			return nil, fmt.Errorf("apt: package set %q: probe version of %q: %w", name, pkg, err)
		}
		versions[pkg] = v
	}
	// Upgrade-time package drainage runs LAST — after the new-spec install
	// and the per-package version probes have both succeeded. Two failure
	// modes are bounded by this ordering:
	//
	//   - runInstall fails → no drainage attempted, host stays at old
	//     state (matches state.json which still records old packages).
	//   - Version probe fails → the post-probe rollback removes only the
	//     newly-placed packages (delta from the pre-install snapshot);
	//     no drainage has happened yet, so dropped packages remain on the
	//     host. State.json still records old packages, host has old
	//     packages — state and host agree.
	//   - Drainage fails → Install returns error; state.json keeps the
	//     OLD package list (no save happened); host has old packages
	//     plus any newly-placed ones (re-applying will re-emit Upgrade
	//     and retry drainage; apt-get install is idempotent).
	//
	// Earlier drafts drained BEFORE install for symmetry with the
	// pre-install snapshot, but that ordering leaves a window where the
	// host has FEWER packages than state.json claims if install or probe
	// fails — the inverse of the orphan-packages problem and harder to
	// recover from because dropped packages do not auto-re-install.
	if oldPkgs := executor.OldPackagesFromContext(ctx); len(oldPkgs) > 0 {
		newSet := make(map[string]bool, len(spec.Packages))
		for _, pkg := range spec.Packages {
			newSet[pkg] = true
		}
		var toRemove []string
		for _, pkg := range oldPkgs {
			if !newSet[pkg] {
				toRemove = append(toRemove, pkg)
			}
		}
		if len(toRemove) > 0 {
			// Pre-flight the drain via apt-get -s to detect cascades.
			// apt's reverse-dependency handling can pull packages we
			// did NOT name into the removal — including packages the
			// new spec still relies on (e.g. shrinking [cowsay, perl]
			// → [cowsay] would normally cascade-remove cowsay because
			// cowsay depends on perl). Catching this BEFORE the actual
			// remove avoids a state-vs-host drift where state.json
			// records "cowsay installed" but the host no longer has it.
			cascade, err := p.client.SimulateRemoveCascade(ctx, toRemove)
			if err != nil {
				return nil, fmt.Errorf("apt: package set %q: simulate upgrade drain %v: %w", name, toRemove, err)
			}
			var cascadeIntoSpec []string
			for _, pkg := range cascade {
				if newSet[pkg] {
					cascadeIntoSpec = append(cascadeIntoSpec, pkg)
				}
			}
			if len(cascadeIntoSpec) > 0 {
				return nil, fmt.Errorf("apt: package set %q: upgrade drain %v would cascade-remove %v from the new spec; do not split dependent packages across SystemPackageSets (consider declaring the dependency chain in a single set)", name, toRemove, cascadeIntoSpec)
			}
			if err := p.runRemove(ctx, toRemove); err != nil {
				return nil, fmt.Errorf("apt: package set %q: upgrade drain %v: %w", name, toRemove, err)
			}
		}
	}
	return &resource.SystemPackageSetState{
		InstallerRef:      spec.InstallerRef,
		RepositoryRef:     spec.RepositoryRef,
		Packages:          slices.Clone(spec.Packages),
		InstalledVersions: versions,
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
//	sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get remove -y -o DPkg::Lock::Timeout=60 -- <packages>
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

// runInstall executes "sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get
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
	cmd := "sudo -n " + aptGetEnvPrefix +
		" apt-get install -y -o DPkg::Lock::Timeout=60 -- " +
		strings.Join(packages, " ")
	// nil callback drains stdout/stderr to io.Discard rather than buffering
	// in memory; apt-get install output can be large.
	if err := p.client.runner.ExecuteWithOutput(ctx, []string{cmd}, command.Vars{}, nil, nil); err != nil {
		return fmt.Errorf("apt: install %q: %w", packages, err)
	}
	return nil
}

// runRemove executes "sudo -n env DEBIAN_FRONTEND=noninteractive LC_ALL=C LANGUAGE=C apt-get
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
	cmd := "sudo -n " + aptGetEnvPrefix +
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
