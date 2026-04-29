package reconciler

import (
	"github.com/terassyi/tomei/internal/resource"
)

// SystemInstallerComparator returns a comparator for SystemInstaller resources.
// It never triggers an upgrade because system installers are managed by the OS;
// tomei only records the detected version. Install and remove actions are still
// emitted by the generic Reconciler when a resource is added to or removed from the spec.
func SystemInstallerComparator() Comparator[*resource.SystemInstaller, *resource.SystemInstallerState] {
	return func(_ *resource.SystemInstaller, _ *resource.SystemInstallerState) (bool, string) {
		return false, ""
	}
}

// NewSystemInstallerReconciler creates a new Reconciler for SystemInstaller resources.
func NewSystemInstallerReconciler() *Reconciler[*resource.SystemInstaller, *resource.SystemInstallerState] {
	return New(SystemInstallerComparator())
}
