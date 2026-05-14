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
// end-to-end with a multi-package set against the real apt-get / dpkg
// stack, then exercises Remove. The key additional surface beyond
// TestPackageSetInstaller_RealSystem (single-package, in
// apt_install_integration_test.go) is the post-install per-package
// dpkg-query probe wired in #198: state.InstalledVersions must be
// populated with a non-empty version string for every package.
//
// Requires Linux + apt-get + dpkg + passwordless sudo (CI: ubuntu-latest).
//
// Package choice: hello + cowsay. The previous canonical "safe test"
// pair (tree + jq) is unsafe on the GitHub `ubuntu-latest` runner image
// where `tree` is preinstalled — installing it would no-op and the test
// would not exercise the install path. hello and cowsay are both small,
// have no significant runtime dependencies, and are not preinstalled on
// the runner image. A per-package preinstall guard skips the whole test
// if either is already present on a developer machine, so we never
// mutate a host that already depends on these binaries.
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

	pkgs := []string{"hello", "cowsay"}
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
