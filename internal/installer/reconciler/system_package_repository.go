package reconciler

import (
	"maps"
	"slices"

	"github.com/terassyi/tomei/internal/resource"
)

// SystemPackageRepositoryComparator returns a comparator for SystemPackageRepository resources.
// It detects changes in the repository source configuration that require re-setup.
func SystemPackageRepositoryComparator() Comparator[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState] {
	return func(res *resource.SystemPackageRepository, state *resource.SystemPackageRepositoryState) (bool, string) {
		spec := res.SystemPackageRepositorySpec
		if spec.InstallerRef != state.InstallerRef {
			return true, "installerRef changed: " + state.InstallerRef + " -> " + spec.InstallerRef
		}
		if spec.Source.URL != state.Source.URL {
			return true, "source URL changed: " + state.Source.URL + " -> " + spec.Source.URL
		}
		if spec.Source.KeyURL != state.Source.KeyURL {
			return true, "source key URL changed: " + state.Source.KeyURL + " -> " + spec.Source.KeyURL
		}
		if spec.Source.KeyHash != state.Source.KeyHash {
			return true, "source key hash changed: " + state.Source.KeyHash + " -> " + spec.Source.KeyHash
		}
		if spec.Source.Suite != state.Source.Suite {
			return true, "source suite changed: " + state.Source.Suite + " -> " + spec.Source.Suite
		}
		if !slices.Equal(spec.Source.Components, state.Source.Components) {
			return true, "source components changed"
		}
		if !maps.Equal(spec.Source.Options, state.Source.Options) {
			return true, "source options changed"
		}
		return false, ""
	}
}

// NewSystemPackageRepositoryReconciler creates a new Reconciler for SystemPackageRepository resources.
func NewSystemPackageRepositoryReconciler() *Reconciler[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState] {
	return New(SystemPackageRepositoryComparator())
}
