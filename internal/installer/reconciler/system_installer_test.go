package reconciler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func newSystemInstaller(name string) *resource.SystemInstaller {
	return &resource.SystemInstaller{
		BaseResource: resource.BaseResource{
			APIVersion:   "tomei.terassyi.net/v1beta1",
			ResourceKind: resource.KindSystemInstaller,
			Metadata:     resource.Metadata{Name: name},
		},
		SystemInstallerSpec: &resource.SystemInstallerSpec{
			Pattern: "delegation",
		},
	}
}

func TestSystemInstallerReconciler_Install(t *testing.T) {
	t.Parallel()
	installers := []*resource.SystemInstaller{newSystemInstaller("apt")}
	states := make(map[string]*resource.SystemInstallerState)

	r := NewSystemInstallerReconciler()
	actions := r.Reconcile(installers, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionInstall, actions[0].Type)
	assert.Equal(t, "apt", actions[0].Name)
}

func TestSystemInstallerReconciler_NoChange(t *testing.T) {
	t.Parallel()
	installers := []*resource.SystemInstaller{newSystemInstaller("apt")}
	states := map[string]*resource.SystemInstallerState{
		"apt": {Version: "2.6.1", UpdatedAt: time.Now()},
	}

	r := NewSystemInstallerReconciler()
	actions := r.Reconcile(installers, states)

	require.Empty(t, actions)
}

func TestSystemInstallerReconciler_VersionChanged_StillNoOp(t *testing.T) {
	t.Parallel()
	installers := []*resource.SystemInstaller{newSystemInstaller("apt")}
	states := map[string]*resource.SystemInstallerState{
		"apt": {Version: "2.4.0", UpdatedAt: time.Now()},
	}

	// Even if the detected version changes, the comparator returns false
	// because SystemInstaller is OS-managed; tomei does not upgrade it.
	r := NewSystemInstallerReconciler()
	actions := r.Reconcile(installers, states)

	require.Empty(t, actions)
}

func TestSystemInstallerReconciler_Remove(t *testing.T) {
	t.Parallel()
	installers := []*resource.SystemInstaller{} // empty spec
	states := map[string]*resource.SystemInstallerState{
		"apt": {Version: "2.6.1", UpdatedAt: time.Now()},
	}

	r := NewSystemInstallerReconciler()
	actions := r.Reconcile(installers, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionRemove, actions[0].Type)
	assert.Equal(t, "apt", actions[0].Name)
}
