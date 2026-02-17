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
