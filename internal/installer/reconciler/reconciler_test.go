package reconciler

import (
	"testing"
	"testing/quick"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/toto/internal/resource"
)

func TestNew(t *testing.T) {
	r := NewToolReconciler()
	assert.NotNil(t, r)
}

func TestReconciler_Reconcile_Install(t *testing.T) {
	// Tool exists in spec but not in state -> Install
	tools := []*resource.Tool{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "ripgrep"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.1.1",
			},
		},
	}

	states := make(map[string]*resource.ToolState)

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	require.Len(t, actions, 1)

	assert.Equal(t, resource.ActionInstall, actions[0].Type)
	assert.Equal(t, "ripgrep", actions[0].Name)
}

func TestReconciler_Reconcile_Upgrade(t *testing.T) {
	// Tool exists in both but version differs -> Upgrade
	tools := []*resource.Tool{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "ripgrep"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.2.0", // New version
			},
		},
	}

	states := map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "download",
			Version:      "14.1.1", // Old version
			VersionKind:  resource.VersionExact,
			SpecVersion:  "14.1.1",
			InstallPath:  "/path/to/rg",
			BinPath:      "/bin/rg",
			UpdatedAt:    time.Now(),
		},
	}

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	require.Len(t, actions, 1)

	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Equal(t, "ripgrep", actions[0].Name)
}

func TestReconciler_Reconcile_Skip(t *testing.T) {
	// Tool exists in both with same version -> Skip (no action)
	tools := []*resource.Tool{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "ripgrep"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.1.1",
			},
		},
	}

	states := map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "download",
			Version:      "14.1.1", // Same version
			VersionKind:  resource.VersionExact,
			SpecVersion:  "14.1.1",
			InstallPath:  "/path/to/rg",
			BinPath:      "/bin/rg",
			UpdatedAt:    time.Now(),
		},
	}

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	assert.Empty(t, actions) // No actions needed
}

func TestReconciler_Reconcile_Remove(t *testing.T) {
	// Tool exists in state but not in spec -> Remove
	tools := []*resource.Tool{} // Empty spec

	states := map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "download",
			Version:      "14.1.1",
			VersionKind:  resource.VersionExact,
			SpecVersion:  "14.1.1",
			InstallPath:  "/path/to/rg",
			BinPath:      "/bin/rg",
			UpdatedAt:    time.Now(),
		},
	}

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	require.Len(t, actions, 1)

	assert.Equal(t, resource.ActionRemove, actions[0].Type)
	assert.Equal(t, "ripgrep", actions[0].Name)
}

func TestReconciler_Reconcile_MultipleTools(t *testing.T) {
	// Multiple tools with different actions
	tools := []*resource.Tool{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "ripgrep"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.2.0", // Upgrade
			},
		},
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "fd"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "9.0.0", // New install
			},
		},
	}

	states := map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "download",
			Version:      "14.1.1",
			VersionKind:  resource.VersionExact,
			SpecVersion:  "14.1.1",
			InstallPath:  "/path/to/rg",
			BinPath:      "/bin/rg",
			UpdatedAt:    time.Now(),
		},
		"bat": {
			InstallerRef: "download",
			Version:      "0.24.0",
			VersionKind:  resource.VersionExact,
			SpecVersion:  "0.24.0",
			InstallPath:  "/path/to/bat",
			BinPath:      "/bin/bat",
			UpdatedAt:    time.Now(),
		},
	}

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	require.Len(t, actions, 3) // Upgrade ripgrep, Install fd, Remove bat

	// Find actions by name
	actionMap := make(map[string]*Action[*resource.Tool, *resource.ToolState])
	for i := range actions {
		actionMap[actions[i].Name] = &actions[i]
	}

	assert.Equal(t, resource.ActionUpgrade, actionMap["ripgrep"].Type)
	assert.Equal(t, resource.ActionInstall, actionMap["fd"].Type)
	assert.Equal(t, resource.ActionRemove, actionMap["bat"].Type)
}

