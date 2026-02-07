package git

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

// Repository represents a git repository with its metadata.
type Repository struct {
	// Owner is the repository owner (e.g., "aquaproj" for github.com/aquaproj/aqua-registry)
	Owner string
	// Name is the repository name (e.g., "aqua-registry")
	Name string
	// Host is the git host (default: "github.com")
	Host string
}

// NewRepository creates a new Repository with the given owner and name.
// Host defaults to "github.com".
func NewRepository(owner, name string) *Repository {
	return &Repository{
		Owner: owner,
		Name:  name,
		Host:  "github.com",
	}
}

// URL returns the HTTPS clone URL for the repository.
func (r *Repository) URL() string {
	host := r.Host
	if host == "" {
		host = "github.com"
	}
	return fmt.Sprintf("https://%s/%s/%s.git", host, r.Owner, r.Name)
}

// CloneOptions configures clone behavior.
type CloneOptions struct {
	// Branch to checkout (default: default branch)
	Branch string
	// Depth for shallow clone (0 = full clone)
	Depth int
}

// Clone clones the repository to destPath.
func (r *Repository) Clone(ctx context.Context, destPath string, opts *CloneOptions) error {
	return CloneURL(ctx, r.URL(), destPath, opts)
}

// Pull pulls latest changes for the repository at repoPath.
func (r *Repository) Pull(ctx context.Context, repoPath string) error {
	return PullPath(ctx, repoPath)
}

// CloneOrPull clones the repository if it doesn't exist at destPath, or pulls if it does.
func (r *Repository) CloneOrPull(ctx context.Context, destPath string, opts *CloneOptions) error {
	return CloneOrPullURL(ctx, r.URL(), destPath, opts)
}

// CloneURL clones a git repository from url to destPath.
func CloneURL(ctx context.Context, url, destPath string, opts *CloneOptions) error {
	slog.Debug("cloning repository", "url", url, "dest", destPath)

	cloneOpts := &git.CloneOptions{
		URL: url,
	}

	if opts != nil {
		if opts.Depth > 0 {
			cloneOpts.Depth = opts.Depth
			cloneOpts.SingleBranch = true
		}
		if opts.Branch != "" {
			cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(opts.Branch)
			cloneOpts.SingleBranch = true
		}
	}

	_, err := git.PlainCloneContext(ctx, destPath, false, cloneOpts)
	if err != nil {
		if errors.Is(err, git.ErrRepositoryAlreadyExists) {
			slog.Debug("repository already exists", "path", destPath)
			return fmt.Errorf("repository already exists at %s: %w", destPath, err)
		}
		return fmt.Errorf("failed to clone repository: %w", err)
	}

	slog.Debug("clone completed", "url", url, "path", destPath)
	return nil
}

// PullPath pulls latest changes for the repository at repoPath.
func PullPath(ctx context.Context, repoPath string) error {
	slog.Debug("pulling repository", "path", repoPath)

	repo, err := git.PlainOpen(repoPath)
	if err != nil {
		return fmt.Errorf("failed to open repository: %w", err)
	}

	w, err := repo.Worktree()
	if err != nil {
		return fmt.Errorf("failed to get worktree: %w", err)
	}

	err = w.PullContext(ctx, &git.PullOptions{})
	if err != nil {
		if errors.Is(err, git.NoErrAlreadyUpToDate) {
			slog.Debug("repository already up-to-date", "path", repoPath)
			return nil
		}
		return fmt.Errorf("failed to pull: %w", err)
	}

	slog.Debug("pull completed", "path", repoPath)
	return nil
}

// CloneOrPullURL clones a git repository if it doesn't exist at destPath, or pulls if it does.
func CloneOrPullURL(ctx context.Context, url, destPath string, opts *CloneOptions) error {
	if Exists(destPath) {
		return PullPath(ctx, destPath)
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	return CloneURL(ctx, url, destPath, opts)
}

// Exists checks if a git repository exists at the given path.
func Exists(path string) bool {
	_, err := git.PlainOpen(path)
	return err == nil
}
