package system

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func newTestValidator(distro *DistroInfo, versionFuncs map[PackageManager]VersionFunc) *Validator {
	return NewValidator(distro, versionFuncs)
}

func mockVersionFunc(version string) VersionFunc {
	return func(_ context.Context) (string, error) {
		return version, nil
	}
}

func failingVersionFunc(msg string) VersionFunc {
	return func(_ context.Context) (string, error) {
		return "", fmt.Errorf("%s", msg)
	}
}

func newSystemInstaller(name string) *resource.SystemInstaller {
	return &resource.SystemInstaller{
		BaseResource: resource.BaseResource{
			Metadata: resource.Metadata{Name: name},
		},
	}
}

func TestValidator_Validate_AptOnDebian(t *testing.T) {
	t.Parallel()
	v := newTestValidator(
		&DistroInfo{ID: "debian"},
		map[PackageManager]VersionFunc{PackageManagerAPT: mockVersionFunc("2.6.1")},
	)

	state, err := v.Validate(context.Background(), newSystemInstaller("apt"))
	require.NoError(t, err)
	assert.Equal(t, "2.6.1", state.Version)
	assert.False(t, state.UpdatedAt.IsZero())
}

func TestValidator_Validate_AptOnUbuntu(t *testing.T) {
	t.Parallel()
	v := newTestValidator(
		&DistroInfo{ID: "ubuntu", IDLike: []string{"debian"}},
		map[PackageManager]VersionFunc{PackageManagerAPT: mockVersionFunc("2.7.14")},
	)

	state, err := v.Validate(context.Background(), newSystemInstaller("apt"))
	require.NoError(t, err)
	assert.Equal(t, "2.7.14", state.Version)
}

func TestValidator_Validate_AptOnMint(t *testing.T) {
	t.Parallel()
	v := newTestValidator(
		&DistroInfo{ID: "linuxmint", IDLike: []string{"ubuntu", "debian"}},
		map[PackageManager]VersionFunc{PackageManagerAPT: mockVersionFunc("2.4.12")},
	)

	state, err := v.Validate(context.Background(), newSystemInstaller("apt"))
	require.NoError(t, err)
	assert.Equal(t, "2.4.12", state.Version)
}

func TestValidator_Validate_AptOnFedora(t *testing.T) {
	t.Parallel()
	v := newTestValidator(
		&DistroInfo{ID: "fedora"},
		map[PackageManager]VersionFunc{PackageManagerAPT: mockVersionFunc("2.4.12")},
	)

	_, err := v.Validate(context.Background(), newSystemInstaller("apt"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
	assert.Contains(t, err.Error(), "fedora")
}

func TestValidator_Validate_UnknownInstaller(t *testing.T) {
	t.Parallel()
	v := newTestValidator(
		&DistroInfo{ID: "ubuntu", IDLike: []string{"debian"}},
		map[PackageManager]VersionFunc{PackageManagerAPT: mockVersionFunc("2.7.14")},
	)

	_, err := v.Validate(context.Background(), newSystemInstaller("brew"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown package manager")
}

func TestValidator_Validate_UnsupportedPM(t *testing.T) {
	t.Parallel()
	v := newTestValidator(
		&DistroInfo{ID: "debian"},
		map[PackageManager]VersionFunc{
			PackageManagerAPT: mockVersionFunc("2.6.1"),
			PackageManagerDNF: mockVersionFunc("4.0"),
		},
	)

	_, err := v.Validate(context.Background(), newSystemInstaller("dnf"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestValidator_Validate_VersionFuncError(t *testing.T) {
	t.Parallel()
	v := newTestValidator(
		&DistroInfo{ID: "debian"},
		map[PackageManager]VersionFunc{PackageManagerAPT: failingVersionFunc("command not found")},
	)

	_, err := v.Validate(context.Background(), newSystemInstaller("apt"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "command not found")
}

func TestValidator_SupportedInstallers_Debian(t *testing.T) {
	t.Parallel()
	v := newTestValidator(&DistroInfo{ID: "debian"}, nil)

	supported := v.SupportedInstallers()
	assert.Equal(t, []PackageManager{PackageManagerAPT}, supported)
}

func TestValidator_SupportedInstallers_Ubuntu(t *testing.T) {
	t.Parallel()
	v := newTestValidator(&DistroInfo{ID: "ubuntu", IDLike: []string{"debian"}}, nil)

	supported := v.SupportedInstallers()
	assert.Equal(t, []PackageManager{PackageManagerAPT}, supported)
}

func TestValidator_SupportedInstallers_UnknownDistro(t *testing.T) {
	t.Parallel()
	v := newTestValidator(&DistroInfo{ID: "custom-distro"}, nil)

	supported := v.SupportedInstallers()
	assert.Empty(t, supported)
}
