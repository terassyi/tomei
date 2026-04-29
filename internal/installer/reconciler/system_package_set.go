package reconciler

import (
	"strings"

	"github.com/terassyi/tomei/internal/resource"
)

// SystemPackageSetComparator returns a comparator for SystemPackageSet resources.
// It detects changes in the package list (order-insensitive) and installer/repository references.
func SystemPackageSetComparator() Comparator[*resource.SystemPackageSet, *resource.SystemPackageSetState] {
	return func(res *resource.SystemPackageSet, state *resource.SystemPackageSetState) (bool, string) {
		spec := res.SystemPackageSetSpec
		if spec.InstallerRef != state.InstallerRef {
			return true, "installerRef changed: " + state.InstallerRef + " -> " + spec.InstallerRef
		}
		if spec.RepositoryRef != state.RepositoryRef {
			return true, "repositoryRef changed: " + state.RepositoryRef + " -> " + spec.RepositoryRef
		}
		if added, removed := packageDiff(spec.Packages, state.Packages); len(added) > 0 || len(removed) > 0 {
			return true, formatPackageDiffReason(added, removed)
		}
		return false, ""
	}
}

// NewSystemPackageSetReconciler creates a new Reconciler for SystemPackageSet resources.
func NewSystemPackageSetReconciler() *Reconciler[*resource.SystemPackageSet, *resource.SystemPackageSetState] {
	return New(SystemPackageSetComparator())
}

// packageDiff returns packages added to and removed from the spec compared to state.
func packageDiff(spec, state []string) (added, removed []string) {
	specSet := make(map[string]bool, len(spec))
	for _, p := range spec {
		specSet[p] = true
	}
	stateSet := make(map[string]bool, len(state))
	for _, p := range state {
		stateSet[p] = true
	}

	for _, p := range spec {
		if !stateSet[p] {
			added = append(added, p)
		}
	}
	for _, p := range state {
		if !specSet[p] {
			removed = append(removed, p)
		}
	}
	return added, removed
}

// formatPackageDiffReason formats added/removed packages into a human-readable reason string.
func formatPackageDiffReason(added, removed []string) string {
	var parts []string
	for _, p := range added {
		parts = append(parts, "+"+p)
	}
	for _, p := range removed {
		parts = append(parts, "-"+p)
	}
	return "packages changed: " + strings.Join(parts, ", ")
}
