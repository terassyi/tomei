package apt

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/resource"
)

// InstallerInstaller handles the SystemInstaller resource for APT.
// Install checks APT availability via "apt-get --version" and records the version in state.
// Remove is a no-op since APT is a system package manager and is never removed by tomei.
type InstallerInstaller struct {
	runner commandRunner
}

// NewInstallerInstaller creates a new InstallerInstaller with the default command executor.
func NewInstallerInstaller() *InstallerInstaller {
	return &InstallerInstaller{
		runner: command.NewExecutor(""),
	}
}

// newInstallerInstallerWith creates a new InstallerInstaller with a custom runner (for testing).
func newInstallerInstallerWith(runner commandRunner) *InstallerInstaller {
	return &InstallerInstaller{
		runner: runner,
	}
}

// Install checks APT availability by running "apt-get --version" and records the version in state.
func (i *InstallerInstaller) Install(ctx context.Context, _ *resource.SystemInstaller, name string) (*resource.SystemInstallerState, error) {
	slog.Debug("checking apt availability", "name", name)

	output, err := i.runner.ExecuteCapture(ctx, []string{"apt-get --version"}, command.Vars{}, nil)
	if err != nil {
		return nil, fmt.Errorf("apt-get not found: this installer requires a Debian-based system: %w", err)
	}

	version, err := parseAptVersion(output)
	if err != nil {
		return nil, fmt.Errorf("failed to parse apt-get version: %w", err)
	}

	slog.Debug("apt detected", "name", name, "version", version)

	return &resource.SystemInstallerState{
		Version:   version,
		UpdatedAt: time.Now(),
	}, nil
}

// Remove is a no-op. APT is a system package manager and is never removed by tomei.
func (i *InstallerInstaller) Remove(_ context.Context, _ *resource.SystemInstallerState, _ string) error {
	return nil
}
