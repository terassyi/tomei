package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/graph"
	"github.com/terassyi/tomei/internal/path"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
)

// stubResolver is a minimal graph.Resolver for renderSectionedTree tests.
type stubResolver struct {
	nodes []*graph.Node
	edges []graph.Edge
}

func (s *stubResolver) AddResource(resource.Resource) {}
func (s *stubResolver) Resolve() ([]graph.Layer, error) {
	return nil, nil
}
func (s *stubResolver) Validate() error         { return nil }
func (s *stubResolver) NodeCount() int          { return len(s.nodes) }
func (s *stubResolver) EdgeCount() int          { return len(s.edges) }
func (s *stubResolver) GetEdges() []graph.Edge  { return s.edges }
func (s *stubResolver) GetNodes() []*graph.Node { return s.nodes }

func makeNode(kind resource.Kind, name string) *graph.Node {
	return &graph.Node{ID: graph.NewNodeID(kind, name), Kind: kind, Name: name}
}

func TestCollectSkipInfos(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		info      map[graph.NodeID]graph.ResourceInfo
		wantNames []string
	}{
		{
			name:      "empty map",
			info:      map[graph.NodeID]graph.ResourceInfo{},
			wantNames: nil,
		},
		{
			name: "no skip actions",
			info: map[graph.NodeID]graph.ResourceInfo{
				graph.NewNodeID(resource.KindTool, "rg"): {Kind: resource.KindTool, Name: "rg", Action: resource.ActionInstall},
			},
			wantNames: nil,
		},
		{
			name: "collects skip actions sorted by kind then name",
			info: map[graph.NodeID]graph.ResourceInfo{
				graph.NewNodeID(resource.KindTool, "rg"):    {Kind: resource.KindTool, Name: "rg", Action: resource.ActionInstall},
				graph.NewNodeID(resource.KindTool, "bat"):   {Kind: resource.KindTool, Name: "bat", Action: resource.ActionSkip},
				graph.NewNodeID(resource.KindTool, "fd"):    {Kind: resource.KindTool, Name: "fd", Action: resource.ActionSkip},
				graph.NewNodeID(resource.KindRuntime, "go"): {Kind: resource.KindRuntime, Name: "go", Action: resource.ActionNone},
			},
			wantNames: []string{"bat", "fd"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := collectSkipInfos(tt.info)

			var names []string
			for _, info := range got {
				names = append(names, info.Name)
			}
			assert.Equal(t, tt.wantNames, names)
		})
	}
}

func TestAddDisabledResourceInfo(t *testing.T) {
	t.Parallel()

	t.Run("disabled tool not in state gets ActionSkip", func(t *testing.T) {
		t.Parallel()
		info := make(map[graph.NodeID]graph.ResourceInfo)
		disabled := []resource.Resource{
			&resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "bat"}},
				ToolSpec:     &resource.ToolSpec{InstallerRef: "aqua", Version: "0.24.0", Enabled: new(false)},
			},
		}

		addDisabledResourceInfo(info, disabled)

		nodeID := graph.NewNodeID(resource.KindTool, "bat")
		require.Contains(t, info, nodeID)
		assert.Equal(t, resource.ActionSkip, info[nodeID].Action)
		assert.Equal(t, "0.24.0", info[nodeID].Version)
		assert.Equal(t, resource.KindTool, info[nodeID].Kind)
		assert.Equal(t, "bat", info[nodeID].Name)
	})

	t.Run("disabled tool with existing ActionRemove is not overwritten", func(t *testing.T) {
		t.Parallel()
		nodeID := graph.NewNodeID(resource.KindTool, "bat")
		info := map[graph.NodeID]graph.ResourceInfo{
			nodeID: {
				Kind:    resource.KindTool,
				Name:    "bat",
				Version: "0.23.0",
				Action:  resource.ActionRemove,
			},
		}
		disabled := []resource.Resource{
			&resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "bat"}},
				ToolSpec:     &resource.ToolSpec{InstallerRef: "aqua", Version: "0.24.0", Enabled: new(false)},
			},
		}

		addDisabledResourceInfo(info, disabled)

		assert.Equal(t, resource.ActionRemove, info[nodeID].Action)
		assert.Equal(t, "0.23.0", info[nodeID].Version)
	})

	t.Run("empty disabled list does not modify info", func(t *testing.T) {
		t.Parallel()
		info := map[graph.NodeID]graph.ResourceInfo{
			graph.NewNodeID(resource.KindTool, "rg"): {
				Kind:   resource.KindTool,
				Name:   "rg",
				Action: resource.ActionNone,
			},
		}

		addDisabledResourceInfo(info, nil)

		assert.Len(t, info, 1)
	})

	t.Run("disabled tool with nil spec has empty version", func(t *testing.T) {
		t.Parallel()
		info := make(map[graph.NodeID]graph.ResourceInfo)
		disabled := []resource.Resource{
			&resource.Tool{
				BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "bat"}},
			},
		}

		addDisabledResourceInfo(info, disabled)

		nodeID := graph.NewNodeID(resource.KindTool, "bat")
		require.Contains(t, info, nodeID)
		assert.Equal(t, resource.ActionSkip, info[nodeID].Action)
		assert.Empty(t, info[nodeID].Version)
	})
}

