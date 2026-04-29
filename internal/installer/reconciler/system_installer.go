package reconciler

import (
	"github.com/terassyi/tomei/internal/resource"
)

// SystemInstallerComparator returns a comparator for SystemInstaller resources.
// It always returns false because system installers are managed by the OS;
// tomei only records the detected version and never upgrades them.
func SystemInstallerComparator() Comparator[*resource.SystemInstaller, *resource.SystemInstallerState] {
	return func(_ *resource.SystemInstaller, _ *resource.SystemInstallerState) (bool, string) {
		return false, ""
	}
}

// NewSystemInstallerReconciler creates a new Reconciler for SystemInstaller resources.
func NewSystemInstallerReconciler() *Reconciler[*resource.SystemInstaller, *resource.SystemInstallerState] {
	return New(SystemInstallerComparator())
}
