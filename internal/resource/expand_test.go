package resource

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandSets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		resources []Resource
		wantNames []string // expected Tool names in output (sorted)
		wantErr   string   // expected error substring
	}{
		{
			name:      "no resources",
			resources: nil,
			wantNames: nil,
		},
		{
			name: "no toolsets - resources unchanged",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1"},
				},
				&Installer{
					BaseResource:  BaseResource{Metadata: Metadata{Name: "aqua"}},
					InstallerSpec: &InstallerSpec{Type: InstallTypeDownload},
				},
			},
			wantNames: []string{"rg"},
		},
		{
			name: "single toolset with 2 tools",
			resources: []Resource{
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd":  {Version: "9.0.0"},
							"bat": {Version: "0.24.0"},
						},
					},
				},
			},
			wantNames: []string{"bat", "fd"},
		},
		{
			name: "toolset with disabled tool",
			resources: []Resource{
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd":  {Version: "9.0.0"},
							"bat": {Version: "0.24.0", Enabled: new(false)},
						},
					},
				},
			},
			wantNames: []string{"fd"},
		},
		{
			name: "toolset with all disabled - zero tools",
			resources: []Resource{
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd":  {Version: "9.0.0", Enabled: new(false)},
							"bat": {Version: "0.24.0", Enabled: new(false)},
						},
					},
				},
			},
			wantNames: nil,
		},
		{
			name: "toolset with installerRef - tools inherit",
			resources: []Resource{
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd": {Version: "9.0.0"},
						},
					},
				},
			},
			wantNames: []string{"fd"},
		},
		{
			name: "toolset with runtimeRef - tools inherit",
			resources: []Resource{
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "go-tools"}},
					ToolSetSpec: &ToolSetSpec{
						RuntimeRef: "go",
						Tools: map[string]ToolItem{
							"gopls": {Package: &Package{Name: "golang.org/x/tools/gopls"}, Version: "v0.21.0"},
						},
					},
				},
			},
			wantNames: []string{"gopls"},
		},
		{
			name: "name conflict - toolset vs standalone tool",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "fd"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "9.0.0"},
				},
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd": {Version: "10.0.0"},
						},
					},
				},
			},
			wantErr: "name conflict",
		},
		{
			name: "name conflict - same name in two toolsets",
			resources: []Resource{
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "set-a"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd": {Version: "9.0.0"},
						},
					},
				},
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "set-b"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd": {Version: "10.0.0"},
						},
					},
				},
			},
			wantErr: "name conflict",
		},
		{
			name: "mixed standalone tools and toolset",
			resources: []Resource{
				&Installer{
					BaseResource:  BaseResource{Metadata: Metadata{Name: "aqua"}},
					InstallerSpec: &InstallerSpec{Type: InstallTypeDownload},
				},
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1"},
				},
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd":  {Version: "9.0.0"},
							"bat": {Version: "0.24.0"},
						},
					},
				},
			},
			wantNames: []string{"bat", "fd", "rg"},
		},
		{
			name: "standalone tool with enabled false",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1", Enabled: new(false)},
				},
			},
			wantNames: nil,
		},
		{
			name: "standalone tool with enabled true",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1", Enabled: new(true)},
				},
			},
			wantNames: []string{"rg"},
		},
		{
			name: "standalone tool with enabled nil",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1"},
				},
			},
			wantNames: []string{"rg"},
		},
		{
			name: "mixed enabled and disabled standalone tools",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1", Enabled: new(false)},
				},
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "bat"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "0.24.0", Enabled: new(true)},
				},
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "fd"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "9.0.0"},
				},
			},
			wantNames: []string{"bat", "fd"},
		},
		{
			name: "disabled standalone tool does not conflict with toolset expansion",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "fd"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "9.0.0", Enabled: new(false)},
				},
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd": {Version: "10.0.0"},
						},
					},
				},
			},
			wantNames: []string{"fd"},
		},
		{
			name: "disabled standalone tool does not conflict with another standalone tool",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.0.0", Enabled: new(false)},
				},
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1"},
				},
			},
			wantNames: []string{"rg"},
		},
		{
			name: "tool with nil spec is enabled by default",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
				},
			},
			wantNames: []string{"rg"},
		},
		{
			name: "toolitem with package field",
			resources: []Resource{
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "go-tools"}},
					ToolSetSpec: &ToolSetSpec{
						RuntimeRef: "go",
						Tools: map[string]ToolItem{
							"gopls":     {Package: &Package{Name: "golang.org/x/tools/gopls"}, Version: "v0.21.0"},
							"goimports": {Package: &Package{Name: "golang.org/x/tools/cmd/goimports"}, Version: "v0.31.0"},
						},
					},
				},
			},
			wantNames: []string{"goimports", "gopls"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := ExpandSets(tt.resources)

			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)

			// Collect tool names (sorted for determinism)
			var toolNames []string
			for _, r := range got {
				if r.Kind() == KindTool {
					toolNames = append(toolNames, r.Name())
				}
			}
			// Sort for deterministic comparison
			sort.Strings(toolNames)
			sort.Strings(tt.wantNames)
			assert.Equal(t, tt.wantNames, toolNames)

			// Verify no Expandable resource remains in output
			for _, r := range got {
				_, isExpandable := r.(Expandable)
				assert.False(t, isExpandable, "Expandable resource %s/%s should not remain after expansion", r.Kind(), r.Name())
			}
		})
	}
}

