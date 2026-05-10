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

// TestIsInstalled_RealSystem exercises Client.IsInstalled against the real
// dpkg database on a Linux runner. The "preinstalled fixture" pitfall
// — where an integration test using an "if already installed, skip"
// guard silently no-ops on every CI run because the fixture happens to
// be preinstalled on the runner image — does not apply here: IsInstalled
// is a read-only probe and host state is not mutated, so a preinstalled
// fixture is a feature, not a hazard.
//
//   - Phase 1 — bash: Essential: yes / Priority: required on Debian-family
//     distros, so guaranteed installed on any sane Linux runner. Also
//     serves as the single-quote regression guard for the production
//     command: if the format-string single quotes in apt.go ever change to
//     double quotes, sh interprets "${db:Status-Status}" as a parameter
//     expansion, fails with "Bad substitution" (exit 2), and IsInstalled
//     returns a wrapped error — flunking require.NoError here.
//   - Phase 2 — definitely-not-a-real-package-tomei-test: a fixed sentinel
//     chosen to be Debian-Policy-compliant but unlikely to exist in any
//     APT repository. Verifies the exit-1 → (false, nil) path. Do NOT
//     reuse this name in any install/remove integration test.
//
// Requires Linux + dpkg-query (no sudo, no apt). The install/remove cycle
// is covered by TestPackageSetInstaller_RealSystem in a separate file.
func TestIsInstalled_RealSystem(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("dpkg-query integration test requires Linux")
	}
	if _, err := exec.LookPath("dpkg-query"); err != nil {
		t.Skip("dpkg-query not found in PATH")
	}

	client := apt.New(command.NewExecutor(""))

	// Phase 1: preinstalled package
	got, err := client.IsInstalled(context.Background(), "bash")
	require.NoError(t, err)
	assert.True(t, got, "bash should be reported as installed on a Linux runner")

	// Phase 2: unknown package
	got, err = client.IsInstalled(
		context.Background(),
		"definitely-not-a-real-package-tomei-test",
	)
	require.NoError(t, err, "exit 1 (unknown package) must be mapped to (false, nil)")
	assert.False(t, got)
}
