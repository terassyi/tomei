package reconciler

import (
	"maps"
	"slices"

	"github.com/terassyi/tomei/internal/resource"
)

// SystemPackageRepositoryComparator returns a comparator for SystemPackageRepository resources.
// It detects changes in the repository source configuration that require re-setup.
// The comparator dispatches on InstallerRef so each installer's source arm is
// diffed using its own field set; an InstallerRef change is always a difference.
//
// Defensive nil handling: the state store's validator only checks the file
// version, not the contents of the SystemPackageRepositories map, so a
// corrupt or partially-migrated state file may legitimately contain a nil
// entry. Likewise, the engine could (in principle) hand the reconciler a
// resource whose SystemPackageRepositorySpec was never populated. Both
// cases are treated as "needs update" rather than panicking; the
// subsequent install run will overwrite the state with a coherent value.
func SystemPackageRepositoryComparator() Comparator[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState] {
	return func(res *resource.SystemPackageRepository, state *resource.SystemPackageRepositoryState) (bool, string) {
		if res == nil || res.SystemPackageRepositorySpec == nil {
			return true, "spec missing (corrupt or unpopulated resource)"
		}
		if state == nil {
			return true, "state missing (first install or state migration)"
		}
		spec := res.SystemPackageRepositorySpec
		if spec.InstallerRef != state.InstallerRef {
			return true, "installerRef changed: " + state.InstallerRef + " -> " + spec.InstallerRef
		}
		switch spec.InstallerRef {
		case resource.InstallerRefApt:
			return compareAptSource(spec.Apt, state.Apt)
		default:
			// Defensive: validation should have rejected the spec before
			// reaching the reconciler, but keep a generic "config changed"
			// signal so an unknown installer is treated as needing re-setup.
			return true, "unknown installerRef " + spec.InstallerRef
		}
	}
}

// compareAptSource diffs two *AptSource values field-by-field. Either
// side being nil is treated as a structural change. spec.Apt nil is
// forbidden by SystemPackageRepositorySpec.Validate at the apply
// boundary, so the nil-spec branch is defense-in-depth for callers that
// bypass Validate. state.Apt nil happens during a clean Install (state
// not yet populated) or after a manually-mutated state file — in either
// case the right reconciliation is "treat as missing, force reinstall."
func compareAptSource(spec, state *resource.AptSource) (bool, string) {
	if spec == nil {
		return true, "apt source removed from spec"
	}
	if state == nil {
		return true, "apt source not recorded in state (first install or state migration)"
	}
	if spec.URL != state.URL {
		return true, "apt source URL changed: " + state.URL + " -> " + spec.URL
	}
	if spec.KeyURL != state.KeyURL {
		return true, "apt source key URL changed: " + state.KeyURL + " -> " + spec.KeyURL
	}
	if spec.KeyHash != state.KeyHash {
		return true, "apt source key hash changed: " + state.KeyHash + " -> " + spec.KeyHash
	}
	if spec.Suite != state.Suite {
		return true, "apt source suite changed: " + state.Suite + " -> " + spec.Suite
	}
	if !slices.Equal(spec.Components, state.Components) {
		return true, "apt source components changed"
	}
	if !maps.Equal(spec.Options, state.Options) {
		return true, "apt source options changed"
	}
	return false, ""
}

// NewSystemPackageRepositoryReconciler creates a new Reconciler for SystemPackageRepository resources.
func NewSystemPackageRepositoryReconciler() *Reconciler[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState] {
	return New(SystemPackageRepositoryComparator())
}