// systemRepoResource returns a minimal SystemPackageRepository resource
// suitable for first-install reconcile tests (state-absent → ActionInstall).
func systemRepoResource(name string) *resource.SystemPackageRepository {
	return &resource.SystemPackageRepository{
		BaseResource: resource.BaseResource{
			APIVersion:   "tomei.terassyi.net/v1beta1",
			ResourceKind: resource.KindSystemPackageRepository,
			Metadata:     resource.Metadata{Name: name},
		},
		SystemPackageRepositorySpec: &resource.SystemPackageRepositorySpec{
			InstallerRef: resource.InstallerRefApt,
			Apt: &resource.AptSource{
				URL:        "https://download.docker.com/linux/ubuntu",
				KeyURL:     "https://download.docker.com/linux/ubuntu/gpg",
				KeyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				Suite:      "jammy",
				Components: []string{"stable"},
			},
		},
	}
}

func systemPackageSetResource(name string) *resource.SystemPackageSet {
	return &resource.SystemPackageSet{
		BaseResource: resource.BaseResource{
			APIVersion:   "tomei.terassyi.net/v1beta1",
			ResourceKind: resource.KindSystemPackageSet,
			Metadata:     resource.Metadata{Name: name},
		},
		SystemPackageSetSpec: &resource.SystemPackageSetSpec{
			InstallerRef: resource.InstallerRefApt,
			Packages:     []string{"curl"},
		},
	}
}

func systemInstallerResource(name string) *resource.SystemInstaller {
	// markAllSystemAsInstall only reads Kind() and Name(), so the spec
	// content is unused here. Leave it nil to keep the helper minimal.
	return &resource.SystemInstaller{
		BaseResource: resource.BaseResource{
			APIVersion:   "tomei.terassyi.net/v1beta1",
			ResourceKind: resource.KindSystemInstaller,
			Metadata:     resource.Metadata{Name: name},
		},
	}
}

