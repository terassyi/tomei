package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/terassyi/tomei/internal/installer/apt"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/installer/download"
	"github.com/terassyi/tomei/internal/installer/engine"
	"github.com/terassyi/tomei/internal/installer/executor"
	"github.com/terassyi/tomei/internal/resource"
	"github.com/terassyi/tomei/internal/state"
	"github.com/terassyi/tomei/internal/system"
)

// validatorInstaller adapts *system.Validator into the
// executor.Installer[*SystemInstaller, *SystemInstallerState] interface
// expected by the system engine. Install validates the host's package
// manager; Remove is a no-op so stale state entries can be cleaned up
// without trying to uninstall an OS-managed package manager.
type validatorInstaller struct {
	validator *system.Validator
}

func (a *validatorInstaller) Install(ctx context.Context, res *resource.SystemInstaller, _ string) (*resource.SystemInstallerState, error) {
	if a.validator == nil {
		return nil, errors.New("system: installer: package manager validation unavailable (distro detection failed or unsupported platform)")
	}
	return a.validator.Validate(ctx, res)
}

func (a *validatorInstaller) Remove(_ context.Context, _ *resource.SystemInstallerState, _ string) error {
	// System package managers are OS-managed and cannot actually be removed.
	// Returning nil allows the executor to delete the stale state entry when
	// a SystemInstaller is dropped from the manifest.
	return nil
}

// unsupportedHostRepoInstaller is the placeholder used when distro detection
// fails or the host lacks a supported package manager. Install surfaces a
// clear platform-availability error instead of letting raw apt/gpg exec
// failures escape. Remove returns nil so re-imaged hosts can drop stale
// state entries; a warn log captures the host limitation and the orphaned
// files so dotfile-sync users can audit the originating host.
type unsupportedHostRepoInstaller struct{}

func (*unsupportedHostRepoInstaller) Install(_ context.Context, _ *resource.SystemPackageRepository, name string) (*resource.SystemPackageRepositoryState, error) {
	// %q Go-quotes control chars in name, defanging log-injection via a
	// crafted manifest name.
	return nil, fmt.Errorf("system: repository %q: requires a supported Linux package manager (apt) on this host", name)
}

func (*unsupportedHostRepoInstaller) Remove(_ context.Context, st *resource.SystemPackageRepositoryState, name string) error {
	// Allow stale state cleanup on hosts that lost (or never had) apt support.
	// Warn so cross-host state sync (e.g., dotfile sync between a Linux box
	// and a macOS box) does not silently leave the actual /etc/apt files on
	// the host that originally installed the repository. Surface the
	// recorded InstalledFiles so a curious user can clean them up on the
	// originating host.
	var orphaned []string
	if st != nil {
		orphaned = st.InstalledFiles
	}
	slog.Warn("removing state entry for system package repository without touching actual files; this host cannot manage apt repositories",
		"name", name, "orphaned_files", orphaned)
	return nil
}

// skipPackageInstaller is a placeholder for SystemPackageSet operations.
// Concrete implementations (e.g., APT apt-get install) are tracked in #198.
type skipPackageInstaller struct{}

func (*skipPackageInstaller) Install(_ context.Context, _ *resource.SystemPackageSet, name string) (*resource.SystemPackageSetState, error) {
	return nil, fmt.Errorf("system: package %q: not yet implemented", name)
}

func (*skipPackageInstaller) Remove(_ context.Context, _ *resource.SystemPackageSetState, name string) error {
	return fmt.Errorf("system: package %q: not yet implemented", name)
}

// filterSupportedSystemResources splits system resources into those that have
// a concrete installer (SystemInstaller, SystemPackageRepository) and those
// that do not (currently SystemPackageSet, #198). Callers warn and skip the
// unsupported group.
func filterSupportedSystemResources(resources []resource.Resource) (supported, skipped []resource.Resource) {
	for _, res := range resources {
		switch res.Kind() {
		case resource.KindSystemInstaller, resource.KindSystemPackageRepository:
			supported = append(supported, res)
		default:
			skipped = append(skipped, res)
		}
	}
	return supported, skipped
}

