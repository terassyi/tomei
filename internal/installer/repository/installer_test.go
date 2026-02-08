package repository

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/terassyi/tomei/internal/installer/command"
	"github.com/terassyi/tomei/internal/resource"
)

// --- mock implementations ---

type cmdCall struct {
	cmdStr string
	vars   command.Vars
	env    map[string]string
}

type mockCommandRunner struct {
	executeErr   error
	checkResult  bool
	executeCalls []cmdCall
	checkCalls   []cmdCall
}

func (m *mockCommandRunner) Execute(_ context.Context, cmdStr string, vars command.Vars) error {
	m.executeCalls = append(m.executeCalls, cmdCall{cmdStr: cmdStr, vars: vars})
	return m.executeErr
}

func (m *mockCommandRunner) ExecuteWithEnv(_ context.Context, cmdStr string, vars command.Vars, env map[string]string) error {
	m.executeCalls = append(m.executeCalls, cmdCall{cmdStr: cmdStr, vars: vars, env: env})
	return m.executeErr
}

func (m *mockCommandRunner) Check(_ context.Context, cmdStr string, vars command.Vars, env map[string]string) bool {
	m.checkCalls = append(m.checkCalls, cmdCall{cmdStr: cmdStr, vars: vars, env: env})
	return m.checkResult
}

type gitCall struct {
	url       string
	localPath string
}

type mockGitRunner struct {
	cloneErr     error
	pullErr      error
	existsResult bool
	cloneFn      func(url, localPath string) error // optional custom behavior
	cloneCalls   []gitCall
	pullCalls    []string
}

func (m *mockGitRunner) Clone(_ context.Context, url, localPath string) error {
	m.cloneCalls = append(m.cloneCalls, gitCall{url: url, localPath: localPath})
	if m.cloneFn != nil {
		return m.cloneFn(url, localPath)
	}
	return m.cloneErr
}

func (m *mockGitRunner) Pull(_ context.Context, localPath string) error {
	m.pullCalls = append(m.pullCalls, localPath)
	return m.pullErr
}

func (m *mockGitRunner) Exists(_ string) bool {
	return m.existsResult
}

// --- delegation tests ---

func TestInstaller_Install_Delegation(t *testing.T) {
	tests := []struct {
		name        string
		checkResult bool
		hasCheck    bool
		executeErr  error
		wantErr     bool
		wantInstall bool // install command should be called
		wantCheck   bool // check command should be called
	}{
		{
			name:        "fresh install",
			checkResult: false,
			hasCheck:    true,
			wantInstall: true,
			wantCheck:   true,
		},
		{
			name:        "already installed (check succeeds)",
			checkResult: true,
			hasCheck:    true,
			wantInstall: false,
			wantCheck:   true,
		},
		{
			name:        "no check command - always installs",
			hasCheck:    false,
			wantInstall: true,
			wantCheck:   false,
		},
		{
			name:        "install command fails",
			checkResult: false,
			hasCheck:    true,
			executeErr:  fmt.Errorf("command failed"),
			wantErr:     true,
			wantInstall: true,
			wantCheck:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &mockCommandRunner{
				checkResult: tt.checkResult,
				executeErr:  tt.executeErr,
			}
			git := &mockGitRunner{}
			inst := newInstallerWithRunners(t.TempDir(), cmd, git)

			commands := &resource.CommandSet{
				Install: "helm repo add {{.Name}} https://example.com",
				Remove:  "helm repo remove {{.Name}}",
			}
			if tt.hasCheck {
				commands.Check = "helm repo list | grep {{.Name}}"
			}

			repo := &resource.InstallerRepository{
				BaseResource: resource.BaseResource{
					Metadata: resource.Metadata{Name: "bitnami"},
				},
				InstallerRepositorySpec: &resource.InstallerRepositorySpec{
					InstallerRef: "helm",
					Source: resource.InstallerRepositorySourceSpec{
						Type:     resource.InstallerRepositorySourceDelegation,
						URL:      "https://charts.bitnami.com/bitnami",
						Commands: commands,
					},
				},
			}

			ctx := context.Background()
			state, err := inst.Install(ctx, repo, "bitnami")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			assert.Equal(t, "helm", state.InstallerRef)
			assert.Equal(t, resource.InstallerRepositorySourceDelegation, state.SourceType)
			assert.Equal(t, "https://charts.bitnami.com/bitnami", state.URL)
			assert.Equal(t, "helm repo remove {{.Name}}", state.RemoveCommand)
			assert.False(t, state.UpdatedAt.IsZero())

			if tt.wantCheck {
				require.Len(t, cmd.checkCalls, 1)
				assert.Equal(t, "bitnami", cmd.checkCalls[0].vars.Name)
			} else {
				assert.Empty(t, cmd.checkCalls)
			}

			if tt.wantInstall {
				require.Len(t, cmd.executeCalls, 1)
				assert.Equal(t, "helm repo add {{.Name}} https://example.com", cmd.executeCalls[0].cmdStr)
				assert.Equal(t, "bitnami", cmd.executeCalls[0].vars.Name)
			} else {
				assert.Empty(t, cmd.executeCalls)
			}

			// Git runner should never be called for delegation
			assert.Empty(t, git.cloneCalls)
			assert.Empty(t, git.pullCalls)
		})
	}
}