func TestReconciler_Reconcile_Tainted(t *testing.T) {
	// Tool is tainted -> Reinstall (upgrade action)
	tools := []*resource.Tool{
		{
			BaseResource: resource.BaseResource{
				APIVersion:   "toto.terassyi.net/v1beta1",
				ResourceKind: resource.KindTool,
				Metadata:     resource.Metadata{Name: "ripgrep"},
			},
			ToolSpec: &resource.ToolSpec{
				InstallerRef: "download",
				Version:      "14.1.1",
			},
		},
	}

	states := map[string]*resource.ToolState{
		"ripgrep": {
			InstallerRef: "download",
			Version:      "14.1.1", // Same version
			VersionKind:  resource.VersionExact,
			SpecVersion:  "14.1.1",
			InstallPath:  "/path/to/rg",
			BinPath:      "/bin/rg",
			TaintReason:  "runtime_upgraded",
			UpdatedAt:    time.Now(),
		},
	}

	r := NewToolReconciler()
	actions := r.Reconcile(tools, states)

	require.Len(t, actions, 1)

	assert.Equal(t, resource.ActionUpgrade, actions[0].Type)
	assert.Equal(t, "ripgrep", actions[0].Name)
	assert.Contains(t, actions[0].Reason, "tainted")
}

// --- specVersionChanged unit tests ---

func TestSpecVersionChanged(t *testing.T) {
	tests := []struct {
		name             string
		specVersion      string
		stateVersionKind resource.VersionKind
		stateVersion     string
		stateSpecVersion string
		want             bool
	}{
		// VersionExact cases
		{
			name:             "exact: same version",
			specVersion:      "14.1.1",
			stateVersionKind: resource.VersionExact,
			stateVersion:     "14.1.1",
			stateSpecVersion: "14.1.1",
			want:             false,
		},
		{
			name:             "exact: version changed",
			specVersion:      "14.2.0",
			stateVersionKind: resource.VersionExact,
			stateVersion:     "14.1.1",
			stateSpecVersion: "14.1.1",
			want:             true,
		},
		{
			name:             "exact: spec changed to empty (latest)",
			specVersion:      "",
			stateVersionKind: resource.VersionExact,
			stateVersion:     "14.1.1",
			stateSpecVersion: "14.1.1",
			want:             true,
		},

		// VersionLatest cases
		{
			name:             "latest: spec still empty - no change",
			specVersion:      "",
			stateVersionKind: resource.VersionLatest,
			stateVersion:     "2.86.0",
			stateSpecVersion: "",
			want:             false,
		},
		{
			name:             "latest: spec changed to explicit version",
			specVersion:      "2.87.0",
			stateVersionKind: resource.VersionLatest,
			stateVersion:     "2.86.0",
			stateSpecVersion: "",
			want:             true,
		},
		{
			name:             "latest: spec changed to alias",
			specVersion:      "stable",
			stateVersionKind: resource.VersionLatest,
			stateVersion:     "2.86.0",
			stateSpecVersion: "",
			want:             true,
		},

		// VersionAlias cases
		{
			name:             "alias: same alias - no change",
			specVersion:      "stable",
			stateVersionKind: resource.VersionAlias,
			stateVersion:     "1.83.0",
			stateSpecVersion: "stable",
			want:             false,
		},
		{
			name:             "alias: changed to explicit version",
			specVersion:      "1.84.0",
			stateVersionKind: resource.VersionAlias,
			stateVersion:     "1.83.0",
			stateSpecVersion: "stable",
			want:             true,
		},
		{
			name:             "alias: changed to different alias",
			specVersion:      "nightly",
			stateVersionKind: resource.VersionAlias,
			stateVersion:     "1.83.0",
			stateSpecVersion: "stable",
			want:             true,
		},
		{
			name:             "alias: changed to empty (latest)",
			specVersion:      "",
			stateVersionKind: resource.VersionAlias,
			stateVersion:     "1.83.0",
			stateSpecVersion: "stable",
			want:             true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := specVersionChanged(tt.specVersion, tt.stateVersionKind, tt.stateVersion, tt.stateSpecVersion)
			assert.Equal(t, tt.want, got)
		})
	}
}

