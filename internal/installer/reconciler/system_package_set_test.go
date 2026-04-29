package reconciler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func newSystemPackageSet(name, installerRef, repoRef string, packages []string) *resource.SystemPackageSet {
	return &resource.SystemPackageSet{
		BaseResource: resource.BaseResource{
			APIVersion:   "tomei.terassyi.net/v1beta1",
			ResourceKind: resource.KindSystemPackageSet,
			Metadata:     resource.Metadata{Name: name},
		},
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef:  installerRef,
			RepositoryRef: repoRef,
			Packages:      packages,
		},
	}
}

func dockerPackageSet() *resource.SystemPackageSet {
	return newSystemPackageSet("docker-packages", "apt", "docker",
		[]string{"docker-ce", "docker-ce-cli", "containerd.io"},
	)
}

func dockerPackageSetState() *resource.SystemPackageSetState {
	return &resource.SystemPackageSetState{
		InstallerRef:  "apt",
		RepositoryRef: "docker",
		Packages:      []string{"docker-ce", "docker-ce-cli", "containerd.io"},
		InstalledVersions: map[string]string{
			"docker-ce":     "5:24.0.7-1~ubuntu.22.04~jammy",
			"docker-ce-cli": "5:24.0.7-1~ubuntu.22.04~jammy",
			"containerd.io": "1.6.28-1",
		},
		UpdatedAt: time.Now(),
	}
}

func TestSystemPackageSetReconciler_Install(t *testing.T) {
	t.Parallel()
	sets := []*resource.SystemPackageSet{dockerPackageSet()}
	states := make(map[string]*resource.SystemPackageSetState)

	r := NewSystemPackageSetReconciler()
	actions := r.Reconcile(sets, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionInstall, actions[0].Type)
	assert.Equal(t, "docker-packages", actions[0].Name)
}

func TestSystemPackageSetReconciler_NoChange(t *testing.T) {
	t.Parallel()
	sets := []*resource.SystemPackageSet{dockerPackageSet()}
	states := map[string]*resource.SystemPackageSetState{
		"docker-packages": dockerPackageSetState(),
	}

	r := NewSystemPackageSetReconciler()
	actions := r.Reconcile(sets, states)

	require.Empty(t, actions)
}

func TestSystemPackageSetReconciler_NoChange_Reordered(t *testing.T) {
	t.Parallel()
	set := dockerPackageSet()
	set.SystemPackageSetSpec.Packages = []string{"containerd.io", "docker-ce-cli", "docker-ce"}

	sets := []*resource.SystemPackageSet{set}
	states := map[string]*resource.SystemPackageSetState{
		"docker-packages": dockerPackageSetState(),
	}

	r := NewSystemPackageSetReconciler()
	actions := r.Reconcile(sets, states)

	require.Empty(t, actions)
}

func TestSystemPackageSetReconciler_Upgrade_PackageAdded(t *testing.T) {
	t.Parallel()
	set := dockerPackageSet()
	set.SystemPackageSetSpec.Packages = append(set.SystemPackageSetSpec.Packages, "docker-buildx-plugin")

	sets := []*resource.SystemPackageSet{set}
	states := map[string]*resource.SystemPackageSetState{
		"docker-packages": dockerPackageSetState(),
	}

	r := NewSystemPackageSetReconciler()
	actions := r.Reconcile(sets, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "+docker-buildx-plugin")
}

func TestSystemPackageSetReconciler_Upgrade_PackageRemoved(t *testing.T) {
	t.Parallel()
	set := dockerPackageSet()
	set.SystemPackageSetSpec.Packages = []string{"docker-ce", "docker-ce-cli"} // removed containerd.io

	sets := []*resource.SystemPackageSet{set}
	states := map[string]*resource.SystemPackageSetState{
		"docker-packages": dockerPackageSetState(),
	}

	r := NewSystemPackageSetReconciler()
	actions := r.Reconcile(sets, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "-containerd.io")
}

func TestSystemPackageSetReconciler_Upgrade_PackageAddedAndRemoved(t *testing.T) {
	t.Parallel()
	set := dockerPackageSet()
	set.SystemPackageSetSpec.Packages = []string{"docker-ce", "docker-ce-cli", "docker-buildx-plugin"}

	sets := []*resource.SystemPackageSet{set}
	states := map[string]*resource.SystemPackageSetState{
		"docker-packages": dockerPackageSetState(),
	}

	r := NewSystemPackageSetReconciler()
	actions := r.Reconcile(sets, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "+docker-buildx-plugin")
	assert.Contains(t, actions[0].Reason, "-containerd.io")
}

func TestSystemPackageSetReconciler_Upgrade_InstallerRefChanged(t *testing.T) {
	t.Parallel()
	set := dockerPackageSet()
	set.SystemPackageSetSpec.InstallerRef = "dnf"

	sets := []*resource.SystemPackageSet{set}
	states := map[string]*resource.SystemPackageSetState{
		"docker-packages": dockerPackageSetState(),
	}

	r := NewSystemPackageSetReconciler()
	actions := r.Reconcile(sets, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "installerRef changed")
}

func TestSystemPackageSetReconciler_Upgrade_RepositoryRefChanged(t *testing.T) {
	t.Parallel()
	set := dockerPackageSet()
	set.SystemPackageSetSpec.RepositoryRef = "docker-new"

	sets := []*resource.SystemPackageSet{set}
	states := map[string]*resource.SystemPackageSetState{
		"docker-packages": dockerPackageSetState(),
	}

	r := NewSystemPackageSetReconciler()
	actions := r.Reconcile(sets, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "repositoryRef changed")
}

func TestSystemPackageSetReconciler_Remove(t *testing.T) {
	t.Parallel()
	sets := []*resource.SystemPackageSet{} // empty spec
	states := map[string]*resource.SystemPackageSetState{
		"docker-packages": dockerPackageSetState(),
	}

	r := NewSystemPackageSetReconciler()
	actions := r.Reconcile(sets, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionRemove, actions[0].Type)
	assert.Equal(t, "docker-packages", actions[0].Name)
}
