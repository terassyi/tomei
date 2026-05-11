//go:build integration

package tests

import (
	"context"
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/installer/apt"
	"github.com/terassyi/tomei/internal/installer/command"
)

// TestPackageVersion_RealSystem exercises Client.PackageVersion against
// the real dpkg database on a Linux runner. PackageVersion is a
// read-only probe so the "preinstalled fixture" pitfall (where a
// fixture happens to be preinstalled on the runner image and an
// "if installed, skip" test silently no-ops) does not apply here —
// we are not mutating host state, so a preinstalled fixture is a
// feature, not a hazard.
//
//   - Phase 1 — bash: Essential: yes / Priority: required on
//     Debian-family distros, so guaranteed installed on any sane Linux
//     runner. Asserts the returned version is non-empty and contains
//     at least one digit (sanity check that we got an actual version
//     string and not a status keyword leaked through).
//   - Phase 2 — definitely-not-a-real-package-tomei-test: a fixed
//     sentinel chosen to be Debian-Policy-compliant but unlikely to
//     exist in any APT repository. Verifies the exit-1 → wrapped
//     "is not installed" mapping; the additional
//     command.IsExitCode(err, 1) assertion confirms the underlying
//     *exec.ExitError still reaches the public API through the wrap
//     chain. Do NOT reuse this name in any install/remove integration
//     test.
//
// Requires Linux + dpkg-query (no sudo, no apt). The install/remove
// cycle is covered by TestPackageSetInstaller_RealSystem in a separate
// file.
func TestPackageVersion_RealSystem(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dpkg-query integration test requires Linux")
	}
	if _, err := exec.LookPath("dpkg-query"); err != nil {
		t.Skip("dpkg-query not found in PATH")
	}

	client := apt.New(command.NewExecutor(""))

	// Phase 1: preinstalled package
	got, err := client.PackageVersion(context.Background(), "bash")
	require.NoError(t, err)
	assert.NotEmpty(t, got, "bash should report a non-empty version")
	assert.Regexp(t, `\d`, got, "version string should contain at least one digit")

	// Phase 2: unknown package → "is not installed" error,
	// chain still surfaces *exec.ExitError exit 1
	got, err = client.PackageVersion(
		context.Background(),
		"definitely-not-a-real-package-tomei-test",
	)
	require.Error(t, err)
	assert.Empty(t, got)
	assert.Contains(t, err.Error(), "is not installed")
	assert.True(t, command.IsExitCode(err, 1),
		"exit 1 (unknown package) must reach the API via the wrap chain")
}
