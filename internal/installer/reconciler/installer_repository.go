package reconciler

import (
	"github.com/terassyi/tomei/internal/resource"
)

// InstallerRepositoryComparator returns a comparator for InstallerRepository resources.
func InstallerRepositoryComparator() Comparator[*resource.InstallerRepository, *resource.InstallerRepositoryState] {
	return func(res *resource.InstallerRepository, state *resource.InstallerRepositoryState) (bool, string) {
		if res.InstallerRepositorySpec.Source.URL != state.URL {
			return true, "source URL changed: " + state.URL + " -> " + res.InstallerRepositorySpec.Source.URL
		}
		if res.InstallerRepositorySpec.Source.Type != state.SourceType {
			return true, "source type changed"
		}
		return false, ""
	}
}

// NewInstallerRepositoryReconciler creates a new Reconciler for InstallerRepository resources.
func NewInstallerRepositoryReconciler() *Reconciler[*resource.InstallerRepository, *resource.InstallerRepositoryState] {
	return New(InstallerRepositoryComparator())
}
