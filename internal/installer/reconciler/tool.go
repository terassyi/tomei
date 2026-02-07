package reconciler

import (
	"github.com/terassyi/toto/internal/resource"
)

// specVersionChanged determines whether the spec's version specification
// has changed compared to what is recorded in state, based on the VersionKind.
//
// Rules:
//   - VersionLatest: only changed if spec switches to a non-empty version
//     (actual latest updates are driven by --sync taint, not reconciler)
//   - VersionAlias: changed if spec version differs from the stored alias (state.SpecVersion)
//   - VersionExact: changed if spec version differs from the installed version (state.Version)
func specVersionChanged(specVersion string, stateVersionKind resource.VersionKind, stateVersion, stateSpecVersion string) bool {
	switch stateVersionKind {
	case resource.VersionLatest:
		return specVersion != ""
	case resource.VersionAlias:
		return specVersion != stateSpecVersion
	default: // VersionExact
		return specVersion != stateVersion
	}
}

// ToolComparator returns a comparator for Tool resources.
func ToolComparator() Comparator[*resource.Tool, *resource.ToolState] {
	return func(res *resource.Tool, state *resource.ToolState) (bool, string) {
		if specVersionChanged(res.ToolSpec.Version, state.VersionKind, state.Version, state.SpecVersion) {
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
