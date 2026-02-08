//go:build integration

package tests

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	repository "github.com/terassyi/tomei/internal/installer/repository"
	"github.com/terassyi/tomei/internal/resource"
)

// TestInstallerRepository_Delegation tests the real repository Installer
// with the delegation pattern using shell commands.
func TestInstallerRepository_Delegation(t *testing.T) {
	t.Run("install and verify state", func(t *testing.T) {
		tmpDir := t.TempDir()
		markerFile := filepath.Join(tmpDir, "repo-installed")

		inst := repository.NewInstaller(filepath.Join(tmpDir, "repos"))

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "test-repo"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "helm",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceDelegation,
					URL:  "https://example.com/charts",
					Commands: &resource.CommandSet{
						Install: "touch " + markerFile,
						Check:   "test -f " + markerFile,
						Remove:  "rm " + markerFile,
					},
				},
			},
		}

		ctx := context.Background()
		state, err := inst.Install(ctx, repo, "test-repo")
		require.NoError(t, err)

		assert.Equal(t, "helm", state.InstallerRef)
		assert.Equal(t, resource.InstallerRepositorySourceDelegation, state.SourceType)
		assert.Equal(t, "https://example.com/charts", state.URL)
		assert.Equal(t, "rm "+markerFile, state.RemoveCommand)
		assert.False(t, state.UpdatedAt.IsZero())
		assert.FileExists(t, markerFile)
	})

	t.Run("idempotent - check skips install", func(t *testing.T) {
		tmpDir := t.TempDir()
		markerFile := filepath.Join(tmpDir, "repo-installed")
		counterFile := filepath.Join(tmpDir, "install-count")

		// Pre-create marker so check succeeds
		require.NoError(t, os.WriteFile(markerFile, []byte("ok"), 0644))

		inst := repository.NewInstaller(filepath.Join(tmpDir, "repos"))

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "test-repo"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "helm",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceDelegation,
					Commands: &resource.CommandSet{
						// Install appends to counter file - if called, file will exist
						Install: "echo x >> " + counterFile,
						Check:   "test -f " + markerFile,
					},
				},
			},
		}

		ctx := context.Background()
		_, err := inst.Install(ctx, repo, "test-repo")
		require.NoError(t, err)

		// Counter file should NOT exist because install was skipped
		_, err = os.Stat(counterFile)
		assert.True(t, os.IsNotExist(err), "install command should not have been called")
	})

	t.Run("install then remove lifecycle", func(t *testing.T) {
		tmpDir := t.TempDir()
		markerFile := filepath.Join(tmpDir, "repo-installed")

		inst := repository.NewInstaller(filepath.Join(tmpDir, "repos"))

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "test-repo"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "helm",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceDelegation,
					Commands: &resource.CommandSet{
						Install: "touch " + markerFile,
						Check:   "test -f " + markerFile,
						Remove:  "rm " + markerFile,
					},
				},
			},
		}

		ctx := context.Background()

		// Install
		state, err := inst.Install(ctx, repo, "test-repo")
		require.NoError(t, err)
		assert.FileExists(t, markerFile)

		// Remove
		err = inst.Remove(ctx, state, "test-repo")
		require.NoError(t, err)

		_, err = os.Stat(markerFile)
		assert.True(t, os.IsNotExist(err), "marker file should be removed")
	})

	t.Run("install command fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		inst := repository.NewInstaller(filepath.Join(tmpDir, "repos"))

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "test-repo"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "helm",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceDelegation,
					Commands: &resource.CommandSet{
						Install: "exit 1",
					},
				},
			},
		}

		_, err := inst.Install(context.Background(), repo, "test-repo")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to install repository")
	})
}

