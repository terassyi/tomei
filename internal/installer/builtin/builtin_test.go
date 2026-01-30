package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/resource"
)

func TestInstallers(t *testing.T) {
	installers := Installers()

	// Should have at least the "download" installer
	require.NotEmpty(t, installers)

	// Find "download" installer
	var downloadInstaller *resource.Installer
	for _, i := range installers {
		if i.Metadata.Name == "download" {
			downloadInstaller = i
			break
		}
	}

	require.NotNil(t, downloadInstaller, "download installer not found")

	// Verify download installer structure
	assert.Equal(t, resource.GroupVersion, downloadInstaller.APIVersion)
	assert.Equal(t, resource.KindInstaller, downloadInstaller.ResourceKind)
	assert.Equal(t, "download", downloadInstaller.Metadata.Name)

	// Verify spec
	require.NotNil(t, downloadInstaller.InstallerSpec)
	assert.Equal(t, resource.InstallerPatternDownload, downloadInstaller.InstallerSpec.Pattern)
	assert.Empty(t, downloadInstaller.InstallerSpec.RuntimeRef)
	assert.Nil(t, downloadInstaller.InstallerSpec.Bootstrap)
	assert.Nil(t, downloadInstaller.InstallerSpec.Commands)
}

func TestInstallers_AllValid(t *testing.T) {
	installers := Installers()

	for _, inst := range installers {
		t.Run(inst.Metadata.Name, func(t *testing.T) {
			// Verify required fields
			assert.NotEmpty(t, inst.APIVersion)
			assert.NotEmpty(t, inst.Metadata.Name)
			assert.NotNil(t, inst.InstallerSpec)

			// Validate spec
			err := inst.InstallerSpec.Validate()
			assert.NoError(t, err)
		})
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name        string
		installerID string
		wantNil     bool
	}{
		{
			name:        "download installer exists",
			installerID: "download",
			wantNil:     false,
		},
		{
			name:        "nonexistent installer",
			installerID: "nonexistent",
			wantNil:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inst := Get(tt.installerID)
			if tt.wantNil {
				assert.Nil(t, inst)
			} else {
				assert.NotNil(t, inst)
				assert.Equal(t, tt.installerID, inst.Metadata.Name)
			}
		})
	}
}

func TestIsBuiltin(t *testing.T) {
	tests := []struct {
		name        string
		installerID string
		want        bool
	}{
		{
			name:        "download is builtin",
			installerID: "download",
			want:        true,
		},
		{
			name:        "unknown is not builtin",
			installerID: "unknown",
			want:        false,
		},
		{
			name:        "empty is not builtin",
			installerID: "",
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsBuiltin(tt.installerID)
			assert.Equal(t, tt.want, got)
		})
	}
}
