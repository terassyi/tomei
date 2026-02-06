package graph

import (
	"github.com/terassyi/toto/internal/errors"
)

// NewCycleError creates a DependencyError for circular dependencies.
// The cycle should include the starting node at both the beginning and end.
func NewCycleError(cycle []NodeID) *errors.DependencyError {
	cycleStrings := make([]string, len(cycle))
	for i, id := range cycle {
		cycleStrings[i] = string(id)
	}
	return errors.NewCycleError(cycleStrings)
}
