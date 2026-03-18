package resource

import (
	"cmp"
	"fmt"
	"log/slog"
	"slices"
)

// isEnabled reports whether a resource should be included in processing.
// Resources that do not implement Enableable are always enabled.
func isEnabled(res Resource) bool {
	if e, ok := res.(Enableable); ok {
		return e.IsEnabled()
	}
	return true
}

// ExpandSets expands all Expandable resources into individual resources
// and filters out disabled resources (those implementing Enableable with IsEnabled() == false).
// Expandable resources are removed from the output; expanded resources are added.
// Returns an error if expanded resource names conflict with existing resources
// or with resources from other Expandable sets.
func ExpandSets(resources []Resource) ([]Resource, error) {
	// Track resource identities (Kind/Name) to detect conflicts.
	// Value is the source description.
	names := make(map[string]string)

	// Register non-expandable resource names first.
	// Disabled resources are excluded so they do not cause spurious conflicts.
	for _, res := range resources {
		if !isEnabled(res) {
			continue
		}
		if _, ok := res.(Expandable); !ok {
			key := string(res.Kind()) + "/" + res.Name()
			names[key] = fmt.Sprintf("standalone %s", res.Kind())
		}
	}

	var result []Resource

	for _, res := range resources {
		if !isEnabled(res) {
			slog.Debug("skipping disabled resource", "kind", res.Kind(), "name", res.Name())
			continue
		}

		exp, ok := res.(Expandable)
		if !ok {
			result = append(result, res)
			continue
		}

		expanded, err := exp.Expand()
		if err != nil {
			return nil, fmt.Errorf("failed to expand %s %q: %w", res.Kind(), res.Name(), err)
		}

		// Check for name conflicts among expanded resources
		for _, r := range expanded {
			key := string(r.Kind()) + "/" + r.Name()
			if source, exists := names[key]; exists {
				return nil, fmt.Errorf("name conflict: %s %q expands %s %q which conflicts with %s",
					res.Kind(), res.Name(), r.Kind(), r.Name(), source)
			}
			names[key] = fmt.Sprintf("%s %q", res.Kind(), res.Name())
		}

		result = append(result, expanded...)
	}

	return result, nil
}

// CollectDisabled returns disabled resources for plan display.
// Standalone disabled resources are returned as-is.
// For ToolSet, each disabled ToolItem is returned as an individual Tool.
func CollectDisabled(resources []Resource) []Resource {
	var disabled []Resource

	for _, res := range resources {
		switch r := res.(type) {
		case *Tool:
			if !r.IsEnabled() {
				disabled = append(disabled, r)
			}
		case *ToolSet:
			for name, item := range r.ToolSetSpec.Tools {
				if !item.IsEnabled() {
					disabled = append(disabled, buildToolFromSetItem(r, name, item))
				}
			}
		default:
			if e, ok := res.(Enableable); ok && !e.IsEnabled() {
				disabled = append(disabled, res)
			}
		}
	}

	// Sort by kind then name for deterministic output
	slices.SortFunc(disabled, func(a, b Resource) int {
		if c := cmp.Compare(string(a.Kind()), string(b.Kind())); c != 0 {
			return c
		}
		return cmp.Compare(a.Name(), b.Name())
	})

	return disabled
}
