package repository

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	gogit "github.com/terassyi/tomei/internal/git"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/resource"
)

// commandRunner is the interface for executing shell commands.
// This enables testing with mocks instead of real command execution.
type commandRunner interface {
	Execute(ctx context.Context, cmds []string, vars command.Vars) error
	ExecuteWithEnv(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) error
	Check(ctx context.Context, cmds []string, vars command.Vars, env map[string]string) bool
}

// gitRunner is the interface for git operations.
// This enables testing with mocks instead of real git execution.
type gitRunner interface {
	Clone(ctx context.Context, url, localPath string) error
	Pull(ctx context.Context, localPath string) error
	Exists(localPath string) bool
}

// goGitRunner implements gitRunner using the internal/git package (go-git).
type goGitRunner struct{}

func (g *goGitRunner) Clone(ctx context.Context, url, localPath string) error {
	return gogit.CloneURL(ctx, url, localPath, &gogit.CloneOptions{Depth: 1})
}

func (g *goGitRunner) Pull(ctx context.Context, localPath string) error {
	return gogit.PullPath(ctx, localPath)
}

func (g *goGitRunner) Exists(localPath string) bool {
	return gogit.Exists(localPath)
}

// Installer installs/manages installer repositories.
type Installer struct {
	cmdRunner    commandRunner
	gitRunner    gitRunner
	reposDir     string            // base directory for git-cloned repos
	toolBinPaths map[string]string // installer name → tool bin directory (e.g., "helm" → "~/.local/bin")
}

// NewInstaller creates a new repository Installer.
func NewInstaller(reposDir string) *Installer {
	return &Installer{
		cmdRunner: command.NewExecutor(""),
		gitRunner: &goGitRunner{},
		reposDir:  reposDir,
	}
}

// newInstallerWithRunners creates a new repository Installer with custom runners (for testing).
func newInstallerWithRunners(reposDir string, cmdRunner commandRunner, gitRunner gitRunner) *Installer {
	return &Installer{
		cmdRunner: cmdRunner,
		gitRunner: gitRunner,
		reposDir:  reposDir,
	}
}

// SetToolBinPaths sets the mapping from installer name to tool bin directory.
// This is used to add the tool's bin directory to PATH when executing delegation commands.
func (i *Installer) SetToolBinPaths(paths map[string]string) {
	i.toolBinPaths = paths
}

// buildEnvWithToolPath builds an environment map with the tool's bin directory prepended to PATH.
func (i *Installer) buildEnvWithToolPath(installerRef string) map[string]string {
	if i.toolBinPaths == nil {
		return nil
	}
	binDir, ok := i.toolBinPaths[installerRef]
	if !ok || binDir == "" {
		return nil
	}
	currentPath := os.Getenv("PATH")
	return map[string]string{
		"PATH": binDir + string(os.PathListSeparator) + currentPath,
	}
}

// Install sets up an installer repository and returns its state.
func (i *Installer) Install(ctx context.Context, res *resource.InstallerRepository, name string) (*resource.InstallerRepositoryState, error) {
	spec := res.InstallerRepositorySpec

	slog.Debug("installing installer repository", "name", name, "type", spec.Source.Type)

	switch spec.Source.Type {
	case resource.InstallerRepositorySourceDelegation:
		return i.installDelegation(ctx, spec, name)
	case resource.InstallerRepositorySourceGit:
		return i.installGit(ctx, spec, name)
	default:
		return nil, fmt.Errorf("unsupported source type: %s", spec.Source.Type)
	}
}

// Remove removes an installer repository.
func (i *Installer) Remove(ctx context.Context, st *resource.InstallerRepositoryState, name string) error {
	slog.Debug("removing installer repository", "name", name, "type", st.SourceType)

	switch st.SourceType {
	case resource.InstallerRepositorySourceDelegation:
		return i.removeDelegation(ctx, st, name)
	case resource.InstallerRepositorySourceGit:
		return i.removeGit(st, name)
	default:
		return fmt.Errorf("unsupported source type: %s", st.SourceType)
	}
}

