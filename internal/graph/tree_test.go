package graph

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/terassyi/tomei/internal/resource"
)

func TestPrintSummary(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		info     map[NodeID]ResourceInfo
		wantLine string
	}{
		{
			name:     "no actions",
			info:     map[NodeID]ResourceInfo{},
			wantLine: "\nSummary: 0 to install, 0 to upgrade, 0 to reinstall, 0 to remove\n",
		},
		{
			name: "install only",
			info: map[NodeID]ResourceInfo{
				NewNodeID(resource.KindTool, "gopls"): {Kind: resource.KindTool, Name: "gopls", Action: resource.ActionInstall},
				NewNodeID(resource.KindTool, "dlv"):   {Kind: resource.KindTool, Name: "dlv", Action: resource.ActionInstall},
				NewNodeID(resource.KindRuntime, "go"): {Kind: resource.KindRuntime, Name: "go", Action: resource.ActionNone},
			},
			wantLine: "\nSummary: 2 to install, 0 to upgrade, 0 to reinstall, 0 to remove\n",
		},
		{
			name: "upgrade triggers reinstall",
			info: map[NodeID]ResourceInfo{
				NewNodeID(resource.KindRuntime, "go"): {Kind: resource.KindRuntime, Name: "go", Version: "1.25.6", Action: resource.ActionUpgrade},
				NewNodeID(resource.KindTool, "gopls"): {Kind: resource.KindTool, Name: "gopls", Action: resource.ActionReinstall},
				NewNodeID(resource.KindTool, "dlv"):   {Kind: resource.KindTool, Name: "dlv", Action: resource.ActionReinstall},
			},
			wantLine: "\nSummary: 0 to install, 1 to upgrade, 2 to reinstall, 0 to remove\n",
		},
		{
			name: "mixed actions",
			info: map[NodeID]ResourceInfo{
				NewNodeID(resource.KindRuntime, "go"): {Kind: resource.KindRuntime, Name: "go", Action: resource.ActionUpgrade},
				NewNodeID(resource.KindTool, "gopls"): {Kind: resource.KindTool, Name: "gopls", Action: resource.ActionReinstall},
				NewNodeID(resource.KindTool, "fd"):    {Kind: resource.KindTool, Name: "fd", Action: resource.ActionInstall},
				NewNodeID(resource.KindTool, "old"):   {Kind: resource.KindTool, Name: "old", Action: resource.ActionRemove},
				NewNodeID(resource.KindTool, "bat"):   {Kind: resource.KindTool, Name: "bat", Action: resource.ActionNone},
			},
			wantLine: "\nSummary: 1 to install, 1 to upgrade, 1 to reinstall, 1 to remove\n",
		},
		{
			name: "remove only",
			info: map[NodeID]ResourceInfo{
				NewNodeID(resource.KindTool, "old-tool"): {Kind: resource.KindTool, Name: "old-tool", Action: resource.ActionRemove},
			},
			wantLine: "\nSummary: 0 to install, 0 to upgrade, 0 to reinstall, 1 to remove\n",
		},
		{
			name: "all none is zero counts",
			info: map[NodeID]ResourceInfo{
				NewNodeID(resource.KindTool, "gopls"): {Kind: resource.KindTool, Name: "gopls", Action: resource.ActionNone},
				NewNodeID(resource.KindRuntime, "go"): {Kind: resource.KindRuntime, Name: "go", Action: resource.ActionNone},
			},
			wantLine: "\nSummary: 0 to install, 0 to upgrade, 0 to reinstall, 0 to remove\n",
		},
		{
			name: "with skip count",
			info: map[NodeID]ResourceInfo{
				NewNodeID(resource.KindTool, "fd"):  {Kind: resource.KindTool, Name: "fd", Action: resource.ActionInstall},
				NewNodeID(resource.KindTool, "bat"): {Kind: resource.KindTool, Name: "bat", Action: resource.ActionSkip},
				NewNodeID(resource.KindTool, "rg"):  {Kind: resource.KindTool, Name: "rg", Action: resource.ActionSkip},
			},
			wantLine: "\nSummary: 1 to install, 0 to upgrade, 0 to reinstall, 0 to remove, 2 disabled\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			printer := NewTreePrinter(&buf, true)
			printer.PrintSummary(tt.info)

			assert.Equal(t, tt.wantLine, buf.String())
		})
	}
}

// stubResolver is a minimal Resolver implementation for tree tests so we
// can fix the node/edge set without going through resource.Resource.
type stubResolver struct {
	nodes []*Node
	edges []Edge
}

func (s *stubResolver) AddResource(resource.Resource) {}
func (s *stubResolver) Resolve() ([]Layer, error)     { return nil, nil }
func (s *stubResolver) Validate() error               { return nil }
func (s *stubResolver) NodeCount() int                { return len(s.nodes) }
func (s *stubResolver) EdgeCount() int                { return len(s.edges) }
func (s *stubResolver) GetEdges() []Edge              { return s.edges }
func (s *stubResolver) GetNodes() []*Node             { return s.nodes }

func newStubResolver(nodes []*Node, edges []Edge) *stubResolver {
	return &stubResolver{nodes: nodes, edges: edges}
}

