//go:build integration

package tests

import (
	"context"
	"os/exec"
	"runtime"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/installer/apt"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/system"
)

func TestValidator_RealSystem(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != "linux" {
		t.Skip("os-release detection requires Linux")
	}

	distro, err := system.DetectDistro()
	require.NoError(t, err)

	runner := command.NewExecutor("")
	versionFuncs := map[system.PackageManager]system.VersionFunc{
		system.PackageManagerAPT: apt.New(runner).VersionFunc(),
	}

	v, err := system.NewValidator(distro, versionFuncs)
	require.NoError(t, err)

	if !slices.Contains(v.SupportedInstallers(), system.PackageManagerAPT) {
		t.Skip("apt is not supported on this system")
	}
	if _, err := exec.LookPath("apt-get"); err != nil {
		t.Skip("apt-get not found in PATH")
	}

	res := &resource.SystemInstaller{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "apt"},
		},
		SystemInstallerSpec: &resource.SystemInstallerSpec{
			Pattern: "delegation",
		},
	}

	state, err := v.Validate(context.Background(), res)
	require.NoError(t, err)

	assert.NotEmpty(t, state.Version)
	assert.False(t, state.UpdatedAt.IsZero())

	t.Logf("detected APT version: %s", state.Version)
	t.Logf("supported installers: %v", v.SupportedInstallers())
}
