package reconciler

import (
	"github.com/terassyi/toto/internal/resource"
)

// ToolComparator returns a comparator for Tool resources.
func ToolComparator() Comparator[*resource.Tool, *resource.ToolState] {
	return func(res *resource.Tool, state *resource.ToolState) (bool, string) {
		if res.ToolSpec.Version != state.Version {
			return true, "version changed: " + state.Version + " -> " + res.ToolSpec.Version
		}
		if state.IsTainted() {
			return true, "tainted: " + state.TaintReason
		}
		return false, ""
	}
}

// NewToolReconciler creates a new Reconciler for Tool resources.
func NewToolReconciler() *Reconciler[*resource.Tool, *resource.ToolState] {
	return New(ToolComparator())
}
