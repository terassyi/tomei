//go:build integration

package tests

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
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
		if err := exec.Command("sudo", "-n", "apt-get", "remove", "-y", pkg).Run(); err != nil {
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
	assert.True(t, strings.Contains(string(out), "ii  "+pkg), "%s should be installed (status ii); got: %s", pkg, out)

	require.NoError(t, installer.Remove(context.Background(), state, pkg+"-only"))

	// After remove, dpkg -l should no longer show "ii  <pkg>". dpkg -l may
	// exit 0 (entry retained as "rc") or non-zero (entry purged), so we
	// don't check the exit code — only that the install marker is gone.
	out, _ = exec.Command("dpkg", "-l", pkg).CombinedOutput()
	assert.False(t, strings.Contains(string(out), "ii  "+pkg), "%s should not be installed (status ii) after Remove; got: %s", pkg, out)
}