func (i *Installer) installDelegation(ctx context.Context, spec *resource.InstallerRepositorySpec, name string) (*resource.InstallerRepositoryState, error) {
	commands := spec.Source.Commands
	env := i.buildEnvWithToolPath(spec.InstallerRef)

	// Check if already installed
	if len(commands.Check) > 0 {
		vars := command.Vars{Name: name}
		if i.cmdRunner.Check(ctx, commands.Check, vars, env) {
			slog.Debug("installer repository already configured", "name", name)
			return i.buildDelegationState(spec), nil
		}
	}

	// Execute install command
	vars := command.Vars{Name: name}
	if err := i.cmdRunner.ExecuteWithEnv(ctx, commands.Install, vars, env); err != nil {
		return nil, fmt.Errorf("failed to install repository %s: %w", name, err)
	}

	slog.Debug("installer repository configured", "name", name)
	return i.buildDelegationState(spec), nil
}

func (i *Installer) installGit(ctx context.Context, spec *resource.InstallerRepositorySpec, name string) (*resource.InstallerRepositoryState, error) {
	localPath := filepath.Join(i.reposDir, spec.InstallerRef, name)

	// Check if already cloned
	if i.gitRunner.Exists(localPath) {
		// Pull latest
		if err := i.gitRunner.Pull(ctx, localPath); err != nil {
			slog.Warn("git pull failed, continuing with existing", "name", name, "error", err)
		}
		return i.buildGitState(spec, localPath), nil
	}

	// Clone
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}
	if err := i.gitRunner.Clone(ctx, spec.Source.URL, localPath); err != nil {
		return nil, fmt.Errorf("failed to clone repository: %w", err)
	}

	slog.Debug("installer repository cloned", "name", name, "path", localPath)
	return i.buildGitState(spec, localPath), nil
}

func (i *Installer) removeDelegation(ctx context.Context, st *resource.InstallerRepositoryState, name string) error {
	if len(st.RemoveCommand) == 0 {
		slog.Warn("no remove command for repository, skipping", "name", name)
		return nil
	}
	env := i.buildEnvWithToolPath(st.InstallerRef)
	vars := command.Vars{Name: name}
	return i.cmdRunner.ExecuteWithEnv(ctx, st.RemoveCommand, vars, env)
}

func (i *Installer) removeGit(st *resource.InstallerRepositoryState, name string) error {
	if st.LocalPath == "" {
		return nil
	}
	if err := os.RemoveAll(st.LocalPath); err != nil {
		return fmt.Errorf("failed to remove repository directory: %w", err)
	}
	slog.Debug("installer repository removed", "name", name, "path", st.LocalPath)

	// Try to remove empty parent directories (installerRef dir, then reposDir)
	parentDir := filepath.Dir(st.LocalPath)
	_ = os.Remove(parentDir)
	_ = os.Remove(filepath.Dir(parentDir))

	return nil
}

func (i *Installer) buildDelegationState(spec *resource.InstallerRepositorySpec) *resource.InstallerRepositoryState {
	var removeCmd []string
	if spec.Source.Commands != nil {
		removeCmd = spec.Source.Commands.Remove
	}
	return &resource.InstallerRepositoryState{
		InstallerRef:  spec.InstallerRef,
		SourceType:    resource.InstallerRepositorySourceDelegation,
		URL:           spec.Source.URL,
		RemoveCommand: removeCmd,
		UpdatedAt:     time.Now(),
	}
}

func (i *Installer) buildGitState(spec *resource.InstallerRepositorySpec, localPath string) *resource.InstallerRepositoryState {
	return &resource.InstallerRepositoryState{
		InstallerRef: spec.InstallerRef,
		SourceType:   resource.InstallerRepositorySourceGit,
		URL:          spec.Source.URL,
		LocalPath:    localPath,
		UpdatedAt:    time.Now(),
	}
}
