package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func TestValidatorInstallerAdapter_Remove(t *testing.T) {
	t.Parallel()
	adapter := &validatorInstallerAdapter{validator: nil}
	err := adapter.Remove(context.Background(), nil, "apt")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
	assert.Contains(t, err.Error(), "apt")
}

func TestSkipRepoInstaller(t *testing.T) {
	t.Parallel()
	installer := &skipRepoInstaller{}

	t.Run("Install", func(t *testing.T) {
		t.Parallel()
		_, err := installer.Install(context.Background(), nil, "docker-repo")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
		assert.Contains(t, err.Error(), "docker-repo")
	})

	t.Run("Remove", func(t *testing.T) {
		t.Parallel()
		err := installer.Remove(context.Background(), nil, "docker-repo")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})
}

func TestSkipPackageInstaller(t *testing.T) {
	t.Parallel()
	installer := &skipPackageInstaller{}

	t.Run("Install", func(t *testing.T) {
		t.Parallel()
		_, err := installer.Install(context.Background(), nil, "dev-tools")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
		assert.Contains(t, err.Error(), "dev-tools")
	})

	t.Run("Remove", func(t *testing.T) {
		t.Parallel()
		err := installer.Remove(context.Background(), nil, "dev-tools")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not yet implemented")
	})
}

func TestFilterSupportedSystemResources(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		supported, skipped := filterSupportedSystemResources(nil)
		assert.Empty(t, supported)
		assert.Empty(t, skipped)
	})

	t.Run("installer only", func(t *testing.T) {
		t.Parallel()
		resources := []resource.Resource{
			&resource.SystemInstaller{BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "apt"}}},
		}
		supported, skipped := filterSupportedSystemResources(resources)
		assert.Len(t, supported, 1)
		assert.Empty(t, skipped)
	})

	t.Run("mixed", func(t *testing.T) {
		t.Parallel()
		resources := []resource.Resource{
			&resource.SystemInstaller{BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "apt"}}},
			&resource.SystemPackageRepository{BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "docker-repo"}}},
			&resource.SystemPackageSet{BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "dev-tools"}}},
		}
		supported, skipped := filterSupportedSystemResources(resources)
		assert.Len(t, supported, 1)
		assert.Equal(t, "apt", supported[0].Name())
		assert.Len(t, skipped, 2)
	})
}
