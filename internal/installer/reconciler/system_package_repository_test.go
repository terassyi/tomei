package reconciler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func newSystemPackageRepository(name, installerRef string, source resource.SourceConfig) *resource.SystemPackageRepository {
	return &resource.SystemPackageRepository{
		BaseResource: resource.BaseResource{
			APIVersion:   "tomei.terassyi.net/v1beta1",
			ResourceKind: resource.KindSystemPackageRepository,
			Metadata:     resource.Metadata{Name: name},
		},
		SystemPackageRepositorySpec: &resource.SystemPackageRepositorySpec{
			InstallerRef: installerRef,
			Source:       source,
		},
	}
}

func dockerSource() resource.SourceConfig {
	return resource.SourceConfig{
		URL:        "https://download.docker.com/linux/ubuntu",
		KeyURL:     "https://download.docker.com/linux/ubuntu/gpg",
		KeyHash:    "sha256:1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570",
		Suite:      "jammy",
		Components: []string{"stable"},
		Options:    map[string]string{"arch": "amd64"},
	}
}

func dockerRepo() *resource.SystemPackageRepository {
	return newSystemPackageRepository("docker", "apt", dockerSource())
}

func dockerRepoState() *resource.SystemPackageRepositoryState {
	return &resource.SystemPackageRepositoryState{
		InstallerRef:   "apt",
		Source:         dockerSource(),
		InstalledFiles: []string{"/usr/share/keyrings/docker.gpg", "/etc/apt/sources.list.d/docker.list"},
		UpdatedAt:      time.Now(),
	}
}

func TestSystemPackageRepositoryReconciler_Install(t *testing.T) {
	t.Parallel()
	repos := []*resource.SystemPackageRepository{dockerRepo()}
	states := make(map[string]*resource.SystemPackageRepositoryState)

	r := NewSystemPackageRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionInstall, actions[0].Type)
	assert.Equal(t, "docker", actions[0].Name)
}

func TestSystemPackageRepositoryReconciler_NoChange(t *testing.T) {
	t.Parallel()
	repos := []*resource.SystemPackageRepository{dockerRepo()}
	states := map[string]*resource.SystemPackageRepositoryState{
		"docker": dockerRepoState(),
	}

	r := NewSystemPackageRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Empty(t, actions)
}

func TestSystemPackageRepositoryReconciler_NoChange_NilVsEmptyOptions(t *testing.T) {
	t.Parallel()
	src := resource.SourceConfig{
		URL:        "https://example.com/repo",
		KeyURL:     "https://example.com/repo/gpg",
		KeyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
		Suite:      "stable",
		Components: []string{"main"},
	}
	repo := newSystemPackageRepository("simple", "apt", src)

	stateSrc := src
	stateSrc.Options = map[string]string{}

	repos := []*resource.SystemPackageRepository{repo}
	states := map[string]*resource.SystemPackageRepositoryState{
		"simple": {
			InstallerRef: "apt",
			Source:       stateSrc,
			UpdatedAt:    time.Now(),
		},
	}

	r := NewSystemPackageRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Empty(t, actions)
}

func TestSystemPackageRepositoryReconciler_Upgrade_URLChanged(t *testing.T) {
	t.Parallel()
	repo := dockerRepo()
	repo.SystemPackageRepositorySpec.Source.URL = "https://download.docker.com/linux/debian"

	repos := []*resource.SystemPackageRepository{repo}
	states := map[string]*resource.SystemPackageRepositoryState{
		"docker": dockerRepoState(),
	}

	r := NewSystemPackageRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "source URL changed")
}

func TestSystemPackageRepositoryReconciler_Upgrade_KeyURLChanged(t *testing.T) {
	t.Parallel()
	repo := dockerRepo()
	repo.SystemPackageRepositorySpec.Source.KeyURL = "https://new-key-server.example.com/gpg"

	repos := []*resource.SystemPackageRepository{repo}
	states := map[string]*resource.SystemPackageRepositoryState{
		"docker": dockerRepoState(),
	}

	r := NewSystemPackageRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "source key URL changed")
}

func TestSystemPackageRepositoryReconciler_Upgrade_KeyHashChanged(t *testing.T) {
	t.Parallel()
	repo := dockerRepo()
	repo.SystemPackageRepositorySpec.Source.KeyHash = "sha256:aaaa"

	repos := []*resource.SystemPackageRepository{repo}
	states := map[string]*resource.SystemPackageRepositoryState{
		"docker": dockerRepoState(),
	}

	r := NewSystemPackageRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "source key hash changed")
}

func TestSystemPackageRepositoryReconciler_Upgrade_SuiteChanged(t *testing.T) {
	t.Parallel()
	repo := dockerRepo()
	repo.SystemPackageRepositorySpec.Source.Suite = "noble"

	repos := []*resource.SystemPackageRepository{repo}
	states := map[string]*resource.SystemPackageRepositoryState{
		"docker": dockerRepoState(),
	}

	r := NewSystemPackageRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "source suite changed")
}

func TestSystemPackageRepositoryReconciler_Upgrade_ComponentsChanged(t *testing.T) {
	t.Parallel()
	repo := dockerRepo()
	repo.SystemPackageRepositorySpec.Source.Components = []string{"stable", "test"}

	repos := []*resource.SystemPackageRepository{repo}
	states := map[string]*resource.SystemPackageRepositoryState{
		"docker": dockerRepoState(),
	}

	r := NewSystemPackageRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "source components changed")
}

func TestSystemPackageRepositoryReconciler_Upgrade_OptionsChanged(t *testing.T) {
	t.Parallel()
	repo := dockerRepo()
	repo.SystemPackageRepositorySpec.Source.Options = map[string]string{"arch": "arm64"}

	repos := []*resource.SystemPackageRepository{repo}
	states := map[string]*resource.SystemPackageRepositoryState{
		"docker": dockerRepoState(),
	}

	r := NewSystemPackageRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "source options changed")
}

func TestSystemPackageRepositoryReconciler_Upgrade_InstallerRefChanged(t *testing.T) {
	t.Parallel()
	repo := dockerRepo()
	repo.SystemPackageRepositorySpec.InstallerRef = "dnf"

	repos := []*resource.SystemPackageRepository{repo}
	states := map[string]*resource.SystemPackageRepositoryState{
		"docker": dockerRepoState(),
	}

	r := NewSystemPackageRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "installerRef changed")
}

func TestSystemPackageRepositoryReconciler_Remove(t *testing.T) {
	t.Parallel()
	repos := []*resource.SystemPackageRepository{} // empty spec
	states := map[string]*resource.SystemPackageRepositoryState{
		"docker": dockerRepoState(),
	}

	r := NewSystemPackageRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionRemove, actions[0].Type)
	assert.Equal(t, "docker", actions[0].Name)
}
