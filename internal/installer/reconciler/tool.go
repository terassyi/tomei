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
//
// Branch ordering MUST match cmd/tomei/plan.go's buildResourceInfo Tool
// switch — otherwise plan output and the engine disagree on the reason
// surfaced when multiple signals fire at once.
func ToolComparator() Comparator[*resource.Tool, *resource.ToolState] {
	return func(res *resource.Tool, state *resource.ToolState) (bool, string) {
		// Ref pinning (SHA) is checked first. ref→ref rotation, ref→version,
		// and version→ref switches are all surfaced by simple string equality
		// against state.Ref. Ordering note: a ref change short-circuits the
		// Version/taint branches below — taint will be cleared by the reinstall
		// regardless.
		if res.ToolSpec.Ref != state.Ref {
			return true, "ref changed: " + state.Ref + " -> " + res.ToolSpec.Ref
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
