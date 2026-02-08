package reconciler

import (
	"github.com/terassyi/tomei/internal/resource"
)

// RuntimeComparator returns a comparator for Runtime resources.
func RuntimeComparator() Comparator[*resource.Runtime, *resource.RuntimeState] {
	return func(res *resource.Runtime, state *resource.RuntimeState) (bool, string) {
		if specVersionChanged(res.RuntimeSpec.Version, state.VersionKind, state.Version, state.SpecVersion) {
			return true, "version changed: " + state.Version + " -> " + res.RuntimeSpec.Version
		}
		return false, ""
	}
}

// NewRuntimeReconciler creates a new Reconciler for Runtime resources.
func NewRuntimeReconciler() *Reconciler[*resource.Runtime, *resource.RuntimeState] {
	return New(RuntimeComparator())
}
