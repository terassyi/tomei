package graph

import (
	"encoding/json"
	"io"

	"github.com/goccy/go-yaml"

	"github.com/terassyi/toto/internal/resource"
)

// PlanOutput represents the structured output of a plan.
type PlanOutput struct {
	Resources []PlanResource `json:"resources" yaml:"resources"`
	Layers    []PlanLayer    `json:"layers" yaml:"layers"`
	Summary   PlanSummary    `json:"summary" yaml:"summary"`
}

// PlanResource represents a resource in the plan output.
type PlanResource struct {
	Kind         resource.Kind `json:"kind" yaml:"kind"`
	Name         string        `json:"name" yaml:"name"`
	Version      string        `json:"version,omitempty" yaml:"version,omitempty"`
	Action       Action        `json:"action" yaml:"action"`
	Layer        int           `json:"layer" yaml:"layer"`
	Dependencies []string      `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`
}

// PlanLayer represents an execution layer in the plan output.
type PlanLayer struct {
	Index     int      `json:"index" yaml:"index"`
	Resources []string `json:"resources" yaml:"resources"`
}

// PlanSummary represents the summary of actions in the plan.
type PlanSummary struct {
	Total     int `json:"total" yaml:"total"`
	Layers    int `json:"layers" yaml:"layers"`
	Install   int `json:"install" yaml:"install"`
	Upgrade   int `json:"upgrade" yaml:"upgrade"`
	Reinstall int `json:"reinstall" yaml:"reinstall"`
	Remove    int `json:"remove" yaml:"remove"`
	NoChange  int `json:"noChange" yaml:"noChange"`
}

// Exporter exports plan data in various formats.
type Exporter struct {
	layers       []Layer
	resourceInfo map[NodeID]ResourceInfo
	edges        []Edge
}

// NewExporter creates a new Exporter.
func NewExporter(layers []Layer, resourceInfo map[NodeID]ResourceInfo, edges []Edge) *Exporter {
	return &Exporter{
		layers:       layers,
		resourceInfo: resourceInfo,
		edges:        edges,
	}
}

// BuildOutput builds the PlanOutput structure.
func (e *Exporter) BuildOutput() PlanOutput {
	output := PlanOutput{
		Resources: make([]PlanResource, 0),
		Layers:    make([]PlanLayer, 0, len(e.layers)),
	}

	// Build dependency map
	deps := make(map[NodeID][]string)
	for _, edge := range e.edges {
		deps[edge.From] = append(deps[edge.From], string(edge.To))
	}

	// Build resources and layers
	for i, layer := range e.layers {
		planLayer := PlanLayer{
			Index:     i + 1,
			Resources: make([]string, 0, len(layer.Nodes)),
		}

		for _, node := range layer.Nodes {
			nodeID := node.ID
			info := e.resourceInfo[nodeID]

			planResource := PlanResource{
				Kind:         node.Kind,
				Name:         node.Name,
				Version:      info.Version,
				Action:       info.Action,
				Layer:        i + 1,
				Dependencies: deps[nodeID],
			}
			output.Resources = append(output.Resources, planResource)
			planLayer.Resources = append(planLayer.Resources, string(nodeID))
		}

		output.Layers = append(output.Layers, planLayer)
	}

	// Build summary
	summary := PlanSummary{
		Total:  len(output.Resources),
		Layers: len(output.Layers),
	}
	for _, res := range output.Resources {
		switch res.Action {
		case ActionInstall:
			summary.Install++
		case ActionUpgrade:
			summary.Upgrade++
		case ActionReinstall:
			summary.Reinstall++
		case ActionRemove:
			summary.Remove++
		case ActionNone:
			summary.NoChange++
		}
	}
	output.Summary = summary

	return output
}

// ExportJSON writes the plan as JSON.
func (e *Exporter) ExportJSON(w io.Writer) error {
	output := e.BuildOutput()
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(output)
}

// ExportYAML writes the plan as YAML.
func (e *Exporter) ExportYAML(w io.Writer) error {
	output := e.BuildOutput()
	data, err := yaml.MarshalWithOptions(output, yaml.Indent(2))
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
