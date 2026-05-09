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

// TestPackageSetInstaller_RealSystem installs `hello` via the apt
// PackageSetInstaller, verifies presence with dpkg, runs Remove, and
// re-verifies absence. t.Cleanup runs a raw apt-get remove as a
// belt-and-suspenders safety net. Requires Linux + apt-get + dpkg +
// passwordless sudo (CI: ubuntu-latest).
func TestPackageSetInstaller_RealSystem(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("apt-get integration test requires Linux")
	}
	for _, bin := range []string{"apt-get", "dpkg", "sudo"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not found in PATH", bin)
		}
	}
	// Mirror the precedent at cmd/tomei/apply.go (sudoHandler.Acquire) so dev
	// machines without passwordless sudo skip with a clear message instead of
	// failing later with a generic exit-status error.
	if err := exec.Command("sudo", "-n", "true").Run(); err != nil {
		t.Skip("passwordless sudo not available")
	}

	// `hello` (GNU hello) is the canonical apt smoke-test package: small,
	// no runtime dependencies, and not preinstalled on the GitHub
	// `ubuntu-latest` runner image (whereas `tree`, our previous choice,
	// is). On dev machines that already have it, we skip rather than risk
	// mutating the host.
	const pkg = "hello"
	if err := exec.Command("dpkg", "-s", pkg).Run(); err == nil {
		t.Skipf("%s is already installed; skipping to avoid mutating the host environment", pkg)
	}
	t.Cleanup(func() {
		// Belt-and-suspenders cleanup using raw exec to avoid depending on
		// installer.Remove (which is the primary subject under test below).
		// Mirror the production invocation's hardening (DEBIAN_FRONTEND,
		// lock-timeout, `--` operand separator) so the safety net stays
		// robust if pkg ever changes to something with shell-meaningful
		// chars or if the cleanup races with apt-daily.
		cleanup := exec.Command(
			"sudo", "-n", "env", "DEBIAN_FRONTEND=noninteractive",
			"apt-get", "remove", "-y", "-o", "DPkg::Lock::Timeout=60", "--", pkg,
		)
		if err := cleanup.Run(); err != nil {
			t.Logf("cleanup: apt-get remove %s: %v", pkg, err)
		}
	})

	// apt-get install can fail on minimal images / stale package indexes
	// ("Unable to locate package", 404). Refresh the index first; if that
	// fails (no network, etc.), skip rather than treat as test failure.
	updateOut, err := exec.Command("sudo", "-n", "env", "DEBIAN_FRONTEND=noninteractive", "apt-get", "update").CombinedOutput()
	if err != nil {
		t.Skipf("apt-get update failed (cannot run integration test): %v\noutput: %s", err, updateOut)
	}

	runner := command.NewExecutor("")
	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: "apt",
			Packages:     []string{pkg},
		},
	}
	installer := apt.New(runner).PackageSetInstaller()
	state, err := installer.Install(context.Background(), res, pkg+"-only")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, []string{pkg}, state.Packages)

	out, err := exec.Command("dpkg", "-l", pkg).CombinedOutput()
	require.NoError(t, err, "dpkg -l output: %s", out)
	assert.Contains(t, string(out), "ii  "+pkg, "%s should be installed (status ii); got: %s", pkg, out)

	require.NoError(t, installer.Remove(context.Background(), state, pkg+"-only"))

	// After remove, dpkg -l should no longer show "ii  <pkg>". dpkg-query
	// exits 0 on success, 1 when no matching package exists (purged
	// path), and 2+ on real errors. Exit 0 doesn't surface here as an
	// error; only exit 1 is an acceptable non-zero status, anything
	// else (or a non-ExitError such as dpkg binary missing / permission
	// denied) is a genuine test failure that must not be swallowed.
	out, dpkgErr := exec.Command("dpkg", "-l", pkg).CombinedOutput()
	if dpkgErr != nil {
		var exitErr *exec.ExitError
		if !errors.As(dpkgErr, &exitErr) {
			require.NoError(t, dpkgErr, "dpkg -l invocation failed: %s", out)
		}
		require.Equal(t, 1, exitErr.ExitCode(), "dpkg -l returned unexpected exit %d: %s", exitErr.ExitCode(), out)
	}
	assert.NotContains(t, string(out), "ii  "+pkg, "%s should not be installed (status ii) after Remove; got: %s", pkg, out)
}
