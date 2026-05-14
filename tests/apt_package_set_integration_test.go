//go:build integration

package tests

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/installer/apt"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/resource"
)

// TestPackageSetInstaller_RealSystem_InstallAndRemove exercises Install
// end-to-end against the real apt-get / dpkg stack, then exercises
// Remove. The headline additional surface beyond
// TestPackageSetInstaller_RealSystem (single-package, in
// apt_install_integration_test.go) is the post-install dpkg-query probe
// wired in #198: state.InstalledVersions must be populated with a
// non-empty version string for every package.
//
// Requires Linux + apt-get + dpkg + passwordless sudo (CI: ubuntu-latest).
//
// Package choice: `hello` (single package, zero new transitive deps).
// The earlier draft used `hello + cowsay`, but cowsay pulls in perl /
// perl-modules / etc.; the cleanup either had to use `--auto-remove`
// (over-broad — it can sweep any orphaned auto-installed packages a
// developer already had on the host) or leave those deps behind
// (under-broad — mutates the developer's machine). `hello` depends
// only on libc6 (always preinstalled), so plain `apt-get remove`
// restores the host fully without disturbing anything else.
//
// Multi-package install logic (loop over spec.Packages, command-line
// composition, per-package version probe) is independently covered by
// the table-driven mock tests in apt_test.go's TestPackageSetInstaller_Install
// "multiple packages" subtest — the integration test's job is to verify
// the real apt-get / dpkg shell path, not to re-test the loop body.
//
// A preinstall guard skips the whole test if `hello` is already present
// on a developer machine, so we never mutate a host that depends on it.
func TestPackageSetInstaller_RealSystem_InstallAndRemove(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("apt-get integration test requires Linux")
	}
	for _, bin := range []string{"apt-get", "dpkg", "sudo"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not found in PATH", bin)
		}
	}
	if err := exec.Command("sudo", "-n", "true").Run(); err != nil {
		t.Skip("passwordless sudo not available")
	}

	pkgs := []string{"hello"}
	for _, pkg := range pkgs {
		if err := exec.Command("dpkg", "-s", pkg).Run(); err == nil {
			t.Skipf("%s is already installed; skipping to avoid mutating the host environment", pkg)
		}
	}

	t.Cleanup(func() {
		// Belt-and-suspenders cleanup: raw exec so we don't rely on the
		// SUT's Remove path. Mirror the production invocation hardening
		// (DEBIAN_FRONTEND, lock-timeout, `--` operand separator).
		//
		// Per-package invocations rather than one batch call: if the
		// install left the host in a partial state (one of N packages
		// somehow placed, the rest not), batching could surface an
		// "Unable to locate package" / exit-100 on the missing ones and
		// strand the placed ones. Per-package isolates the failure to a
		// single binary so the others still get drained.
		//
		// Plain `apt-get remove` (no --auto-remove): the chosen test
		// package(s) have zero new transitive deps, so there is nothing
		// to sweep. --auto-remove would be over-broad — it can pick up
		// any orphaned auto-installed packages a developer already had
		// on the host, mutating state beyond what this test touched.
		for _, pkg := range pkgs {
			cleanup := exec.Command("sudo",
				"-n", "env", "DEBIAN_FRONTEND=noninteractive",
				"apt-get", "remove", "-y", "-o", "DPkg::Lock::Timeout=60", "--", pkg)
			if err := cleanup.Run(); err != nil {
				t.Logf("cleanup: apt-get remove %s: %v", pkg, err)
			}
		}
	})

	client := apt.New(command.NewExecutor(""))

	// Refresh the index first: minimal images / stale runner caches can
	// surface "Unable to locate package" or 404 errors that mask the
	// real subject under test. If Update itself fails (no network), skip.
	if err := client.Update(context.Background()); err != nil {
		t.Skipf("apt-get update failed (cannot run integration test): %v", err)
	}

	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: "apt",
			Packages:     pkgs,
		},
	}
	installer := client.PackageSetInstaller()

	state, err := installer.Install(context.Background(), res, "integration-set")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, pkgs, state.Packages)

	// Per-package version probe: the headline behavior wired in #198.
	// We assert non-empty rather than a specific version because the
	// archive shipping versions varies across Debian/Ubuntu releases.
	require.NotNil(t, state.InstalledVersions, "InstalledVersions must be populated after Install (#198)")
	for _, pkg := range pkgs {
		assert.NotEmpty(t, state.InstalledVersions[pkg],
			"InstalledVersions[%q] must be a non-empty version string", pkg)
	}

	// Cross-check against dpkg -l for each package; this is the
	// independent oracle that proves apt-get install actually ran.
	for _, pkg := range pkgs {
		out, dpkgErr := exec.Command("dpkg", "-l", pkg).CombinedOutput()
		require.NoError(t, dpkgErr, "dpkg -l %s output: %s", pkg, out)
		assert.Contains(t, string(out), "ii  "+pkg,
			"%s should be installed (status ii); got: %s", pkg, out)
	}

	require.NoError(t, installer.Remove(context.Background(), state, "integration-set"))

	// After Remove, dpkg -l should no longer show "ii  <pkg>" for any of
	// the packages. `dpkg -l` exits 0 on success, 1 when no matching
	// package exists (purged path), and 2+ on real errors. Exit 1 is an
	// acceptable non-zero status; anything else (or a non-ExitError such
	// as the dpkg binary missing / permission denied) is a genuine test
	// failure that must not be swallowed.
	for _, pkg := range pkgs {
		out, dpkgErr := exec.Command("dpkg", "-l", pkg).CombinedOutput()
		if dpkgErr != nil {
			var exitErr *exec.ExitError
			if !errors.As(dpkgErr, &exitErr) {
				require.NoError(t, dpkgErr, "dpkg -l invocation failed: %s", out)
			}
			require.Equal(t, 1, exitErr.ExitCode(),
				"dpkg -l %s returned unexpected exit %d: %s", pkg, exitErr.ExitCode(), out)
		}
		assert.NotContains(t, string(out), "ii  "+pkg,
			"%s should not be installed (status ii) after Remove; got: %s", pkg, out)
	}
}