func TestInstaller_Remove_Delegation(t *testing.T) {
	tests := []struct {
		name        string
		removeCmd   string
		executeErr  error
		wantErr     bool
		wantExecute bool
	}{
		{
			name:        "successful remove",
			removeCmd:   "helm repo remove {{.Name}}",
			wantExecute: true,
		},
		{
			name:        "no remove command - skips silently",
			removeCmd:   "",
			wantExecute: false,
		},
		{
			name:        "remove command fails",
			removeCmd:   "helm repo remove {{.Name}}",
			executeErr:  fmt.Errorf("command failed"),
			wantErr:     true,
			wantExecute: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &mockCommandRunner{executeErr: tt.executeErr}
			git := &mockGitRunner{}
			inst := newInstallerWithRunners(t.TempDir(), cmd, git)

			st := &resource.InstallerRepositoryState{
				InstallerRef:  "helm",
				SourceType:    resource.InstallerRepositorySourceDelegation,
				RemoveCommand: tt.removeCmd,
			}

			err := inst.Remove(context.Background(), st, "bitnami")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if tt.wantExecute {
				require.Len(t, cmd.executeCalls, 1)
				assert.Equal(t, tt.removeCmd, cmd.executeCalls[0].cmdStr)
				assert.Equal(t, "bitnami", cmd.executeCalls[0].vars.Name)
			} else {
				assert.Empty(t, cmd.executeCalls)
			}
		})
	}
}

// --- git tests ---