// fixture: Installer/aqua parents Tool/bat and Tool/lazygit (privileged);
// SystemInstaller/apt parents SystemPackageSet/build-deps.
func sampleGraph() (*stubResolver, map[NodeID]ResourceInfo) {
	nAqua := &Node{ID: NewNodeID(resource.KindInstaller, "aqua"), Kind: resource.KindInstaller, Name: "aqua"}
	nBat := &Node{ID: NewNodeID(resource.KindTool, "bat"), Kind: resource.KindTool, Name: "bat"}
	nLazygit := &Node{ID: NewNodeID(resource.KindTool, "lazygit"), Kind: resource.KindTool, Name: "lazygit"}
	nApt := &Node{ID: NewNodeID(resource.KindSystemInstaller, "apt"), Kind: resource.KindSystemInstaller, Name: "apt"}
	nPkgs := &Node{ID: NewNodeID(resource.KindSystemPackageSet, "build-deps"), Kind: resource.KindSystemPackageSet, Name: "build-deps"}
	res := newStubResolver(
		[]*Node{nAqua, nBat, nLazygit, nApt, nPkgs},
		[]Edge{
			{From: nBat.ID, To: nAqua.ID},
			{From: nLazygit.ID, To: nAqua.ID},
			{From: nPkgs.ID, To: nApt.ID},
		},
	)
	info := map[NodeID]ResourceInfo{
		nAqua.ID:    {Kind: resource.KindInstaller, Name: "aqua", Action: resource.ActionNone},
		nBat.ID:     {Kind: resource.KindTool, Name: "bat", Version: "0.24.0", Action: resource.ActionInstall},
		nLazygit.ID: {Kind: resource.KindTool, Name: "lazygit", Version: "v0.62.1", Action: resource.ActionInstall, Privileged: true},
		nApt.ID:     {Kind: resource.KindSystemInstaller, Name: "apt", Action: resource.ActionNone},
		nPkgs.ID:    {Kind: resource.KindSystemPackageSet, Name: "build-deps", Action: resource.ActionInstall},
	}
	return res, info
}

func TestPrintTreeFiltered(t *testing.T) {
	t.Parallel()

	t.Run("all-pass filter equals PrintTree", func(t *testing.T) {
		t.Parallel()
		resolver, info := sampleGraph()

		var bufFull bytes.Buffer
		NewTreePrinter(&bufFull, true).PrintTree(resolver, info)

		var bufFiltered bytes.Buffer
		NewTreePrinter(&bufFiltered, true).PrintTreeFiltered(resolver, info, func(NodeID) bool { return true })

		assert.Equal(t, bufFull.String(), bufFiltered.String())
	})

	t.Run("empty filter emits nothing", func(t *testing.T) {
		t.Parallel()
		resolver, info := sampleGraph()
		var buf bytes.Buffer
		NewTreePrinter(&buf, true).PrintTreeFiltered(resolver, info, func(NodeID) bool { return false })
		assert.Empty(t, buf.String())
	})

	t.Run("excluding Installer/aqua promotes its tool children to roots", func(t *testing.T) {
		t.Parallel()
		resolver, info := sampleGraph()
		var buf bytes.Buffer
		NewTreePrinter(&buf, true).PrintTreeFiltered(resolver, info, func(id NodeID) bool {
			return id != NewNodeID(resource.KindInstaller, "aqua")
		})
		out := buf.String()
		// Both Tool/bat and Tool/lazygit appear at column 0 (no tree connector prefix).
		assert.Contains(t, out, "Tool/bat (0.24.0) [+ install]\n")
		assert.Contains(t, out, "Tool/lazygit (v0.62.1) [+ install]\n")
		assert.NotContains(t, out, "Installer/aqua")
	})

	t.Run("privileged-only filter surfaces lazygit as standalone root", func(t *testing.T) {
		t.Parallel()
		resolver, info := sampleGraph()
		var buf bytes.Buffer
		NewTreePrinter(&buf, true).PrintTreeFiltered(resolver, info, func(id NodeID) bool {
			return info[id].Privileged
		})
		out := buf.String()
		assert.Contains(t, out, "Tool/lazygit (v0.62.1) [+ install]\n")
		assert.NotContains(t, out, "Tool/bat")
		assert.NotContains(t, out, "Installer/aqua")
		assert.NotContains(t, out, "SystemInstaller")
	})
}

func TestPrintDisabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		infos    []ResourceInfo
		wantLine string
	}{
		{
			name: "single disabled tool",
			infos: []ResourceInfo{
				{Kind: resource.KindTool, Name: "bat", Version: "0.24.0", Action: resource.ActionSkip},
			},
			wantLine: "\nDisabled Resources:\n  Tool/bat (0.24.0) [⊘ skip]\n",
		},
		{
			name: "multiple disabled tools",
			infos: []ResourceInfo{
				{Kind: resource.KindTool, Name: "bat", Version: "0.24.0", Action: resource.ActionSkip},
				{Kind: resource.KindTool, Name: "rg", Version: "14.1.1", Action: resource.ActionSkip},
			},
			wantLine: "\nDisabled Resources:\n  Tool/bat (0.24.0) [⊘ skip]\n  Tool/rg (14.1.1) [⊘ skip]\n",
		},
		{
			name: "disabled tool without version",
			infos: []ResourceInfo{
				{Kind: resource.KindTool, Name: "bat", Action: resource.ActionSkip},
			},
			wantLine: "\nDisabled Resources:\n  Tool/bat [⊘ skip]\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			printer := NewTreePrinter(&buf, true)
			printer.PrintDisabled(tt.infos)

			assert.Equal(t, tt.wantLine, buf.String())
		})
	}
}