func TestExpandSets_InheritedFields(t *testing.T) {
	t.Parallel()
	t.Run("installerRef inherited", func(t *testing.T) {
		t.Parallel()
		resources := []Resource{
			&ToolSet{
				BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
				ToolSetSpec: &ToolSetSpec{
					InstallerRef: "aqua",
					Tools: map[string]ToolItem{
						"fd": {Version: "9.0.0"},
					},
				},
			},
		}
		got, err := ExpandSets(resources)
		require.NoError(t, err)
		require.Len(t, got, 1)

		tool := got[0].(*Tool)
		assert.Equal(t, "fd", tool.Name())
		assert.Equal(t, "aqua", tool.ToolSpec.InstallerRef)
		assert.Equal(t, "9.0.0", tool.ToolSpec.Version)
		assert.Empty(t, tool.ToolSpec.RuntimeRef)
	})

	t.Run("runtimeRef inherited", func(t *testing.T) {
		t.Parallel()
		resources := []Resource{
			&ToolSet{
				BaseResource: BaseResource{Metadata: Metadata{Name: "go-tools"}},
				ToolSetSpec: &ToolSetSpec{
					RuntimeRef: "go",
					Tools: map[string]ToolItem{
						"gopls": {Package: &Package{Name: "golang.org/x/tools/gopls"}, Version: "v0.21.0"},
					},
				},
			},
		}
		got, err := ExpandSets(resources)
		require.NoError(t, err)
		require.Len(t, got, 1)

		tool := got[0].(*Tool)
		assert.Equal(t, "gopls", tool.Name())
		assert.Equal(t, "go", tool.ToolSpec.RuntimeRef)
		assert.Equal(t, "v0.21.0", tool.ToolSpec.Version)
		assert.Equal(t, "golang.org/x/tools/gopls", tool.ToolSpec.Package.Name)
		assert.Empty(t, tool.ToolSpec.InstallerRef)
	})

	t.Run("source inherited", func(t *testing.T) {
		t.Parallel()
		src := &DownloadSource{URL: "https://example.com/fd.tar.gz"}
		resources := []Resource{
			&ToolSet{
				BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
				ToolSetSpec: &ToolSetSpec{
					InstallerRef: "aqua",
					Tools: map[string]ToolItem{
						"fd": {Version: "9.0.0", Source: src},
					},
				},
			},
		}
		got, err := ExpandSets(resources)
		require.NoError(t, err)
		require.Len(t, got, 1)

		tool := got[0].(*Tool)
		assert.Equal(t, src, tool.ToolSpec.Source)
	})
}

func TestCollectDisabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resources []Resource
		wantNames []string // expected disabled Tool names (sorted)
	}{
		{
			name:      "no resources",
			resources: nil,
			wantNames: nil,
		},
		{
			name: "standalone disabled Tool is collected",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1", Enabled: new(false)},
				},
			},
			wantNames: []string{"rg"},
		},
		{
			name: "standalone enabled Tool is not collected",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1", Enabled: new(true)},
				},
			},
			wantNames: nil,
		},
		{
			name: "standalone Tool with nil enabled is not collected",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1"},
				},
			},
			wantNames: nil,
		},
		{
			name: "ToolSet disabled items are collected as Tools",
			resources: []Resource{
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd":  {Version: "9.0.0"},
							"bat": {Version: "0.24.0", Enabled: new(false)},
						},
					},
				},
			},
			wantNames: []string{"bat"},
		},
		{
			name: "ToolSet enabled items are not collected",
			resources: []Resource{
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd":  {Version: "9.0.0"},
							"bat": {Version: "0.24.0"},
						},
					},
				},
			},
			wantNames: nil,
		},
		{
			name: "mixed standalone and ToolSet disabled",
			resources: []Resource{
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1", Enabled: new(false)},
				},
				&Tool{
					BaseResource: BaseResource{Metadata: Metadata{Name: "jq"}},
					ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "1.7.1"},
				},
				&ToolSet{
					BaseResource: BaseResource{Metadata: Metadata{Name: "cli-tools"}},
					ToolSetSpec: &ToolSetSpec{
						InstallerRef: "aqua",
						Tools: map[string]ToolItem{
							"fd":  {Version: "9.0.0", Enabled: new(false)},
							"bat": {Version: "0.24.0"},
						},
					},
				},
			},
			wantNames: []string{"fd", "rg"},
		},
		{
			name: "non-enableable resources are not collected",
			resources: []Resource{
				&Installer{
					BaseResource:  BaseResource{Metadata: Metadata{Name: "aqua"}},
					InstallerSpec: &InstallerSpec{Type: InstallTypeDownload},
				},
			},
			wantNames: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CollectDisabled(tt.resources)

			var names []string
			for _, r := range got {
				names = append(names, r.Name())
			}
			sort.Strings(tt.wantNames)
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

func TestCollectDisabled_InheritedFields(t *testing.T) {
	t.Parallel()

	resources := []Resource{
		&ToolSet{
			BaseResource: BaseResource{Metadata: Metadata{Name: "go-tools"}},
			ToolSetSpec: &ToolSetSpec{
				RuntimeRef:    "go",
				RepositoryRef: "custom-repo",
				Tools: map[string]ToolItem{
					"gopls": {Package: &Package{Name: "golang.org/x/tools/gopls"}, Version: "v0.21.0", Enabled: new(false)},
				},
			},
		},
	}

	got := CollectDisabled(resources)
	require.Len(t, got, 1)

	tool := got[0].(*Tool)
	assert.Equal(t, "gopls", tool.Name())
	assert.Equal(t, "go", tool.ToolSpec.RuntimeRef)
	assert.Equal(t, "custom-repo", tool.ToolSpec.RepositoryRef)
	assert.Equal(t, "v0.21.0", tool.ToolSpec.Version)
	assert.Equal(t, "golang.org/x/tools/gopls", tool.ToolSpec.Package.Name)
}

func TestToolImplementsEnableable(t *testing.T) {
	var _ Enableable = (*Tool)(nil)
}

func TestExpandSets_SystemPackage_Basic(t *testing.T) {
	t.Parallel()
	sp := &SystemPackage{
		BaseResource: BaseResource{Metadata: Metadata{Name: "git"}},
		SystemPackageSpec: &SystemPackageSpec{
			InstallerRef: "apt",
			Package:      "git",
		},
	}

	got, err := ExpandSets([]Resource{sp})
	require.NoError(t, err)
	require.Len(t, got, 1)

	set, ok := got[0].(*SystemPackageSet)
	require.True(t, ok, "expected *SystemPackageSet, got %T", got[0])
	assert.Equal(t, "git", set.Name())
	assert.Equal(t, "apt", set.SystemPackageSetSpec.InstallerRef)
	assert.Equal(t, []string{"git"}, set.SystemPackageSetSpec.Packages)

	// SystemPackage must not remain in the output.
	for _, r := range got {
		_, ok := r.(*SystemPackage)
		assert.False(t, ok, "SystemPackage should be replaced by expansion")
	}
}

func TestExpandSets_SystemPackage_NameConflict(t *testing.T) {
	t.Parallel()
	resources := []Resource{
		&SystemPackage{
			BaseResource:      BaseResource{Metadata: Metadata{Name: "curl"}},
			SystemPackageSpec: &SystemPackageSpec{InstallerRef: "apt", Package: "curl"},
		},
		&SystemPackage{
			BaseResource:      BaseResource{Metadata: Metadata{Name: "curl"}},
			SystemPackageSpec: &SystemPackageSpec{InstallerRef: "apt", Package: "curl-minimal"},
		},
	}

	_, err := ExpandSets(resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "name conflict")
}

// TestExpandSets_SystemPackage_SamePackageDifferentName validates the
// Name/Package decoupling: two SystemPackages installing the same OS package
// but with distinct resource identities expand without conflict.
func TestExpandSets_SystemPackage_SamePackageDifferentName(t *testing.T) {
	t.Parallel()
	resources := []Resource{
		&SystemPackage{
			BaseResource:      BaseResource{Metadata: Metadata{Name: "curl-main"}},
			SystemPackageSpec: &SystemPackageSpec{InstallerRef: "apt", Package: "curl"},
		},
		&SystemPackage{
			BaseResource:      BaseResource{Metadata: Metadata{Name: "curl-backup"}},
			SystemPackageSpec: &SystemPackageSpec{InstallerRef: "apt", Package: "curl"},
		},
	}

	got, err := ExpandSets(resources)
	require.NoError(t, err)
	require.Len(t, got, 2)

	names := []string{got[0].Name(), got[1].Name()}
	assert.ElementsMatch(t, []string{"curl-main", "curl-backup"}, names)

	for _, r := range got {
		set, ok := r.(*SystemPackageSet)
		require.True(t, ok)
		assert.Equal(t, []string{"curl"}, set.SystemPackageSetSpec.Packages)
	}
}

func TestIsPrivileged(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		res  Resource
		want bool
	}{
		{
			name: "privileged commands-based tool",
			res:  &Tool{ToolSpec: &ToolSpec{Privileged: true, Commands: &ToolCommandSet{CommandSet: CommandSet{Install: []string{"echo ok"}}}}},
			want: true,
		},
		{
			name: "privileged tool with no install method",
			res:  &Tool{ToolSpec: &ToolSpec{Privileged: true}},
			want: false,
		},
		{
			name: "non-privileged tool",
			res:  &Tool{ToolSpec: &ToolSpec{Privileged: false}},
			want: false,
		},
		{
			name: "tool with nil spec",
			res:  &Tool{},
			want: false,
		},
		{
			name: "runtime (not a tool)",
			res:  &Runtime{RuntimeSpec: &RuntimeSpec{Version: "1.22.0"}},
			want: false,
		},
		{
			// Smoke test for non-Commands privileged → IsPrivileged delegation
			// to Tool.IsPrivileged. Full pattern matrix lives in
			// TestTool_IsPrivileged (tool_test.go).
			name: "privileged download tool delegates through to Tool.IsPrivileged",
			res: &Tool{ToolSpec: &ToolSpec{
				InstallerRef: "aqua",
				Version:      "1.0.0",
				Source:       &DownloadSource{URL: "https://example.com/tool.tar.gz"},
				Privileged:   true,
			}},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsPrivileged(tt.res))
		})
	}
}

