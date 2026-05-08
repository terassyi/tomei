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
	// If pkg was already installed before the test (e.g. on a developer
	// machine where it's part of their environment), skip the post-test
	// removal so we leave the system as we found it.
	preinstalled := exec.Command("dpkg", "-s", pkg).Run() == nil
	t.Cleanup(func() {
		if preinstalled {
			return
		}
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
