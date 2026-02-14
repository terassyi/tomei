package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/resource"
)

func TestDiffUserStates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		old         *UserState
		current     *UserState
		wantChanges int
		check       func(t *testing.T, diff *Diff)
	}{
		{
			name:        "no changes",
			old:         &UserState{Version: Version},
			current:     &UserState{Version: Version},
			wantChanges: 0,
		},
		{
			name: "tool added",
			old:  &UserState{Version: Version},
			current: &UserState{
				Version: Version,
				Tools: map[string]*resource.ToolState{
					"gh": {Version: "2.86.0"},
				},
			},
			wantChanges: 1,
			check: func(t *testing.T, diff *Diff) {
				c := diff.Changes[0]
				assert.Equal(t, "tool", c.Kind)
				assert.Equal(t, "gh", c.Name)
				assert.Equal(t, DiffAdded, c.Type)
				assert.Equal(t, "2.86.0", c.NewVersion)
			},
		},
		{
			name: "tool removed",
			old: &UserState{
				Version: Version,
				Tools: map[string]*resource.ToolState{
					"jq": {Version: "1.7.1"},
				},
			},
			current:     &UserState{Version: Version},
			wantChanges: 1,
			check: func(t *testing.T, diff *Diff) {
				c := diff.Changes[0]
				assert.Equal(t, "tool", c.Kind)
				assert.Equal(t, "jq", c.Name)
				assert.Equal(t, DiffRemoved, c.Type)
				assert.Equal(t, "1.7.1", c.OldVersion)
			},
		},
		{
			name: "tool upgraded",
			old: &UserState{
				Version: Version,
				Tools: map[string]*resource.ToolState{
					"ripgrep": {Version: "14.0.0", InstallPath: "/path/rg"},
				},
			},
			current: &UserState{
				Version: Version,
				Tools: map[string]*resource.ToolState{
					"ripgrep": {Version: "14.1.0", InstallPath: "/path/rg"},
				},
			},
			wantChanges: 1,
			check: func(t *testing.T, diff *Diff) {
				c := diff.Changes[0]
				assert.Equal(t, DiffModified, c.Type)
				assert.Equal(t, "14.0.0", c.OldVersion)
				assert.Equal(t, "14.1.0", c.NewVersion)
				assert.Contains(t, c.Details, "version changed")
			},
		},
		{
			name: "tool unchanged",
			old: &UserState{
				Version: Version,
				Tools: map[string]*resource.ToolState{
					"gh": {Version: "2.86.0", InstallPath: "/path/gh", Digest: "sha256:abc"},
				},
			},
			current: &UserState{
				Version: Version,
				Tools: map[string]*resource.ToolState{
					"gh": {Version: "2.86.0", InstallPath: "/path/gh", Digest: "sha256:abc"},
				},
			},
			wantChanges: 0,
		},
		{
			name: "runtime added",
			old:  &UserState{Version: Version},
			current: &UserState{
				Version: Version,
				Runtimes: map[string]*resource.RuntimeState{
					"go": {Version: "1.25.2"},
				},
			},
			wantChanges: 1,
			check: func(t *testing.T, diff *Diff) {
				c := diff.Changes[0]
				assert.Equal(t, "runtime", c.Kind)
				assert.Equal(t, DiffAdded, c.Type)
				assert.Equal(t, "1.25.2", c.NewVersion)
			},
		},
		{
			name: "runtime upgraded",
			old: &UserState{
				Version: Version,
				Runtimes: map[string]*resource.RuntimeState{
					"go": {Version: "1.25.1", InstallPath: "/path/go/1.25.1"},
				},
			},
			current: &UserState{
				Version: Version,
				Runtimes: map[string]*resource.RuntimeState{
					"go": {Version: "1.25.2", InstallPath: "/path/go/1.25.2"},
				},
			},
			wantChanges: 1,
			check: func(t *testing.T, diff *Diff) {
				c := diff.Changes[0]
				assert.Equal(t, DiffModified, c.Type)
				assert.Contains(t, c.Details, "version changed")
				assert.Contains(t, c.Details, "installPath changed")
			},
		},
		{
			name: "registry added",
			old:  &UserState{Version: Version},
			current: &UserState{
				Version: Version,
				Registry: &RegistryState{
					Aqua: &AquaRegistryState{Ref: "v4.470.0"},
				},
			},
			wantChanges: 1,
			check: func(t *testing.T, diff *Diff) {
				c := diff.Changes[0]
				assert.Equal(t, "registry", c.Kind)
				assert.Equal(t, "aqua", c.Name)
				assert.Equal(t, DiffAdded, c.Type)
				assert.Equal(t, "v4.470.0", c.NewVersion)
			},
		},
		{
			name: "registry updated",
			old: &UserState{
				Version: Version,
				Registry: &RegistryState{
					Aqua: &AquaRegistryState{Ref: "v4.465.0"},
				},
			},
			current: &UserState{
				Version: Version,
				Registry: &RegistryState{
					Aqua: &AquaRegistryState{Ref: "v4.470.0"},
				},
			},
			wantChanges: 1,
			check: func(t *testing.T, diff *Diff) {
				c := diff.Changes[0]
				assert.Equal(t, DiffModified, c.Type)
				assert.Equal(t, "v4.465.0", c.OldVersion)
				assert.Equal(t, "v4.470.0", c.NewVersion)
			},
		},
		{
			name: "registry removed",
			old: &UserState{
				Version: Version,
				Registry: &RegistryState{
					Aqua: &AquaRegistryState{Ref: "v4.465.0"},
				},
			},
			current:     &UserState{Version: Version},
			wantChanges: 1,
			check: func(t *testing.T, diff *Diff) {
				c := diff.Changes[0]
				assert.Equal(t, DiffRemoved, c.Type)
				assert.Equal(t, "v4.465.0", c.OldVersion)
			},
		},
		{
			name: "installer repository added",
			old:  &UserState{Version: Version},
			current: &UserState{
				Version: Version,
				InstallerRepositories: map[string]*resource.InstallerRepositoryState{
					"helm-stable": {InstallerRef: "helm"},
				},
			},
			wantChanges: 1,
			check: func(t *testing.T, diff *Diff) {
				c := diff.Changes[0]
				assert.Equal(t, "installerRepository", c.Kind)
				assert.Equal(t, DiffAdded, c.Type)
			},
		},
		{
			name: "multiple changes",
			old: &UserState{
				Version: Version,
				Tools: map[string]*resource.ToolState{
					"jq":      {Version: "1.7.1"},
					"ripgrep": {Version: "14.0.0"},
				},
				Runtimes: map[string]*resource.RuntimeState{
					"go": {Version: "1.25.1", InstallPath: "/path/go"},
				},
			},
			current: &UserState{
				Version: Version,
				Tools: map[string]*resource.ToolState{
					"gh":      {Version: "2.86.0"},
					"ripgrep": {Version: "14.1.0"},
				},
				Runtimes: map[string]*resource.RuntimeState{
					"go": {Version: "1.25.2", InstallPath: "/path/go"},
				},
			},
			wantChanges: 4, // gh added, jq removed, ripgrep modified, go modified
		},
		{
			name:        "both nil maps",
			old:         &UserState{Version: Version},
			current:     &UserState{Version: Version},
			wantChanges: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			diff := DiffUserStates(tt.old, tt.current)
			require.Len(t, diff.Changes, tt.wantChanges)
			if tt.check != nil {
				tt.check(t, diff)
			}
		})
	}
}

func TestDiff_HasChanges(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		diff *Diff
		want bool
	}{
		{
			name: "no changes",
			diff: &Diff{},
			want: false,
		},
		{
			name: "has changes",
			diff: &Diff{Changes: []ResourceDiff{{Kind: "tool", Name: "gh", Type: DiffAdded}}},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.diff.HasChanges())
		})
	}
}

func TestDiff_Summary(t *testing.T) {
	t.Parallel()
	diff := &Diff{
		Changes: []ResourceDiff{
			{Type: DiffAdded},
			{Type: DiffAdded},
			{Type: DiffModified},
			{Type: DiffRemoved},
		},
	}

	added, modified, removed := diff.Summary()
	assert.Equal(t, 2, added)
	assert.Equal(t, 1, modified)
	assert.Equal(t, 1, removed)
}

func TestCollectKeys(t *testing.T) {
	t.Parallel()
	a := map[string]int{"b": 1, "a": 2}
	b := map[string]int{"c": 3, "a": 4}

	keys := collectKeys(a, b)
	assert.Equal(t, []string{"a", "b", "c"}, keys)
}
