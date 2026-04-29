package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/terassyi/tomei/internal/installer/apt"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
	"github.com/terassyi/tomei/internal/system"
)

// validatorInstallerAdapter wraps system.Validator as executor.Installer
// for SystemInstaller resources. Install validates the package manager;
// Remove is a no-op that allows stale state entries to be cleaned up;
// OS package managers themselves are not removed.
type validatorInstallerAdapter struct {
	validator *system.Validator
}

func (a *validatorInstallerAdapter) Install(ctx context.Context, res *resource.SystemInstaller, _ string) (*resource.SystemInstallerState, error) {
	return a.validator.Validate(ctx, res)
}

func (a *validatorInstallerAdapter) Remove(_ context.Context, _ *resource.SystemInstallerState, _ string) error {
	// System package managers are OS-managed and cannot actually be removed.
	// Returning nil allows the executor to delete the stale state entry when
	// a SystemInstaller is dropped from the manifest.
	return nil
}

// skipRepoInstaller is a placeholder for SystemPackageRepository operations.
// Concrete implementations (e.g., APT add-apt-repository) are tracked as a future issue.
type skipRepoInstaller struct{}

func (*skipRepoInstaller) Install(_ context.Context, _ *resource.SystemPackageRepository, name string) (*resource.SystemPackageRepositoryState, error) {
	return nil, fmt.Errorf("system package repository %q: repository management is not yet implemented", name)
}

func (*skipRepoInstaller) Remove(_ context.Context, _ *resource.SystemPackageRepositoryState, name string) error {
	return fmt.Errorf("system package repository %q: repository management is not yet implemented", name)
}

// skipPackageInstaller is a placeholder for SystemPackageSet operations.
// Concrete implementations (e.g., APT apt-get install) are tracked as a future issue.
type skipPackageInstaller struct{}

func (*skipPackageInstaller) Install(_ context.Context, _ *resource.SystemPackageSet, name string) (*resource.SystemPackageSetState, error) {
	return nil, fmt.Errorf("system package set %q: package management is not yet implemented", name)
}

func (*skipPackageInstaller) Remove(_ context.Context, _ *resource.SystemPackageSetState, name string) error {
	return fmt.Errorf("system package set %q: package management is not yet implemented", name)
}

// filterSupportedSystemResources returns resources that have concrete installer
// implementations (currently only SystemInstaller). Unsupported resources
// (SystemPackageRepository, SystemPackageSet) are returned separately so the
// caller can warn and skip them.
func filterSupportedSystemResources(resources []resource.Resource) (supported, skipped []resource.Resource) {
	for _, res := range resources {
		switch res.Kind() {
		case resource.KindSystemInstaller:
			supported = append(supported, res)
		default:
			skipped = append(skipped, res)
		}
	}
	return supported, skipped
}

// createSystemEngine builds a SystemEngine with the available concrete
// installers for the detected distribution. It auto-creates the system
// state directory if it does not exist.
func createSystemEngine(systemDataDir string) (*engine.SystemEngine, error) {
	store, err := state.NewStore[state.SystemState](systemDataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create system state store: %w", err)
	}

	distro, err := system.DetectDistro()
	if err != nil {
		return nil, fmt.Errorf("failed to detect Linux distribution: %w", err)
	}
	slog.Debug("detected distribution", "id", distro.ID, "id_like", distro.IDLike)

	runner := command.NewExecutor("")
	versionFuncs := map[system.PackageManager]system.VersionFunc{
		system.PackageManagerAPT: apt.VersionFunc(runner),
	}

	validator, err := system.NewValidator(distro, versionFuncs)
	if err != nil {
		return nil, fmt.Errorf("failed to create system validator: %w", err)
	}

	eng := engine.NewSystemEngine(
		&validatorInstallerAdapter{validator: validator},
		&skipRepoInstaller{},
		&skipPackageInstaller{},
		store,
	)
	return eng, nil
}
