package place

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withStubs swaps the symlinkOverrides fields and restores them via t.Cleanup.
// Pass nil to leave a field at its production default. Tests that touch
// package globals must NOT call t.Parallel.
func withStubs(
	t *testing.T,
	sym func(target, linkPath string) error,
	rm func(name string) error,
	sudo func(ctx context.Context, args ...string) error,
) {
	t.Helper()
	orig := symlinkOverrides
	t.Cleanup(func() { symlinkOverrides = orig })
	if sym != nil {
		symlinkOverrides.symlink = sym
	}
	if rm != nil {
		symlinkOverrides.remove = rm
	}
	if sudo != nil {
		symlinkOverrides.sudoRun = sudo
	}
}

func TestInstallSymlink_DirectPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	require.NoError(t, os.WriteFile(target, []byte("x"), 0o644))
	linkPath := filepath.Join(dir, "link")

	require.NoError(t, installSymlink(context.Background(), target, linkPath))

	got, err := os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, target, got)
}

func TestInstallSymlink_ReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink("/old", linkPath))

	require.NoError(t, installSymlink(context.Background(), "/new", linkPath))

	got, err := os.Readlink(linkPath)
	require.NoError(t, err)
	assert.Equal(t, "/new", got)
}

func TestInstallSymlink_RefusesToReplaceRegularFile(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "regular")
	require.NoError(t, os.WriteFile(linkPath, []byte("operator-installed"), 0o755))

	var sudoCalled bool
	withStubs(t, nil, nil, func(ctx context.Context, args ...string) error {
		sudoCalled = true
		return nil
	})

	err := installSymlink(context.Background(), "/some/target", linkPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to replace non-symlink")
	assert.False(t, sudoCalled, "sudo must not be called")

	// Original file still intact.
	body, readErr := os.ReadFile(linkPath)
	require.NoError(t, readErr)
	assert.Equal(t, "operator-installed", string(body))
}

func TestInstallSymlink_FallsBackOnPermissionError(t *testing.T) {
	var capturedArgs []string
	withStubs(t,
		func(target, linkPath string) error {
			return &os.PathError{Op: "symlink", Path: linkPath, Err: syscall.EACCES}
		},
		nil,
		func(ctx context.Context, args ...string) error {
			capturedArgs = args
			return nil
		},
	)

	err := installSymlink(context.Background(), "/some/target", "/usr/local/bin/foo")
	require.NoError(t, err)
	assert.Equal(t, []string{"ln", "-snf", "--", "/some/target", "/usr/local/bin/foo"}, capturedArgs)
}

func TestInstallSymlink_NonPermissionErrorNotEscalated(t *testing.T) {
	var sudoCalled bool
	withStubs(t,
		func(target, linkPath string) error { return errors.New("disk full") },
		nil,
		func(ctx context.Context, args ...string) error {
			sudoCalled = true
			return nil
		},
	)

	dir := t.TempDir()
	err := installSymlink(context.Background(), "/t", filepath.Join(dir, "link"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
	require.NotErrorIs(t, err, errSudoFallback)
	assert.False(t, sudoCalled)
}

func TestInstallSymlink_SudoFallbackFailurePropagates(t *testing.T) {
	withStubs(t,
		func(target, linkPath string) error {
			return &os.PathError{Op: "symlink", Path: linkPath, Err: syscall.EACCES}
		},
		nil,
		func(ctx context.Context, args ...string) error {
			return errors.New("sudo: a password is required")
		},
	)

	err := installSymlink(context.Background(), "/t", "/usr/local/bin/foo")
	require.Error(t, err)
	require.ErrorIs(t, err, errSudoFallback)
	assert.Contains(t, err.Error(), "/usr/local/bin/foo")
	assert.Contains(t, err.Error(), "sudo: a password is required")
}

func TestInstallSymlink_RejectsBadInput(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		linkPath string
		wantMsg  string
	}{
		{"empty target", "", "/usr/local/bin/foo", "target is empty"},
		{"empty linkPath", "/t", "", "linkPath is empty"},
		{"relative linkPath", "/t", "relative/path", "is not absolute"},
		{"root linkPath", "/t", "/", "resolves to root"},
		{"double-slash root", "/t", "//", "resolves to root"},
		{"dotdot to root", "/t", "/foo/..", "resolves to root"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sudoCalled bool
			withStubs(t, nil, nil, func(ctx context.Context, args ...string) error {
				sudoCalled = true
				return nil
			})

			err := installSymlink(context.Background(), tt.target, tt.linkPath)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
			assert.False(t, sudoCalled)
		})
	}
}