// --- ToolComparator tests with VersionKind ---

func TestToolComparator_VersionKind(t *testing.T) {
	tests := []struct {
		name        string
		specVersion string
		stateVer    string
		versionKind resource.VersionKind
		specVer     string
		tainted     bool
		wantUpdate  bool
		wantReason  string
	}{
		{
			name:        "exact: same version - no change",
			specVersion: "14.1.1",
			stateVer:    "14.1.1",
			versionKind: resource.VersionExact,
			specVer:     "14.1.1",
			wantUpdate:  false,
		},
		{
			name:        "exact: version changed",
			specVersion: "14.2.0",
			stateVer:    "14.1.1",
			versionKind: resource.VersionExact,
			specVer:     "14.1.1",
			wantUpdate:  true,
			wantReason:  "version changed",
		},
		{
			name:        "exact: tainted triggers update",
			specVersion: "14.1.1",
			stateVer:    "14.1.1",
			versionKind: resource.VersionExact,
			specVer:     "14.1.1",
			tainted:     true,
			wantUpdate:  true,
			wantReason:  "tainted",
		},
		{
			name:        "latest: spec still empty - no change",
			specVersion: "",
			stateVer:    "2.86.0",
			versionKind: resource.VersionLatest,
			specVer:     "",
			wantUpdate:  false,
		},
		{
			name:        "latest: tainted triggers update",
			specVersion: "",
			stateVer:    "2.86.0",
			versionKind: resource.VersionLatest,
			specVer:     "",
			tainted:     true,
			wantUpdate:  true,
			wantReason:  "tainted",
		},
		{
			name:        "latest: spec changed to explicit",
			specVersion: "2.87.0",
			stateVer:    "2.86.0",
			versionKind: resource.VersionLatest,
			specVer:     "",
			wantUpdate:  true,
			wantReason:  "version changed",
		},
		{
			name:        "alias: same alias - no change",
			specVersion: "stable",
			stateVer:    "1.83.0",
			versionKind: resource.VersionAlias,
			specVer:     "stable",
			wantUpdate:  false,
		},
		{
			name:        "alias: changed to explicit",
			specVersion: "1.84.0",
			stateVer:    "1.83.0",
			versionKind: resource.VersionAlias,
			specVer:     "stable",
			wantUpdate:  true,
			wantReason:  "version changed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comparator := ToolComparator()

			res := &resource.Tool{
				BaseResource: resource.BaseResource{
					Metadata: resource.Metadata{Name: "test"},
				},
				ToolSpec: &resource.ToolSpec{
					InstallerRef: "aqua",
					Version:      tt.specVersion,
				},
			}

			state := &resource.ToolState{
				Version:     tt.stateVer,
				VersionKind: tt.versionKind,
				SpecVersion: tt.specVer,
			}
			if tt.tainted {
				state.TaintReason = "sync_update"
			}

			needsUpdate, reason := comparator(res, state)
			assert.Equal(t, tt.wantUpdate, needsUpdate)
			if tt.wantReason != "" {
				assert.Contains(t, reason, tt.wantReason)
			}
		})
	}
}

// --- RuntimeComparator tests with VersionKind ---

