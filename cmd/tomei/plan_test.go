package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/graph"
	"github.com/terassyi/tomei/internal/resource"
)

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