// lockedDir creates a 0o000 subdir inside t.TempDir(), restoring permissions
// in t.Cleanup so TempDir can clean up. Skips on non-Linux and when running
// as root (chmod is bypassed).
func lockedDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("chmod 0000 unreliable outside Linux")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses chmod permissions")
	}
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	require.NoError(t, os.Mkdir(locked, 0o700))
	require.NoError(t, os.Chmod(locked, 0o000))
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	return locked
}

func TestInstallSymlink_LstatFailureNotEscalated(t *testing.T) {
	locked := lockedDir(t)

	var sudoCalled bool
	withStubs(t, nil, nil, func(ctx context.Context, args ...string) error {
		sudoCalled = true
		return nil
	})

	err := installSymlink(context.Background(), "/some/target", filepath.Join(locked, "link"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lstat:")
	assert.False(t, sudoCalled)
}

func TestInstallSymlink_ExistingSymlinkRemoveEACCESFallsBack(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink("/old", linkPath))

	var capturedArgs []string
	withStubs(t,
		nil,
		func(name string) error {
			return &os.PathError{Op: "remove", Path: name, Err: syscall.EACCES}
		},
		func(ctx context.Context, args ...string) error {
			capturedArgs = args
			return nil
		},
	)

	err := installSymlink(context.Background(), "/new", linkPath)
	require.NoError(t, err)
	assert.Equal(t, []string{"ln", "-snf", "--", "/new", linkPath}, capturedArgs)
}

func TestRemoveSymlink_DirectPath(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink("/anywhere", linkPath))

	require.NoError(t, removeSymlink(context.Background(), linkPath))

	_, err := os.Lstat(linkPath)
	assert.ErrorIs(t, err, fs.ErrNotExist)
}

func TestRemoveSymlink_MissingIsNoOp(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "absent")
	assert.NoError(t, removeSymlink(context.Background(), linkPath))
}

func TestRemoveSymlink_RefusesNonSymlink(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "regular")
	require.NoError(t, os.WriteFile(linkPath, []byte("data"), 0o644))

	var sudoCalled bool
	withStubs(t, nil, nil, func(ctx context.Context, args ...string) error {
		sudoCalled = true
		return nil
	})

	err := removeSymlink(context.Background(), linkPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to remove non-symlink")
	assert.False(t, sudoCalled)

	_, statErr := os.Stat(linkPath)
	assert.NoError(t, statErr, "regular file must not be deleted")
}

func TestRemoveSymlink_FallsBackOnPermissionError(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink("/anywhere", linkPath))

	var capturedArgs []string
	withStubs(t,
		nil,
		func(name string) error {
			return &os.PathError{Op: "remove", Path: name, Err: syscall.EACCES}
		},
		func(ctx context.Context, args ...string) error {
			capturedArgs = args
			return nil
		},
	)

	err := removeSymlink(context.Background(), linkPath)
	require.NoError(t, err)
	assert.Equal(t, []string{"rm", "-f", "--", linkPath}, capturedArgs)
}

func TestRemoveSymlink_LstatFailureNotEscalated(t *testing.T) {
	locked := lockedDir(t)

	var sudoCalled bool
	withStubs(t, nil, nil, func(ctx context.Context, args ...string) error {
		sudoCalled = true
		return nil
	})

	err := removeSymlink(context.Background(), filepath.Join(locked, "link"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lstat:")
	assert.False(t, sudoCalled)
}

func TestRemoveSymlink_SudoFallbackFailurePropagates(t *testing.T) {
	dir := t.TempDir()
	linkPath := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink("/anywhere", linkPath))

	withStubs(t,
		nil,
		func(name string) error {
			return &os.PathError{Op: "remove", Path: name, Err: syscall.EACCES}
		},
		func(ctx context.Context, args ...string) error {
			return errors.New("sudo: a password is required")
		},
	)

	err := removeSymlink(context.Background(), linkPath)
	require.Error(t, err)
	require.ErrorIs(t, err, errSudoFallback)
	assert.Contains(t, err.Error(), linkPath)
	assert.Contains(t, err.Error(), "sudo: a password is required")
}

func TestRemoveSymlink_RejectsBadLinkPath(t *testing.T) {
	tests := []struct {
		name     string
		linkPath string
		wantMsg  string
	}{
		{"empty", "", "linkPath is empty"},
		{"relative", "relative/path", "is not absolute"},
		{"root", "/", "resolves to root"},
		{"double-slash root", "//", "resolves to root"},
		{"dotdot to root", "/foo/..", "resolves to root"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var sudoCalled bool
			withStubs(t, nil, nil, func(ctx context.Context, args ...string) error {
				sudoCalled = true
				return nil
			})

			err := removeSymlink(context.Background(), tt.linkPath)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
			assert.False(t, sudoCalled)
		})
	}
}
