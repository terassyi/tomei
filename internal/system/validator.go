package system

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/terassyi/tomei/internal/resource"
)

// PackageManager represents a system package manager type.
type PackageManager string

const (
	PackageManagerAPT    PackageManager = "apt"
	PackageManagerDNF    PackageManager = "dnf"
	PackageManagerZypper PackageManager = "zypper"
	PackageManagerPacman PackageManager = "pacman"
	PackageManagerAPK    PackageManager = "apk"
)

// allPackageManagers is the set of known package managers, derived from packageManagersByID.
var allPackageManagers map[PackageManager]bool

func init() {
	allPackageManagers = make(map[PackageManager]bool)
	for _, pms := range packageManagersByID {
		for _, pm := range pms {
			allPackageManagers[pm] = true
		}
	}
}

// packageManagersByID maps distro IDs to their supported package managers.
// Only family roots need entries — derivatives match via ID_LIKE chain.
var packageManagersByID = map[string][]PackageManager{
	"debian": {PackageManagerAPT},
	"fedora": {PackageManagerDNF},
	"rhel":   {PackageManagerDNF},
	"suse":   {PackageManagerZypper},
	"arch":   {PackageManagerPacman},
	"alpine": {PackageManagerAPK},
}

// VersionFunc returns the version string for a package manager.
type VersionFunc func(ctx context.Context) (string, error)

// Validator validates SystemInstaller resources against the current system.
type Validator struct {
	distro       *DistroInfo
	versionFuncs map[PackageManager]VersionFunc
}

// NewValidator creates a Validator for the given distribution.
// Call DetectDistro() first to obtain the DistroInfo.
func NewValidator(distro *DistroInfo, versionFuncs map[PackageManager]VersionFunc) *Validator {
	return &Validator{
		distro:       distro,
		versionFuncs: versionFuncs,
	}
}

// Validate checks that the declared installer is available on this system
// and returns the installer state with version info.
func (v *Validator) Validate(ctx context.Context, installer *resource.SystemInstaller) (*resource.SystemInstallerState, error) {
	name := PackageManager(installer.Name())

	if !allPackageManagers[name] {
		return nil, fmt.Errorf("unknown package manager %q", name)
	}

	if !v.isSupported(name) {
		return nil, fmt.Errorf("package manager %q is not supported on this system (ID=%s, ID_LIKE=%v)", name, v.distro.ID, v.distro.IDLike)
	}

	versionFunc, ok := v.versionFuncs[name]
	if !ok {
		return nil, fmt.Errorf("no version function registered for package manager %q", name)
	}

	version, err := versionFunc(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get version for %q: %w", name, err)
	}

	return &resource.SystemInstallerState{
		Version:   version,
		UpdatedAt: time.Now(),
	}, nil
}

// SupportedInstallers returns the package managers supported on this system.
func (v *Validator) SupportedInstallers() []PackageManager {
	seen := make(map[PackageManager]bool)
	var result []PackageManager

	for _, id := range v.distro.IDs() {
		for _, pm := range packageManagersByID[id] {
			if !seen[pm] {
				seen[pm] = true
				result = append(result, pm)
			}
		}
	}

	return result
}

// isSupported checks if the package manager is supported on the detected distro.
func (v *Validator) isSupported(pm PackageManager) bool {
	for _, id := range v.distro.IDs() {
		if slices.Contains(packageManagersByID[id], pm) {
			return true
		}
	}
	return false
}