// selectRepoInstaller returns the executor.Installer for SystemPackageRepository
// resources. The apt-backed installer is returned only when distro detection
// succeeded AND the detected distro family lists APT as a supported package
// manager. On any other host (macOS, minimal containers, or non-apt Linux
// distros such as Fedora/Arch/Alpine where DetectDistro succeeds but APT is
// not native) the placeholder is returned, whose Install fails with the
// documented "requires a supported Linux package manager (apt) on this host"
// error and whose Remove permits state cleanup with a warn log.
//
// Extracted as a pure helper so the conditional wiring can be unit-tested
// without depending on real distro detection. Defense-in-depth: the helper
// falls back to the placeholder when aptClient or downloader is nil, even
// though createSystemEngine's nil-downloader guard makes that path
// unreachable from production code.
func selectRepoInstaller(
	validator *system.Validator,
	aptClient *apt.Client,
	downloader download.Downloader,
) executor.Installer[*resource.SystemPackageRepository, *resource.SystemPackageRepositoryState] {
	if validator == nil || aptClient == nil || downloader == nil {
		return &unsupportedHostRepoInstaller{}
	}
	if !slices.Contains(validator.SupportedInstallers(), system.PackageManagerAPT) {
		return &unsupportedHostRepoInstaller{}
	}
	return aptClient.PackageRepositoryInstaller(downloader)
}

// createSystemEngine builds a SystemEngine wired with the concrete installers
// available on this host. The system state directory is created lazily by
// state.NewStore — createSystemEngine itself performs no filesystem writes.
// downloader must be non-nil; pass an un-authenticated downloader (no GitHub
// token wrapping) because vendor GPG keys do not require GitHub auth and
// attaching a PAT to manifest-supplied github.com URLs would be a token-leak
// risk.
func createSystemEngine(systemDataDir string, downloader download.Downloader) (*engine.SystemEngine, error) {
	if downloader == nil {
		return nil, errors.New("downloader is required")
	}

	store, err := state.NewStore[state.SystemState](systemDataDir)
	if err != nil {
		return nil, fmt.Errorf("system: state store: %w", err)
	}

	// aptClient is constructed unconditionally: command.NewExecutor is stateless
	// and the apt Client is the shared hub used by both the validator's
	// VersionFunc and the repository installer. On non-apt hosts the Client
	// exists but is never invoked — selectRepoInstaller returns the placeholder
	// when either (a) validator is nil because distro detection failed (macOS,
	// minimal containers) or (b) validator is non-nil but the detected distro
	// family does not list APT (Fedora/Arch/Alpine).
	aptClient := apt.New(command.NewExecutor(""))

	// Distro detection and validator creation are best-effort. When unavailable
	// (e.g., macOS, minimal containers), the engine can still run removals —
	// only Install actions will fail with a clear platform-availability error.
	// Each step uses if-init scoping to keep err local to its branch.
	var validator *system.Validator
	if distro, err := system.DetectDistro(); err != nil {
		slog.Warn("system distro detection unavailable; system installer validation will be skipped", "error", err)
	} else {
		slog.Debug("detected distribution", "id", distro.ID, "id_like", distro.IDLike)
		versionFuncs := map[system.PackageManager]system.VersionFunc{
			system.PackageManagerAPT: aptClient.VersionFunc(),
		}
		if v, err := system.NewValidator(distro, versionFuncs); err != nil {
			slog.Warn("failed to create system validator; system installer validation will be skipped", "error", err)
		} else {
			validator = v
		}
	}

	repoInstaller := selectRepoInstaller(validator, aptClient, downloader)

	eng := engine.NewSystemEngine(
		&validatorInstaller{validator: validator},
		repoInstaller,
		&skipPackageInstaller{},
		store,
	)
	return eng, nil
}