// TestInstallerRepository_Git tests the real repository Installer
// with the git source type using a local bare repository.
func TestInstallerRepository_Git(t *testing.T) {
	// Check git is available
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	t.Run("clone and verify state", func(t *testing.T) {
		tmpDir := t.TempDir()
		bareRepo := createBareGitRepo(t, tmpDir)
		reposDir := filepath.Join(tmpDir, "repos")

		inst := repository.NewInstaller(reposDir)

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "custom-registry"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "aqua",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceGit,
					URL:  bareRepo,
				},
			},
		}

		ctx := context.Background()
		state, err := inst.Install(ctx, repo, "custom-registry")
		require.NoError(t, err)

		assert.Equal(t, "aqua", state.InstallerRef)
		assert.Equal(t, resource.InstallerRepositorySourceGit, state.SourceType)
		assert.Equal(t, bareRepo, state.URL)
		assert.Equal(t, filepath.Join(reposDir, "aqua", "custom-registry"), state.LocalPath)
		assert.False(t, state.UpdatedAt.IsZero())

		// Verify cloned content
		assert.FileExists(t, filepath.Join(state.LocalPath, "registry.yaml"))
	})

	t.Run("idempotent - second install pulls", func(t *testing.T) {
		tmpDir := t.TempDir()
		bareRepo := createBareGitRepo(t, tmpDir)
		reposDir := filepath.Join(tmpDir, "repos")

		inst := repository.NewInstaller(reposDir)

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "custom-registry"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "aqua",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceGit,
					URL:  bareRepo,
				},
			},
		}

		ctx := context.Background()

		// First install (clone)
		state1, err := inst.Install(ctx, repo, "custom-registry")
		require.NoError(t, err)

		// Second install (pull)
		state2, err := inst.Install(ctx, repo, "custom-registry")
		require.NoError(t, err)

		assert.Equal(t, state1.LocalPath, state2.LocalPath)
		assert.FileExists(t, filepath.Join(state2.LocalPath, "registry.yaml"))
	})

	t.Run("clone then remove lifecycle", func(t *testing.T) {
		tmpDir := t.TempDir()
		bareRepo := createBareGitRepo(t, tmpDir)
		reposDir := filepath.Join(tmpDir, "repos")

		inst := repository.NewInstaller(reposDir)

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "custom-registry"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "aqua",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceGit,
					URL:  bareRepo,
				},
			},
		}

		ctx := context.Background()

		// Install
		state, err := inst.Install(ctx, repo, "custom-registry")
		require.NoError(t, err)
		assert.DirExists(t, state.LocalPath)

		// Remove
		err = inst.Remove(ctx, state, "custom-registry")
		require.NoError(t, err)
		assert.NoDirExists(t, state.LocalPath)
	})

	t.Run("clone fails with bad URL", func(t *testing.T) {
		tmpDir := t.TempDir()
		inst := repository.NewInstaller(filepath.Join(tmpDir, "repos"))

		repo := &resource.InstallerRepository{
			BaseResource: resource.BaseResource{
				Metadata: resource.Metadata{Name: "bad-registry"},
			},
			InstallerRepositorySpec: &resource.InstallerRepositorySpec{
				InstallerRef: "aqua",
				Source: resource.InstallerRepositorySourceSpec{
					Type: resource.InstallerRepositorySourceGit,
					URL:  filepath.Join(tmpDir, "nonexistent-repo"),
				},
			},
		}

		_, err := inst.Install(context.Background(), repo, "bad-registry")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to clone repository")
	})
}

// createBareGitRepo creates a local bare git repo with a single commit for testing.
func createBareGitRepo(t *testing.T, baseDir string) string {
	t.Helper()

	bareRepo := filepath.Join(baseDir, "bare-repo.git")
	require.NoError(t, os.MkdirAll(bareRepo, 0755))

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test",
			"GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test",
			"GIT_COMMITTER_EMAIL=test@test.com",
		)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v failed: %s", args, string(output))
	}

	// Init bare repo
	run(bareRepo, "init", "--bare")

	// Create working copy, add content, push
	workDir := filepath.Join(baseDir, "work")
	run(baseDir, "clone", bareRepo, workDir)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "registry.yaml"), []byte("packages: []"), 0644))
	run(workDir, "add", ".")
	run(workDir, "commit", "-m", "init")
	run(workDir, "push")

	return bareRepo
}