func TestFilterPrivileged(t *testing.T) {
	t.Parallel()
	resources := []Resource{
		&Tool{
			BaseResource: BaseResource{Metadata: Metadata{Name: "homebrew"}},
			ToolSpec:     &ToolSpec{Privileged: true, Commands: &ToolCommandSet{CommandSet: CommandSet{Install: []string{"echo ok"}}}},
		},
		&Tool{
			BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}},
			ToolSpec:     &ToolSpec{InstallerRef: "aqua", Version: "14.1.1"},
		},
		&Runtime{
			BaseResource: BaseResource{Metadata: Metadata{Name: "go"}},
			RuntimeSpec:  &RuntimeSpec{Version: "1.22.0"},
		},
	}

	normal, privileged := FilterPrivileged(resources)
	assert.Len(t, normal, 2)
	assert.Len(t, privileged, 1)
	assert.Equal(t, "homebrew", privileged[0].Name())
	assert.Equal(t, "rg", normal[0].Name())
	assert.Equal(t, "go", normal[1].Name())
}

func TestFilterSystemKinds(t *testing.T) {
	t.Parallel()

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		user, system := FilterSystemKinds(nil)
		assert.Empty(t, user)
		assert.Empty(t, system)
	})

	t.Run("user only", func(t *testing.T) {
		t.Parallel()
		resources := []Resource{
			&Tool{BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}}},
			&Runtime{BaseResource: BaseResource{Metadata: Metadata{Name: "go"}}},
		}
		user, system := FilterSystemKinds(resources)
		assert.Len(t, user, 2)
		assert.Empty(t, system)
	})

	t.Run("system only", func(t *testing.T) {
		t.Parallel()
		resources := []Resource{
			&SystemInstaller{BaseResource: BaseResource{Metadata: Metadata{Name: "apt"}}},
			&SystemPackageRepository{BaseResource: BaseResource{Metadata: Metadata{Name: "docker-repo"}}},
			&SystemPackage{BaseResource: BaseResource{Metadata: Metadata{Name: "git"}}},
			&SystemPackageSet{BaseResource: BaseResource{Metadata: Metadata{Name: "dev-tools"}}},
		}
		user, system := FilterSystemKinds(resources)
		assert.Empty(t, user)
		assert.Len(t, system, 4)
	})

	t.Run("mixed", func(t *testing.T) {
		t.Parallel()
		resources := []Resource{
			&Tool{BaseResource: BaseResource{Metadata: Metadata{Name: "rg"}}},
			&SystemInstaller{BaseResource: BaseResource{Metadata: Metadata{Name: "apt"}}},
			&Runtime{BaseResource: BaseResource{Metadata: Metadata{Name: "go"}}},
			&SystemPackage{BaseResource: BaseResource{Metadata: Metadata{Name: "git"}}},
			&SystemPackageSet{BaseResource: BaseResource{Metadata: Metadata{Name: "dev-tools"}}},
		}
		user, system := FilterSystemKinds(resources)
		assert.Len(t, user, 2)
		assert.Equal(t, "rg", user[0].Name())
		assert.Equal(t, "go", user[1].Name())
		assert.Len(t, system, 3)
		assert.Equal(t, "apt", system[0].Name())
		assert.Equal(t, "git", system[1].Name())
		assert.Equal(t, "dev-tools", system[2].Name())
	})
}

func TestHasPrivileged(t *testing.T) {
	t.Parallel()

	t.Run("has privileged", func(t *testing.T) {
		t.Parallel()
		resources := []Resource{
			&Tool{ToolSpec: &ToolSpec{Privileged: true, Commands: &ToolCommandSet{CommandSet: CommandSet{Install: []string{"echo ok"}}}}},
			&Tool{ToolSpec: &ToolSpec{}},
		}
		assert.True(t, HasPrivileged(resources))
	})

	t.Run("no privileged", func(t *testing.T) {
		t.Parallel()
		resources := []Resource{
			&Tool{ToolSpec: &ToolSpec{}},
			&Runtime{RuntimeSpec: &RuntimeSpec{Version: "1.22.0"}},
		}
		assert.False(t, HasPrivileged(resources))
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		assert.False(t, HasPrivileged(nil))
	})
}
