package reconciler

import (
	"github.com/terassyi/tomei/internal/resource"
)

// Action represents a planned operation on a resource.
type Action[R resource.Resource, S resource.State] struct {
	Type     resource.ActionType
	Name     string
	Resource R // The desired resource (zero value for Remove)
	State    S // The current state (zero value for Install)
	Reason   string
}

// Comparator compares a resource spec with its current state to determine if an update is needed.
// Returns true if the resource needs to be updated, along with the reason.
type Comparator[R resource.Resource, S resource.State] func(res R, state S) (needsUpdate bool, reason string)

// Reconciler compares desired resources with current state and generates actions.
type Reconciler[R resource.Resource, S resource.State] struct {
	compare Comparator[R, S]
}

// New creates a new Reconciler with the given comparator.
func New[R resource.Resource, S resource.State](compare Comparator[R, S]) *Reconciler[R, S] {
	return &Reconciler[R, S]{
		compare: compare,
	}
}

// Reconcile compares resources with state and returns required actions.
func (r *Reconciler[R, S]) Reconcile(resources []R, states map[string]S) []Action[R, S] {
	var actions []Action[R, S]

	// Build a set of resource names in spec
	specResources := make(map[string]R)
	for _, res := range resources {
		specResources[res.Name()] = res
	}

	// Check each resource in spec
	for name, res := range specResources {
		currentState, exists := states[name]

		if !exists {
			// Resource not in state -> Install
			var zeroState S
			actions = append(actions, Action[R, S]{
				Type:     resource.ActionInstall,
				Name:     name,
				Resource: res,
				State:    zeroState,
				Reason:   "new resource",
			})
			continue
		}

		// Resource exists, check if update needed
		if needsUpdate, reason := r.compare(res, currentState); needsUpdate {
			actions = append(actions, Action[R, S]{
				Type:     resource.ActionUpgrade,
				Name:     name,
				Resource: res,
				State:    currentState,
				Reason:   reason,
			})
		}
	}

	// Check for resources in state but not in spec -> Remove
	for name, state := range states {
		if _, exists := specResources[name]; !exists {
			var zeroRes R
			actions = append(actions, Action[R, S]{
				Type:     resource.ActionRemove,
				Name:     name,
				Resource: zeroRes,
				State:    state,
				Reason:   "removed from spec",
			})
		}
	}

	return actions
}
