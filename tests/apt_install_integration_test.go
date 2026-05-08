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
)

// TestAptGetInstall_RealSystem installs `tree`, verifies via dpkg, and removes
// in t.Cleanup. Requires Linux + apt-get + passwordless sudo (CI: ubuntu-latest).
func TestAptGetInstall_RealSystem(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("apt-get integration test requires Linux")
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		t.Skip("apt-get not found in PATH")
	}

	const pkg = "tree"
	t.Cleanup(func() {
		// #191 (AptGetRemove) is not yet implemented; use raw exec for cleanup.
		_ = exec.Command("sudo", "-n", "apt-get", "remove", "-y", pkg).Run()
	})

	runner := command.NewExecutor("")
	err := apt.GetInstall(context.Background(), runner, []string{pkg})
	require.NoError(t, err)

	out, err := exec.Command("dpkg", "-l", pkg).CombinedOutput()
	require.NoError(t, err, "dpkg -l output: %s", out)
	assert.True(t, strings.Contains(string(out), "ii  "+pkg), "tree should be installed (status ii); got: %s", out)
}