func TestMarkAllSystemAsInstall(t *testing.T) {
	t.Parallel()

	t.Run("SystemInstaller gets ActionInstall", func(t *testing.T) {
		t.Parallel()
		info := make(map[graph.NodeID]graph.ResourceInfo)
		markAllSystemAsInstall(info, []resource.Resource{systemInstallerResource("apt")})

		nodeID := graph.NewNodeID(resource.KindSystemInstaller, "apt")
		require.Contains(t, info, nodeID)
		assert.Equal(t, resource.ActionInstall, info[nodeID].Action)
	})

	t.Run("SystemPackageRepository gets ActionInstall (#196/#198 wiring)", func(t *testing.T) {
		t.Parallel()
		info := make(map[graph.NodeID]graph.ResourceInfo)
		markAllSystemAsInstall(info, []resource.Resource{systemRepoResource("docker")})

		nodeID := graph.NewNodeID(resource.KindSystemPackageRepository, "docker")
		require.Contains(t, info, nodeID)
		assert.Equal(t, resource.ActionInstall, info[nodeID].Action,
			"SystemPackageRepository must NOT be downgraded to Skip after #196 wired the concrete installer")
	})

	t.Run("SystemPackageSet gets ActionInstall (post-#198)", func(t *testing.T) {
		t.Parallel()
		info := make(map[graph.NodeID]graph.ResourceInfo)
		markAllSystemAsInstall(info, []resource.Resource{systemPackageSetResource("dev-tools")})

		nodeID := graph.NewNodeID(resource.KindSystemPackageSet, "dev-tools")
		require.Contains(t, info, nodeID)
		assert.Equal(t, resource.ActionInstall, info[nodeID].Action,
			"SystemPackageSet must NOT be downgraded to Skip after #198 wired the concrete installer")
	})
}

func TestMarkPrivilegedAsSkip(t *testing.T) {
	t.Parallel()

	commandsPrivTool := &resource.Tool{
		BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "cmds-priv"}},
		ToolSpec: &resource.ToolSpec{
			Commands:   &resource.ToolCommandSet{CommandSet: resource.CommandSet{Install: []string{"echo x"}}},
			Privileged: true,
		},
	}
	downloadPrivTool := &resource.Tool{
		BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "dl-priv"}},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Version:      "1.0.0",
			Source:       &resource.DownloadSource{URL: "https://example.com/x.tar.gz"},
			Privileged:   true,
		},
	}
	registryPrivTool := &resource.Tool{
		BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "reg-priv"}},
		ToolSpec: &resource.ToolSpec{
			InstallerRef: "aqua",
			Package:      &resource.Package{Owner: "owner", Repo: "reg-priv"},
			Privileged:   true,
		},
	}
	plainTool := &resource.Tool{
		BaseResource: resource.BaseResource{Metadata: resource.Metadata{Name: "plain"}},
		ToolSpec: &resource.ToolSpec{
			Commands: &resource.ToolCommandSet{CommandSet: resource.CommandSet{Install: []string{"echo y"}}},
		},
	}

	tests := []struct {
		name   string
		res    resource.Resource
		system bool
		// Initial action before the helper runs (simulating the rest of
		// buildResourceInfo). The helper must only flip to Skip when both
		// privileged AND !system; otherwise leave alone.
		initial    resource.ActionType
		wantAction resource.ActionType
	}{
		{name: "commands-privileged without --system → skip", res: commandsPrivTool, system: false, initial: resource.ActionInstall, wantAction: resource.ActionSkip},
		{name: "download-privileged without --system → skip (auto-extends via SUB4)", res: downloadPrivTool, system: false, initial: resource.ActionInstall, wantAction: resource.ActionSkip},
		{name: "registry-privileged without --system → skip (auto-extends via SUB4)", res: registryPrivTool, system: false, initial: resource.ActionInstall, wantAction: resource.ActionSkip},
		{name: "privileged with --system stays put", res: commandsPrivTool, system: true, initial: resource.ActionInstall, wantAction: resource.ActionInstall},
		{name: "non-privileged tool unaffected", res: plainTool, system: false, initial: resource.ActionInstall, wantAction: resource.ActionInstall},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			info := graph.ResourceInfo{
				Kind:   tt.res.Kind(),
				Name:   tt.res.Name(),
				Action: tt.initial,
			}
			markPrivilegedAsSkip(&info, tt.res, tt.system)
			assert.Equal(t, tt.wantAction, info.Action)
		})
	}
}