func TestRuntimeComparator_VersionKind(t *testing.T) {
	tests := []struct {
		name        string
		specVersion string
		stateVer    string
		versionKind resource.VersionKind
		specVer     string
		wantUpdate  bool
		wantReason  string
	}{
		{
			name:        "exact: same version - no change",
			specVersion: "1.25.6",
			stateVer:    "1.25.6",
			versionKind: resource.VersionExact,
			specVer:     "1.25.6",
			wantUpdate:  false,
		},
		{
			name:        "exact: version changed",
			specVersion: "1.25.7",
			stateVer:    "1.25.6",
			versionKind: resource.VersionExact,
			specVer:     "1.25.6",
			wantUpdate:  true,
			wantReason:  "version changed",
		},
		{
			name:        "alias: same alias - no change",
			specVersion: "stable",
			stateVer:    "1.83.0",
			versionKind: resource.VersionAlias,
			specVer:     "stable",
			wantUpdate:  false,
		},
		{
			name:        "alias: changed to explicit version",
			specVersion: "1.84.0",
			stateVer:    "1.83.0",
			versionKind: resource.VersionAlias,
			specVer:     "stable",
			wantUpdate:  true,
			wantReason:  "version changed",
		},
		{
			name:        "alias: changed to different alias",
			specVersion: "nightly",
			stateVer:    "1.83.0",
			versionKind: resource.VersionAlias,
			specVer:     "stable",
			wantUpdate:  true,
			wantReason:  "version changed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comparator := RuntimeComparator()

			res := &resource.Runtime{
				BaseResource: resource.BaseResource{
					Metadata: resource.Metadata{Name: "test"},
				},
				RuntimeSpec: &resource.RuntimeSpec{
					Version: tt.specVersion,
				},
			}

			state := &resource.RuntimeState{
				Version:     tt.stateVer,
				VersionKind: tt.versionKind,
				SpecVersion: tt.specVer,
			}

			needsUpdate, reason := comparator(res, state)
			assert.Equal(t, tt.wantUpdate, needsUpdate)
			if tt.wantReason != "" {
				assert.Contains(t, reason, tt.wantReason)
			}
		})
	}
}

// --- Property tests for specVersionChanged ---

// Property: VersionExact with same spec and state version → always false
func TestSpecVersionChanged_Property_ExactSameVersion(t *testing.T) {
	f := func(version string) bool {
		if version == "" {
			return true // skip: empty is VersionLatest, not VersionExact
		}
		return !specVersionChanged(version, resource.VersionExact, version, version)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// Property: VersionExact with different spec and state version → always true
func TestSpecVersionChanged_Property_ExactDifferentVersion(t *testing.T) {
	f := func(specVersion, stateVersion string) bool {
		if specVersion == stateVersion {
			return true // skip: same version
		}
		return specVersionChanged(specVersion, resource.VersionExact, stateVersion, stateVersion)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// Property: VersionLatest with empty spec → always false (updates driven by --sync)
func TestSpecVersionChanged_Property_LatestStaysEmpty(t *testing.T) {
	f := func(stateVersion string) bool {
		return !specVersionChanged("", resource.VersionLatest, stateVersion, "")
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// Property: VersionLatest with non-empty spec → always true (spec changed from latest to explicit)
func TestSpecVersionChanged_Property_LatestToExplicit(t *testing.T) {
	f := func(specVersion, stateVersion string) bool {
		if specVersion == "" {
			return true // skip: still latest
		}
		return specVersionChanged(specVersion, resource.VersionLatest, stateVersion, "")
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// Property: VersionAlias with same alias → always false
func TestSpecVersionChanged_Property_AliasSameAlias(t *testing.T) {
	f := func(alias, resolvedVersion string) bool {
		if alias == "" {
			return true // skip: empty alias is meaningless
		}
		return !specVersionChanged(alias, resource.VersionAlias, resolvedVersion, alias)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// Property: VersionAlias with different spec → always true
func TestSpecVersionChanged_Property_AliasDifferentSpec(t *testing.T) {
	f := func(specVersion, stateSpecVersion, stateVersion string) bool {
		if specVersion == stateSpecVersion {
			return true // skip: same alias
		}
		return specVersionChanged(specVersion, resource.VersionAlias, stateVersion, stateSpecVersion)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}
