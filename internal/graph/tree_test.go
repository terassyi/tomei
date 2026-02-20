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