func TestAddSystemResourceInfo(t *testing.T) {
	t.Parallel()

	// Setup: TempDir-backed Paths so LoadReadOnly returns an empty SystemState
	// (state.json absent → zero-value state per Store.readState). With empty
	// state, the reconciler emits ActionInstall for both kinds; post-#198 the
	// skip-downgrade loop is removed entirely, so every system kind passes
	// through with the reconciler-determined action.
	setup := func(t *testing.T) *path.Paths {
		t.Helper()
		systemDir := t.TempDir()
		paths, err := path.New(path.WithSystemDataDir(systemDir))
		require.NoError(t, err)
		return paths
	}

	t.Run("SystemPackageRepository gets ActionInstall (skip-downgrade removed)", func(t *testing.T) {
		t.Parallel()
		paths := setup(t)
		info := make(map[graph.NodeID]graph.ResourceInfo)
		addSystemResourceInfo(info, []resource.Resource{systemRepoResource("docker")}, paths)

		nodeID := graph.NewNodeID(resource.KindSystemPackageRepository, "docker")
		require.Contains(t, info, nodeID)
		assert.Equal(t, resource.ActionInstall, info[nodeID].Action,
			"reconciler emits ActionInstall; the addSystemResourceInfo skip-downgrade loop must NOT convert it back to Skip after #196")
	})

	t.Run("SystemPackageSet gets ActionInstall (post-#198)", func(t *testing.T) {
		t.Parallel()
		paths := setup(t)
		info := make(map[graph.NodeID]graph.ResourceInfo)
		addSystemResourceInfo(info, []resource.Resource{systemPackageSetResource("dev-tools")}, paths)

		nodeID := graph.NewNodeID(resource.KindSystemPackageSet, "dev-tools")
		require.Contains(t, info, nodeID)
		assert.Equal(t, resource.ActionInstall, info[nodeID].Action,
			"reconciler emits ActionInstall; the addSystemResourceInfo skip-downgrade loop was removed in #198 so SystemPackageSet must pass through")
	})

	t.Run("absent system data dir falls back to markAllSystemAsInstall", func(t *testing.T) {
		t.Parallel()
		// systemDir is intentionally a path that does not exist: plan must
		// not create it (read-only contract) and must instead fall through
		// to the first-run fallback.
		paths, err := path.New(path.WithSystemDataDir(filepath.Join(t.TempDir(), "absent")))
		require.NoError(t, err)

		info := make(map[graph.NodeID]graph.ResourceInfo)
		addSystemResourceInfo(info, []resource.Resource{
			systemRepoResource("hashicorp"),
			systemPackageSetResource("net-tools"),
		}, paths)

		repoID := graph.NewNodeID(resource.KindSystemPackageRepository, "hashicorp")
		require.Contains(t, info, repoID)
		assert.Equal(t, resource.ActionInstall, info[repoID].Action,
			"first-run fallback must put SystemPackageRepository on the Install path")
		pkgID := graph.NewNodeID(resource.KindSystemPackageSet, "net-tools")
		require.Contains(t, info, pkgID)
		assert.Equal(t, resource.ActionInstall, info[pkgID].Action,
			"first-run fallback must put SystemPackageSet on the Install path post-#198")
	})

	t.Run("corrupt state.json falls back to markAllSystemAsInstall", func(t *testing.T) {
		t.Parallel()
		systemDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(systemDir, "state.json"), []byte("{"), 0644))
		paths, err := path.New(path.WithSystemDataDir(systemDir))
		require.NoError(t, err)

		info := make(map[graph.NodeID]graph.ResourceInfo)
		addSystemResourceInfo(info, []resource.Resource{systemRepoResource("kubernetes")}, paths)

		nodeID := graph.NewNodeID(resource.KindSystemPackageRepository, "kubernetes")
		require.Contains(t, info, nodeID)
		assert.Equal(t, resource.ActionInstall, info[nodeID].Action,
			"corrupt state load must fall back to install path, not panic")
	})

	t.Run("SystemPackageRepository in state but absent from manifest gets ActionRemove", func(t *testing.T) {
		t.Parallel()
		// Pre-seed state with a SystemPackageRepository entry; pass an
		// empty manifest. The reconciler emits ActionRemove; with the
		// skip-downgrade loop removed in #198, the action must pass through.
		systemDir := t.TempDir()
		st := state.NewSystemState()
		st.SystemPackageRepositories["docker"] = &resource.SystemPackageRepositoryState{
			InstallerRef: resource.InstallerRefApt,
			Apt: &resource.AptSource{
				URL:        "https://download.docker.com/linux/ubuntu",
				KeyURL:     "https://download.docker.com/linux/ubuntu/gpg",
				KeyHash:    "sha256:0000000000000000000000000000000000000000000000000000000000000000",
				Suite:      "jammy",
				Components: []string{"stable"},
			},
			InstalledFiles: []string{"/usr/share/keyrings/docker.gpg", "/etc/apt/sources.list.d/docker.list"},
			UpdatedAt:      time.Now(),
		}
		data, err := json.Marshal(st)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(systemDir, "state.json"), data, 0644))

		paths, err := path.New(path.WithSystemDataDir(systemDir))
		require.NoError(t, err)

		info := make(map[graph.NodeID]graph.ResourceInfo)
		addSystemResourceInfo(info, nil, paths) // empty manifest → all state entries should be Remove

		nodeID := graph.NewNodeID(resource.KindSystemPackageRepository, "docker")
		require.Contains(t, info, nodeID)
		assert.Equal(t, resource.ActionRemove, info[nodeID].Action,
			"reconciler emits ActionRemove; the skip-downgrade loop was removed in #198 so Remove must pass through")
	})
}