func TestInstaller_Install_Git(t *testing.T) {
	t.Run("fresh clone", func(t *testing.T) {
		dir := t.TempDir()
		cmd := &mockCommandRunner{}
		git := &mockGitRunner{}
		inst := newInstallerWithRunners(dir, cmd, git)

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "custom-registry"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "aqua",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceGit,
					URL:  "https://github.com/example/registry.git",
				},
			},
		}

		state, err := inst.Install(context.Background(), repo, "custom-registry")
		require.NoError(t, err)

		assert.Equal(t, "aqua", state.InstallerRef)
		assert.Equal(t, resource.InstallerRepositorySourceGit, state.SourceType)
		assert.Equal(t, "https://github.com/example/registry.git", state.URL)
		assert.Equal(t, filepath.Join(dir, "aqua", "custom-registry"), state.LocalPath)
		assert.False(t, state.UpdatedAt.IsZero())

		require.Len(t, git.cloneCalls, 1)
		assert.Equal(t, "https://github.com/example/registry.git", git.cloneCalls[0].url)
		assert.Empty(t, git.pullCalls)
		assert.Empty(t, cmd.executeCalls)
	})

	t.Run("already cloned - pulls", func(t *testing.T) {
		dir := t.TempDir()
		localPath := filepath.Join(dir, "aqua", "custom-registry")

		cmd := &mockCommandRunner{}
		git := &mockGitRunner{existsResult: true}
		inst := newInstallerWithRunners(dir, cmd, git)

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "custom-registry"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "aqua",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceGit,
					URL:  "https://github.com/example/registry.git",
				},
			},
		}

		state, err := inst.Install(context.Background(), repo, "custom-registry")
		require.NoError(t, err)

		assert.Equal(t, localPath, state.LocalPath)
		require.Len(t, git.pullCalls, 1)
		assert.Equal(t, localPath, git.pullCalls[0])
		assert.Empty(t, git.cloneCalls)
	})

	t.Run("pull fails - continues with existing", func(t *testing.T) {
		dir := t.TempDir()
		localPath := filepath.Join(dir, "aqua", "custom-registry")

		cmd := &mockCommandRunner{}
		git := &mockGitRunner{existsResult: true, pullErr: fmt.Errorf("network error")}
		inst := newInstallerWithRunners(dir, cmd, git)

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "custom-registry"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "aqua",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceGit,
					URL:  "https://github.com/example/registry.git",
				},
			},
		}

		// Should succeed even though pull fails
		state, err := inst.Install(context.Background(), repo, "custom-registry")
		require.NoError(t, err)
		assert.Equal(t, localPath, state.LocalPath)
	})

	t.Run("clone fails", func(t *testing.T) {
		dir := t.TempDir()
		cmd := &mockCommandRunner{}
		git := &mockGitRunner{cloneErr: fmt.Errorf("clone failed: repository not found")}
		inst := newInstallerWithRunners(dir, cmd, git)

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "bad-registry"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "aqua",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceGit,
					URL:  "https://github.com/example/nonexistent.git",
				},
			},
		}

		_, err := inst.Install(context.Background(), repo, "bad-registry")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to clone repository")

		require.Len(t, git.cloneCalls, 1)
	})
}

func TestInstaller_Remove_Git(t *testing.T) {
	tests := []struct {
		name      string
		localPath string
		setup     func(t *testing.T, path string)
		wantErr   bool
	}{
		{
			name:      "successful remove",
			localPath: "to-be-set", // set in test
			setup: func(t *testing.T, path string) {
				require.NoError(t, os.MkdirAll(path, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(path, "test"), []byte("data"), 0644))
			},
		},
		{
			name:      "empty local path - skips silently",
			localPath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			localPath := tt.localPath
			if localPath == "to-be-set" {
				localPath = filepath.Join(dir, "repo-to-remove")
			}

			if tt.setup != nil {
				tt.setup(t, localPath)
			}

			cmd := &mockCommandRunner{}
			git := &mockGitRunner{}
			inst := newInstallerWithRunners(dir, cmd, git)

			st := &resource.InstallerRepositoryState{
				InstallerRef: "aqua",
				SourceType:   resource.InstallerRepositorySourceGit,
				LocalPath:    localPath,
			}

			err := inst.Remove(context.Background(), st, "custom-registry")

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			if localPath != "" {
				_, err := os.Stat(localPath)
				assert.True(t, os.IsNotExist(err))
			}
		})
	}
}

// --- unsupported source type ---

func TestInstaller_UnsupportedSourceType(t *testing.T) {
	cmd := &mockCommandRunner{}
	git := &mockGitRunner{}
	inst := newInstallerWithRunners(t.TempDir(), cmd, git)

	t.Run("install", func(t *testing.T) {
		repo := &resource.InstallerRepository{
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				Source: resource.InstallerRepositorySourceSpec{
					Type: "unknown",
				},
			},
		}
		_, err := inst.Install(context.Background(), repo, "test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported source type")
	})

	t.Run("remove", func(t *testing.T) {
		st := &resource.InstallerRepositoryState{
			SourceType: "unknown",
		}
		err := inst.Remove(context.Background(), st, "test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported source type")
	})
}
