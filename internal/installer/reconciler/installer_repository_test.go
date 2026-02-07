package reconciler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/resource"
)

func TestInstallerRepositoryReconciler_Install(t *testing.T) {
	repos := []*resource.InstallerRepository{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindInstallerRepository,
				Metadata:     resource.Metadata{Name: "bitnami"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "helm",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceDelegation,
					Commands: &resource.CommandSet{
						Install: "helm repo add bitnami https://charts.bitnami.com/bitnami",
					},
				},
			},
		},
	}

	states := make(map[string]*resource.InstallerRepositoryState)

	r := NewInstallerRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionInstall, actions[0].Type)
	assert.Equal(t, "bitnami", actions[0].Name)
}

func TestInstallerRepositoryReconciler_NoChange(t *testing.T) {
	repos := []*resource.InstallerRepository{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindInstallerRepository,
				Metadata:     resource.Metadata{Name: "bitnami"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "helm",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceDelegation,
				},
			},
		},
	}

	states := map[string]*resource.InstallerRepositoryState{
		"bitnami": {
			InstallerRef: "helm",
			SourceType:   resource.InstallerRepositorySourceDelegation,
			UpdatedAt:    time.Now(),
		},
	}

	r := NewInstallerRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Empty(t, actions)
}

func TestInstallerRepositoryReconciler_Upgrade_URLChanged(t *testing.T) {
	repos := []*resource.InstallerRepository{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindInstallerRepository,
				Metadata:     resource.Metadata{Name: "custom-registry"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "aqua",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceGit,
					URL:  "https://github.com/my-org/new-registry",
				},
			},
		},
	}

	states := map[string]*resource.InstallerRepositoryState{
		"custom-registry": {
			InstallerRef: "aqua",
			SourceType:   resource.InstallerRepositorySourceGit,
			URL:          "https://github.com/my-org/old-registry",
			UpdatedAt:    time.Now(),
		},
	}

	r := NewInstallerRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Equal(t, "custom-registry", actions[0].Name)
	assert.Contains(t, actions[0].Reason, "source URL changed")
}

func TestInstallerRepositoryReconciler_Upgrade_TypeChanged(t *testing.T) {
	repos := []*resource.InstallerRepository{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindInstallerRepository,
				Metadata:     resource.Metadata{Name: "my-repo"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "helm",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceGit,
					URL:  "https://example.com/repo",
				},
			},
		},
	}

	states := map[string]*resource.InstallerRepositoryState{
		"my-repo": {
			InstallerRef: "helm",
			SourceType:   resource.InstallerRepositorySourceDelegation,
			URL:          "https://example.com/repo", // same URL, different type
			UpdatedAt:    time.Now(),
		},
	}

	r := NewInstallerRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Contains(t, actions[0].Reason, "source type changed")
}

func TestInstallerRepositoryReconciler_Remove(t *testing.T) {
	repos := []*resource.InstallerRepository{} // empty spec

	states := map[string]*resource.InstallerRepositoryState{
		"bitnami": {
			InstallerRef:  "helm",
			SourceType:    resource.InstallerRepositorySourceDelegation,
			RemoveCommand: "helm repo remove bitnami",
			UpdatedAt:     time.Now(),
		},
	}

	r := NewInstallerRepositoryReconciler()
	actions := r.Reconcile(repos, states)

	require.Len(t, actions, 1)
	assert.Equal(t, resource.ActionRemove, actions[0].Type)
	assert.Equal(t, "bitnami", actions[0].Name)
}
