package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRepository(t *testing.T) {
	repo := NewRepository("octocat", "Hello-World")

	assert.Equal(t, "octocat", repo.Owner)
	assert.Equal(t, "Hello-World", repo.Name)
	assert.Equal(t, "github.com", repo.Host)
}

func TestRepository_URL(t *testing.T) {
	t.Run("default host", func(t *testing.T) {
		repo := NewRepository("octocat", "Hello-World")
		assert.Equal(t, "https://github.com/octocat/Hello-World.git", repo.URL())
	})

	t.Run("custom host", func(t *testing.T) {
		repo := &Repository{
			Owner: "user",
			Name:  "repo",
			Host:  "gitlab.com",
		}
		assert.Equal(t, "https://gitlab.com/user/repo.git", repo.URL())
	})

	t.Run("empty host defaults to github.com", func(t *testing.T) {
		repo := &Repository{
			Owner: "user",
			Name:  "repo",
			Host:  "",
		}
		assert.Equal(t, "https://github.com/user/repo.git", repo.URL())
	})
}

func TestRepository_Clone(t *testing.T) {
	// Use a small, stable public repository for testing
	repo := NewRepository("octocat", "Hello-World")

	t.Run("clone repository", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "hello-world")

		err := repo.Clone(context.Background(), destPath, nil)
		require.NoError(t, err)

		// Verify .git directory exists
		gitDir := filepath.Join(destPath, ".git")
		assert.DirExists(t, gitDir)

		// Verify README exists
		readme := filepath.Join(destPath, "README")
		assert.FileExists(t, readme)
	})

	t.Run("clone with shallow depth", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "hello-world-shallow")

		err := repo.Clone(context.Background(), destPath, &CloneOptions{
			Depth: 1,
		})
		require.NoError(t, err)

		// Verify .git directory exists
		gitDir := filepath.Join(destPath, ".git")
		assert.DirExists(t, gitDir)
	})

	t.Run("clone with branch", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "hello-world-branch")

		err := repo.Clone(context.Background(), destPath, &CloneOptions{
			Branch: "master",
			Depth:  1,
		})
		require.NoError(t, err)

		// Verify .git directory exists
		gitDir := filepath.Join(destPath, ".git")
		assert.DirExists(t, gitDir)
	})

	t.Run("clone already exists error", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "hello-world")

		// First clone
		err := repo.Clone(context.Background(), destPath, &CloneOptions{Depth: 1})
		require.NoError(t, err)

		// Second clone should fail
		err = repo.Clone(context.Background(), destPath, &CloneOptions{Depth: 1})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("clone invalid repository", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "invalid")

		invalidRepo := NewRepository("invalid", "nonexistent-repo-12345")
		err := invalidRepo.Clone(context.Background(), destPath, nil)
		require.Error(t, err)
	})

	t.Run("clone context canceled", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "canceled")

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		err := repo.Clone(ctx, destPath, nil)
		require.Error(t, err)
	})
}

func TestRepository_Pull(t *testing.T) {
	repo := NewRepository("octocat", "Hello-World")

	t.Run("pull existing repository", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "hello-world")

		// Clone first
		err := repo.Clone(context.Background(), destPath, &CloneOptions{Depth: 1})
		require.NoError(t, err)

		// Pull should succeed (already up-to-date)
		err = repo.Pull(context.Background(), destPath)
		require.NoError(t, err)
	})

	t.Run("pull non-existent repository", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "nonexistent")

		err := repo.Pull(context.Background(), destPath)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open repository")
	})
}

func TestExists(t *testing.T) {
	repo := NewRepository("octocat", "Hello-World")

	t.Run("exists returns true for git repository", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "hello-world")

		err := repo.Clone(context.Background(), destPath, &CloneOptions{Depth: 1})
		require.NoError(t, err)

		assert.True(t, Exists(destPath))
	})

	t.Run("exists returns false for non-repository", func(t *testing.T) {
		tmpDir := t.TempDir()
		assert.False(t, Exists(tmpDir))
	})

	t.Run("exists returns false for non-existent path", func(t *testing.T) {
		assert.False(t, Exists("/nonexistent/path"))
	})
}

func TestRepository_CloneOrPull(t *testing.T) {
	repo := NewRepository("octocat", "Hello-World")

	t.Run("clone when not exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "hello-world")

		err := repo.CloneOrPull(context.Background(), destPath, &CloneOptions{Depth: 1})
		require.NoError(t, err)

		assert.True(t, Exists(destPath))
	})

	t.Run("pull when exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "hello-world")

		// First clone
		err := repo.CloneOrPull(context.Background(), destPath, &CloneOptions{Depth: 1})
		require.NoError(t, err)

		// Get modification time of a file
		readme := filepath.Join(destPath, "README")
		info1, err := os.Stat(readme)
		require.NoError(t, err)

		// Second call should pull (not clone)
		err = repo.CloneOrPull(context.Background(), destPath, &CloneOptions{Depth: 1})
		require.NoError(t, err)

		// File should still exist
		info2, err := os.Stat(readme)
		require.NoError(t, err)

		// Modification time should be the same (no changes)
		assert.Equal(t, info1.ModTime(), info2.ModTime())
	})

	t.Run("creates parent directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		destPath := filepath.Join(tmpDir, "nested", "dir", "hello-world")

		err := repo.CloneOrPull(context.Background(), destPath, &CloneOptions{Depth: 1})
		require.NoError(t, err)

		assert.True(t, Exists(destPath))
	})
}
