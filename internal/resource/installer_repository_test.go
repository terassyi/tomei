package resource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallerRepositorySpec_Validate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		spec    InstallerRepositorySpec
		wantErr string
	}{
		{
			name: "valid delegation with commands",
			spec: InstallerRepositorySpec{
				InstallerRef: "helm",
				Source: InstallerRepositorySourceSpec{
					Type: InstallerRepositorySourceDelegation,
					Commands: &CommandSet{
						Install: []string{"helm repo add bitnami https://charts.bitnami.com/bitnami"},
						Check:   []string{"helm repo list | grep bitnami"},
						Remove:  []string{"helm repo remove bitnami"},
					},
				},
			},
		},
		{
			name: "valid git with url",
			spec: InstallerRepositorySpec{
				InstallerRef: "aqua",
				Source: InstallerRepositorySourceSpec{
					Type: InstallerRepositorySourceGit,
					URL:  "https://github.com/my-org/aqua-registry",
				},
			},
		},
		{
			name: "missing installerRef",
			spec: InstallerRepositorySpec{
				Source: InstallerRepositorySourceSpec{
					Type: InstallerRepositorySourceDelegation,
					Commands: &CommandSet{
						Install: []string{"helm repo add bitnami https://charts.bitnami.com/bitnami"},
					},
				},
			},
			wantErr: "installerRef is required",
		},
		{
			name: "missing source type",
			spec: InstallerRepositorySpec{
				InstallerRef: "helm",
				Source:       InstallerRepositorySourceSpec{},
			},
			wantErr: "source.type is required",
		},
		{
			name: "invalid source type",
			spec: InstallerRepositorySpec{
				InstallerRef: "helm",
				Source: InstallerRepositorySourceSpec{
					Type: "invalid",
				},
			},
			wantErr: "source.type must be 'delegation' or 'git', got \"invalid\"",
		},
		{
			name: "delegation without commands",
			spec: InstallerRepositorySpec{
				InstallerRef: "helm",
				Source: InstallerRepositorySourceSpec{
					Type: InstallerRepositorySourceDelegation,
				},
			},
			wantErr: "source.commands.install is required for delegation type",
		},
		{
			name: "delegation with empty install command",
			spec: InstallerRepositorySpec{
				InstallerRef: "helm",
				Source: InstallerRepositorySourceSpec{
					Type:     InstallerRepositorySourceDelegation,
					Commands: &CommandSet{},
				},
			},
			wantErr: "source.commands.install is required for delegation type",
		},
		{
			name: "git without url",
			spec: InstallerRepositorySpec{
				InstallerRef: "aqua",
				Source: InstallerRepositorySourceSpec{
					Type: InstallerRepositorySourceGit,
				},
			},
			wantErr: "source.url is required for git type",
		},
		{
			name: "delegation with only install command",
			spec: InstallerRepositorySpec{
				InstallerRef: "krew",
				Source: InstallerRepositorySourceSpec{
					Type: InstallerRepositorySourceDelegation,
					Commands: &CommandSet{
						Install: []string{"kubectl krew index add my-index https://github.com/my-org/krew-index.git"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.spec.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

func TestInstallerRepositorySpec_Dependencies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		spec InstallerRepositorySpec
		want []Ref
	}{
		{
			name: "depends on installer",
			spec: InstallerRepositorySpec{
				InstallerRef: "helm",
			},
			want: []Ref{{Kind: KindInstaller, Name: "helm"}},
		},
		{
			name: "depends on aqua installer",
			spec: InstallerRepositorySpec{
				InstallerRef: "aqua",
			},
			want: []Ref{{Kind: KindInstaller, Name: "aqua"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.spec.Dependencies()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInstallerRepository_Kind(t *testing.T) {
	t.Parallel()
	repo := &InstallerRepository{}
	assert.Equal(t, KindInstallerRepository, repo.Kind())
}

func TestInstallerRepository_Spec(t *testing.T) {
	t.Parallel()
	spec := &InstallerRepositorySpec{
		InstallerRef: "helm",
		Source: InstallerRepositorySourceSpec{
			Type: InstallerRepositorySourceDelegation,
			Commands: &CommandSet{
				Install: []string{"helm repo add bitnami https://charts.bitnami.com/bitnami"},
			},
		},
	}
	repo := &InstallerRepository{
		BaseResource: BaseResource{
			Metadata: Metadata{Name: "bitnami"},
		},
		InstallerRepositorySpec: spec,
	}
	assert.Equal(t, "bitnami", repo.Name())
	assert.Equal(t, spec, repo.Spec())
}

func TestInstallerRepositoryState_IsState(t *testing.T) {
	t.Parallel()
	// Verify InstallerRepositoryState satisfies the State interface
	var _ State = (*InstallerRepositoryState)(nil)
}
