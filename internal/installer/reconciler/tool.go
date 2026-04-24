package reconciler

import (
	"github.com/terassyi/tomei/internal/resource"
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
		return !resource.IsLatestVersion(specVersion)
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
			return true, "tainted: " + string(state.TaintReason)
		}
		// Detect binaryName change (both setting and unsetting)
		if res.ToolSpec.BinaryName != state.BinaryName {
			return true, "binaryName changed: " + state.BinaryName + " -> " + res.ToolSpec.BinaryName
		}
		// Detect privileged change only for commands-based tools. The flag
		// is a spec-vs-state drift signal (it gates --system and is persisted
		// in state). Other install patterns ignore the privileged flag and do
		// not persist it.
		if (res.ToolSpec.Commands != nil || state.Commands != nil) && res.ToolSpec.Privileged != state.Privileged {
			return true, "privileged changed"
		}
		return false, ""
	}
}

// NewToolReconciler creates a new Reconciler for Tool resources.
func NewToolReconciler() *Reconciler[*resource.Tool, *resource.ToolState] {
	return New(ToolComparator())
}
