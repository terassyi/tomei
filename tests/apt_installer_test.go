//go:build integration

package tests

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/installer/apt"
	"github.com/terassyi/tomei/internal/resource"
)

func TestInstallerInstaller_Install_RealAPT(t *testing.T) {
	t.Parallel()

	installer := apt.NewInstallerInstaller()

	res := &resource.SystemInstaller{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: "apt"},
		},
		SystemInstallerSpec: &resource.SystemInstallerSpec{
			Pattern:    "delegation",
			Privileged: true,
		},
	}

	state, err := installer.Install(context.Background(), res, "apt")
	require.NoError(t, err)

	assert.NotEmpty(t, state.Version)
	assert.False(t, state.UpdatedAt.IsZero())

	t.Logf("detected APT version: %s", state.Version)
}

func TestInstallerInstaller_Remove_RealAPT(t *testing.T) {
	t.Parallel()

	installer := apt.NewInstallerInstaller()

	st := &resource.SystemInstallerState{
		Version: "2.4.12",
	}

	err := installer.Remove(context.Background(), st, "apt")
	require.NoError(t, err)
}
