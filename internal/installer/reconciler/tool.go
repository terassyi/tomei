package reconciler

import (
	"github.com/terassyi/tomei/internal/resource"
)

// specVersionChanged is a thin alias for resource.SpecVersionDiffers, kept so the
// tool/runtime comparators (and their tests) read naturally. The version-change
// rules live in resource.SpecVersionDiffers — the single source of truth shared
// with `tomei plan` so the two agree on *version drift* specifically. (Plan and
// apply can still differ on other axes, e.g. plan labels a tainted resource
// ActionReinstall while the reconciler maps any change to ActionUpgrade.)
func specVersionChanged(specVersion string, stateVersionKind resource.VersionKind, stateVersion, stateSpecVersion string) bool {
	return resource.SpecVersionDiffers(specVersion, stateVersionKind, stateVersion, stateSpecVersion)
}

// ToolComparator returns a comparator for Tool resources.
//
// Branch ordering MUST match cmd/tomei/plan.go's buildResourceInfo Tool
// switch — otherwise plan output and the engine disagree on the reason
// surfaced when multiple signals fire at once.
func ToolComparator() Comparator[*resource.Tool, *resource.ToolState] {
	return func(res *resource.Tool, state *resource.ToolState) (bool, string) {
		// SHA pinning is checked first. sha→sha rotation, sha→version,
		// and version→sha switches are all surfaced by simple string equality
		// against state.SHA. Ordering note: a sha change short-circuits the
		// Version/taint branches below — taint will be cleared by the reinstall
		// regardless.
		if res.ToolSpec.SHA != state.SHA {
			return true, "sha changed: " + state.SHA + " -> " + res.ToolSpec.SHA
		}
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
		// Detect privileged change only for commands-based tools.
		// Privileged is persisted on all install patterns that flow through
		// the user-visible apply path (#230: buildState also stamps it for
		// download/registry to pre-wire the state-driven removal-skip gate),
		// but the comparator only treats a privileged-flip as a *reinstall*
		// signal when Commands is involved — for download/registry the flag
		// itself does not change what gets installed (binary contents are
		// identical), and SUB5's BinDirKind routing will handle relocations
		// via the install/update path, not via this comparator.
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