func TestRenderSectionedTree(t *testing.T) {
	t.Parallel()

	// Common nodes used across cases.
	nAqua := makeNode(resource.KindInstaller, "aqua")
	nBat := makeNode(resource.KindTool, "bat")
	nLazygit := makeNode(resource.KindTool, "lazygit")
	nApt := makeNode(resource.KindSystemInstaller, "apt")
	nPkgs := makeNode(resource.KindSystemPackageSet, "build-deps")

	mixed := func() *planResult {
		return &planResult{
			resolver: &stubResolver{
				nodes: []*graph.Node{nAqua, nBat, nLazygit, nApt, nPkgs},
				edges: []graph.Edge{
					{From: nBat.ID, To: nAqua.ID},
					{From: nLazygit.ID, To: nAqua.ID},
					{From: nPkgs.ID, To: nApt.ID},
				},
			},
			resourceInfo: map[graph.NodeID]graph.ResourceInfo{
				// Installer/aqua is intentionally NOT in resourceInfo to also
				// exercise the "node in resolver but not in info map" path
				// (builtin Installers added by AppendBuiltinInstallers). It must
				// route to "Tools:" via the zero-value ResourceInfo.
				nBat.ID:     {Kind: resource.KindTool, Name: "bat", Version: "0.24.0", Action: resource.ActionInstall},
				nLazygit.ID: {Kind: resource.KindTool, Name: "lazygit", Version: "v0.62.1", Action: resource.ActionSkip, Privileged: true},
				nApt.ID:     {Kind: resource.KindSystemInstaller, Name: "apt", Action: resource.ActionInstall},
				nPkgs.ID:    {Kind: resource.KindSystemPackageSet, Name: "build-deps", Action: resource.ActionInstall},
			},
		}
	}

	t.Run("three sections appear in order with mixed manifest", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		renderSectionedTree(graph.NewTreePrinter(&buf, true), &buf, mixed())
		out := buf.String()
		toolsIdx := strings.Index(out, "Tools:\n")
		privIdx := strings.Index(out, "Privileged Tools (--system):\n")
		sysIdx := strings.Index(out, "System:\n")
		require.NotEqual(t, -1, toolsIdx)
		require.NotEqual(t, -1, privIdx)
		require.NotEqual(t, -1, sysIdx)
		assert.Less(t, toolsIdx, privIdx)
		assert.Less(t, privIdx, sysIdx)
		// privileged Tool kept in its own section even with ActionSkip
		assert.Contains(t, out, "Tool/lazygit (v0.62.1) [⊘ skip]")
		// the builtin-Installer-style entry (no resourceInfo) is rendered
		// under "Tools:" as the root with Tool/bat as its child.
		assert.Contains(t, out, "Installer/aqua")
		assert.Contains(t, out, "Tool/bat (0.24.0) [+ install]")
	})

	t.Run("blank line guard: exactly one blank between Privileged and System sections", func(t *testing.T) {
		t.Parallel()
		var buf bytes.Buffer
		renderSectionedTree(graph.NewTreePrinter(&buf, true), &buf, mixed())
		out := buf.String()
		// "Privileged Tools (--system):" section ends with lazygit, then
		// exactly one blank line, then "System:" heading.
		assert.Contains(t, out, "Tool/lazygit (v0.62.1) [⊘ skip]\n\nSystem:\n")
		// And NO double blank line (would indicate stray fmt.Fprintln).
		assert.NotContains(t, out, "\n\n\nSystem:\n")
	})

	t.Run("no privileged → Privileged Tools section omitted", func(t *testing.T) {
		t.Parallel()
		r := &planResult{
			resolver: &stubResolver{
				nodes: []*graph.Node{nAqua, nBat, nApt, nPkgs},
				edges: []graph.Edge{
					{From: nBat.ID, To: nAqua.ID},
					{From: nPkgs.ID, To: nApt.ID},
				},
			},
			resourceInfo: map[graph.NodeID]graph.ResourceInfo{
				nBat.ID:  {Kind: resource.KindTool, Name: "bat", Action: resource.ActionInstall},
				nApt.ID:  {Kind: resource.KindSystemInstaller, Name: "apt", Action: resource.ActionInstall},
				nPkgs.ID: {Kind: resource.KindSystemPackageSet, Name: "build-deps", Action: resource.ActionInstall},
			},
		}
		var buf bytes.Buffer
		renderSectionedTree(graph.NewTreePrinter(&buf, true), &buf, r)
		out := buf.String()
		assert.Contains(t, out, "Tools:\n")
		assert.NotContains(t, out, "Privileged Tools")
		assert.Contains(t, out, "System:\n")
	})

	t.Run("only privileged → only that section appears", func(t *testing.T) {
		t.Parallel()
		r := &planResult{
			resolver: &stubResolver{
				nodes: []*graph.Node{nLazygit},
				edges: nil,
			},
			resourceInfo: map[graph.NodeID]graph.ResourceInfo{
				nLazygit.ID: {Kind: resource.KindTool, Name: "lazygit", Action: resource.ActionInstall, Privileged: true},
			},
		}
		var buf bytes.Buffer
		renderSectionedTree(graph.NewTreePrinter(&buf, true), &buf, r)
		out := buf.String()
		assert.NotContains(t, out, "Tools:\n")
		assert.NotContains(t, out, "System:\n")
		assert.Contains(t, out, "Privileged Tools (--system):\n")
		assert.Contains(t, out, "Tool/lazygit")
	})

	t.Run("privileged Tool removal entry lands under Privileged section", func(t *testing.T) {
		t.Parallel()
		// Simulates buildResourceInfo's removal-detection branch carrying
		// Privileged through from ToolState. Without that propagation, this
		// entry would render under "Tools:" instead.
		nGone := makeNode(resource.KindTool, "gone-priv")
		r := &planResult{
			resolver: &stubResolver{nodes: []*graph.Node{nGone}},
			resourceInfo: map[graph.NodeID]graph.ResourceInfo{
				nGone.ID: {Kind: resource.KindTool, Name: "gone-priv", Version: "1.0.0", Action: resource.ActionRemove, Privileged: true},
			},
		}
		var buf bytes.Buffer
		renderSectionedTree(graph.NewTreePrinter(&buf, true), &buf, r)
		out := buf.String()
		assert.NotContains(t, out, "Tools:\n")
		assert.Contains(t, out, "Privileged Tools (--system):\n")
		assert.Contains(t, out, "Tool/gone-priv (1.0.0) [- remove]")
	})
}
