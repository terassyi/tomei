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

// TestPackageSetInstaller_RealSystem installs `tree` via the apt
// PackageSetInstaller, verifies presence with dpkg, and removes in
// t.Cleanup. Requires Linux + apt-get + dpkg + passwordless sudo
// (CI: ubuntu-latest).
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

	const pkg = "tree"
	// If pkg is already installed (developer environment), skip the whole
	// test rather than just the cleanup: apt-get install could still
	// upgrade the package, and a cleanup-skip would leave the upgrade in
	// place. CI ubuntu-latest ships without tree, so the install path
	// runs normally there.
	if err := exec.Command("dpkg", "-s", pkg).Run(); err == nil {
		t.Skipf("%s is already installed; skipping to avoid mutating the host environment", pkg)
	}
	t.Cleanup(func() {
		// AptGetRemove is tracked separately; use raw exec for test cleanup.
		if err := exec.Command("sudo", "-n", "apt-get", "remove", "-y", pkg).Run(); err != nil {
			t.Logf("cleanup: apt-get remove %s: %v", pkg, err)
		}
	})

	runner := command.NewExecutor("")
	res := &resource.SystemPackageSet{
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: "apt",
			Packages:     []string{pkg},
		},
	}
	state, err := apt.New(runner).PackageSetInstaller().Install(context.Background(), res, "tree-only")
	require.NoError(t, err)
	require.NotNil(t, state)
	assert.Equal(t, []string{pkg}, state.Packages)

	out, err := exec.Command("dpkg", "-l", pkg).CombinedOutput()
	require.NoError(t, err, "dpkg -l output: %s", out)
	assert.True(t, strings.Contains(string(out), "ii  "+pkg), "tree should be installed (status ii); got: %s", out)
}
