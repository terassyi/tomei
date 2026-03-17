package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/terassyi/tomei/internal/resource"
)

func TestBuildOutput_SkipResources(t *testing.T) {
	t.Parallel()

	t.Run("skip resources appear with layer 0 and are sorted", func(t *testing.T) {
		t.Parallel()

		layers := []Layer{
			{Nodes: []*Node{
				{ID: NewNodeID(resource.KindTool, "rg"), Kind: resource.KindTool, Name: "rg"},
			}},
		}
		resourceInfo := map[NodeID]ResourceInfo{
			NewNodeID(resource.KindTool, "rg"):  {Kind: resource.KindTool, Name: "rg", Version: "14.1.1", Action: resource.ActionInstall},
			NewNodeID(resource.KindTool, "fd"):  {Kind: resource.KindTool, Name: "fd", Version: "9.0.0", Action: resource.ActionSkip},
			NewNodeID(resource.KindTool, "bat"): {Kind: resource.KindTool, Name: "bat", Version: "0.24.0", Action: resource.ActionSkip},
		}

		exporter := NewExporter(layers, resourceInfo, nil)
		output := exporter.BuildOutput()

		// Layer resources first, then sorted skip resources
		assert.Len(t, output.Resources, 3)
		assert.Equal(t, "rg", output.Resources[0].Name)
		assert.Equal(t, 1, output.Resources[0].Layer)
		// Skip resources sorted: bat before fd
		assert.Equal(t, "bat", output.Resources[1].Name)
		assert.Equal(t, 0, output.Resources[1].Layer)
		assert.Equal(t, resource.ActionSkip, output.Resources[1].Action)
		assert.Equal(t, "fd", output.Resources[2].Name)
		assert.Equal(t, 0, output.Resources[2].Layer)
		assert.Equal(t, resource.ActionSkip, output.Resources[2].Action)

		// Summary
		assert.Equal(t, 3, output.Summary.Total)
		assert.Equal(t, 1, output.Summary.Install)
		assert.Equal(t, 2, output.Summary.Skip)
		assert.Equal(t, 0, output.Summary.NoChange)
	})

	t.Run("no skip resources produces empty skip count", func(t *testing.T) {
		t.Parallel()

		layers := []Layer{
			{Nodes: []*Node{
				{ID: NewNodeID(resource.KindTool, "rg"), Kind: resource.KindTool, Name: "rg"},
			}},
		}
		resourceInfo := map[NodeID]ResourceInfo{
			NewNodeID(resource.KindTool, "rg"): {Kind: resource.KindTool, Name: "rg", Version: "14.1.1", Action: resource.ActionNone},
		}

		exporter := NewExporter(layers, resourceInfo, nil)
		output := exporter.BuildOutput()

		assert.Len(t, output.Resources, 1)
		assert.Equal(t, 0, output.Summary.Skip)
		assert.Equal(t, 1, output.Summary.NoChange)
	})

	t.Run("skip resources already in layer are not duplicated", func(t *testing.T) {
		t.Parallel()

		// Edge case: a skip resource that somehow ended up in a layer node
		layers := []Layer{
			{Nodes: []*Node{
				{ID: NewNodeID(resource.KindTool, "bat"), Kind: resource.KindTool, Name: "bat"},
			}},
		}
		resourceInfo := map[NodeID]ResourceInfo{
			NewNodeID(resource.KindTool, "bat"): {Kind: resource.KindTool, Name: "bat", Version: "0.24.0", Action: resource.ActionSkip},
		}

		exporter := NewExporter(layers, resourceInfo, nil)
		output := exporter.BuildOutput()

		// Should appear only once (from layer), not duplicated
		assert.Len(t, output.Resources, 1)
		assert.Equal(t, 1, output.Summary.Skip)
	})
}
