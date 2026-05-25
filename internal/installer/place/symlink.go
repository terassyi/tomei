package place

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// errSudoFallback is wrapped into errors when the direct syscall hit
// fs.ErrPermission and the helper fell back to `sudo -n`. Tests use
// errors.Is to detect "this operation required privilege escalation."
// Stays unexported until callers outside this package actually need it.
var errSudoFallback = errors.New("sudo fallback")

// symlinkOverrides groups the three swappable function dependencies of the
// portable-symlink helpers. Production callers must not touch this; the test
// file in this package overrides the fields. Because the value is a package
// global, tests that touch it must NOT call t.Parallel().
var symlinkOverrides = struct {
	symlink func(target, linkPath string) error
	remove  func(name string) error
	sudoRun func(ctx context.Context, args ...string) error
}{
	symlink: os.Symlink,
	remove:  os.Remove,
	sudoRun: defaultSudoRun,
}

// defaultSudoRun executes `sudo -n <args...>`, capturing stderr for diagnostics.
// Successful-run stderr is intentionally discarded — only-on-error capture
// keeps logs quiet on the happy path while turning the otherwise-opaque
// "exit status 1" into something actionable (e.g. "sudo: a password is
// required" when the cached ticket has expired).
func defaultSudoRun(ctx context.Context, args ...string) error {
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "sudo", append([]string{"-n"}, args...)...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

// installSymlink creates linkPath -> target, escalating to `sudo -n ln -sf`
// on permission error. Existing symlinks at linkPath are replaced; existing
// non-symlinks (regular files, directories) are refused to avoid silently
// clobbering operator-installed binaries.
//
// Assumes the caller has already cached a sudo ticket via sudoHandler
// (cmd/tomei/apply.go); the -n flag never prompts.
func installSymlink(ctx context.Context, target, linkPath string) error {
	if target == "" {
		return errors.New("symlink: target is empty")
	}
	if err := validateLinkPath(linkPath); err != nil {
		return err
	}
	info, exists, err := classifyLstat(linkPath)
	if err != nil {
		return fmt.Errorf("install symlink %q: lstat: %w", linkPath, err)
	}
	if exists {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("install symlink %q: refusing to replace non-symlink", linkPath)
		}
		if rmErr := symlinkOverrides.remove(linkPath); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			if errors.Is(rmErr, fs.ErrPermission) {
				return sudoInstallSymlink(ctx, target, linkPath)
			}
			return fmt.Errorf("install symlink %q: remove existing: %w", linkPath, rmErr)
		}
	}
	if err := symlinkOverrides.symlink(target, linkPath); err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return sudoInstallSymlink(ctx, target, linkPath)
		}
		return fmt.Errorf("install symlink %q -> %q: %w", target, linkPath, err)
	}
	return nil
}

// removeSymlink removes linkPath, escalating to `sudo -n rm -f` on permission
// error. A missing linkPath is a no-op (matches `rm -f`). Refuses to remove a
// non-symlink at linkPath — the helper is named removeSymlink for a reason.
func removeSymlink(ctx context.Context, linkPath string) error {
	if err := validateLinkPath(linkPath); err != nil {
		return err
	}
	info, exists, err := classifyLstat(linkPath)
	if err != nil {
		return fmt.Errorf("remove symlink %q: lstat: %w", linkPath, err)
	}
	if !exists {
		return nil
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return fmt.Errorf("remove symlink %q: refusing to remove non-symlink", linkPath)
	}

	rmErr := symlinkOverrides.remove(linkPath)
	if rmErr == nil || errors.Is(rmErr, fs.ErrNotExist) {
		return nil
	}
	if errors.Is(rmErr, fs.ErrPermission) {
		if sudoErr := symlinkOverrides.sudoRun(ctx, "rm", "-f", "--", linkPath); sudoErr != nil {
			return fmt.Errorf("remove symlink %q: %w: %w", linkPath, errSudoFallback, sudoErr)
		}
		return nil
	}
	return fmt.Errorf("remove symlink %q: %w", linkPath, rmErr)
}

// classifyLstat is the single audit point for the safety invariant "refuse on
// un-classified Lstat". Callers must treat `err != nil` as a hard stop —
// proceeding to a sudo fallback would let sudo act on whatever is at linkPath,
// bypassing later non-symlink safety checks.
func classifyLstat(linkPath string) (info os.FileInfo, exists bool, err error) {
	info, lstatErr := os.Lstat(linkPath)
	if errors.Is(lstatErr, fs.ErrNotExist) {
		return nil, false, nil
	}
	if lstatErr != nil {
		return nil, false, lstatErr
	}
	return info, true, nil
}

func sudoInstallSymlink(ctx context.Context, target, linkPath string) error {
	// `ln -snf`: -s symbolic, -f force-replace, -n = treat dest as file even
	// if it's a symlink to a directory (without this, ln dereferences and
	// creates the new link *inside* the directory).
	if err := symlinkOverrides.sudoRun(ctx, "ln", "-snf", "--", target, linkPath); err != nil {
		return fmt.Errorf("install symlink %q -> %q: %w: %w", target, linkPath, errSudoFallback, err)
	}
	return nil
}

// validateLinkPath rejects obviously-dangerous inputs so a buggy caller can't
// have sudo cheerfully run `rm -f -- ""` or similar. Deeper allow-listing
// (refuse /etc, /bin, …) belongs at the call site that knows binDir intent.
func validateLinkPath(linkPath string) error {
	if linkPath == "" {
		return errors.New("symlink: linkPath is empty")
	}
	if !filepath.IsAbs(linkPath) {
		return fmt.Errorf("symlink: linkPath %q is not absolute", linkPath)
	}
	if filepath.Clean(linkPath) == "/" {
		return errors.New("symlink: linkPath resolves to root directory")
	}
	return nil
}
