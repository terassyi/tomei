//go:build integration

package tests

import (
	"context"
	"os/exec"
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

	if _, err := exec.LookPath("apt-get"); err != nil {
		t.Skip("apt-get not available")
	}

	runner := command.NewExecutor("")
	versionFuncs := map[system.PackageManager]system.VersionFunc{
		system.PackageManagerAPT: apt.VersionFunc(runner),
	}

	distro, err := system.DetectDistro()
	require.NoError(t, err)

	v := system.NewValidator(distro, versionFuncs)

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
