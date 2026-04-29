package reconciler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func newSystemPackageRepository(name, installerRef, url, keyURL, keyHash string, options map[string]string) *resource.SystemPackageRepository {
	return &resource.SystemPackageRepository{
		BaseResource: resource.BaseResource{
			APIVersion:   "tomei.terassyi.net/v1beta1",
			ResourceKind: resource.KindSystemPackageRepository,
			Metadata:     resource.Metadata{Name: name},
		},
		SystemPackageRepositorySpec: &resource.SystemPackageRepositorySpec{
			InstallerRef: installerRef,
			Source: resource.SourceConfig{
				URL:     url,
				KeyURL:  keyURL,
				KeyHash: keyHash,
				Options: options,
			},
		},
	}
}

func dockerRepo() *resource.SystemPackageRepository {
	return newSystemPackageRepository(
		"docker", "apt",
		"https://download.docker.com/linux/ubuntu",
		"https://download.docker.com/linux/ubuntu/gpg",
		"sha256:1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570",
		map[string]string{"arch": "amd64"},
	)
}

func dockerRepoState() *resource.SystemPackageRepositoryState {
	return &resource.SystemPackageRepositoryState{
		InstallerRef: "apt",
		Source: resource.SourceConfig{
			URL:     "https://download.docker.com/linux/ubuntu",
			KeyURL:  "https://download.docker.com/linux/ubuntu/gpg",
			KeyHash: "sha256:1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570",
			Options: map[string]string{"arch": "amd64"},
		},
		InstalledFiles: []string{"/usr/share/keyrings/docker.gpg"},
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
	repo := newSystemPackageRepository("simple", "apt", "https://example.com/repo", "", "", nil)

	repos := []*resource.SystemPackageRepository{repo}
	states := map[string]*resource.SystemPackageRepositoryState{
		"simple": {
			InstallerRef: "apt",
			Source: resource.SourceConfig{
				URL:     "https://example.com/repo",
				Options: map[string]string{},
			},
			UpdatedAt: time.Now(),
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
