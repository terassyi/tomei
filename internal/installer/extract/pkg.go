package extract

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
)

// pkgExtractor implements Extractor for macOS flat packages (.pkg).
// It uses pkgutil --expand-full which is available on macOS without sudo.
type pkgExtractor struct{}

// Extract extracts a macOS .pkg file to the destination directory.
// The reader must be an *os.File since pkgutil operates on file paths.
func (e *pkgExtractor) Extract(r io.Reader, destDir string) error {
	slog.Debug("extracting pkg archive", "dest", destDir)

	f, ok := r.(*os.File)
	if !ok {
		return fmt.Errorf("pkg extraction requires *os.File, got %T", r)
	}

	// Check pkgutil availability before attempting extraction
	if _, err := exec.LookPath("pkgutil"); err != nil {
		return fmt.Errorf("pkgutil not found (pkg extraction is only supported on macOS): %w", err)
	}

	// Run pkgutil --expand-full to extract the package contents
	var stderr bytes.Buffer
	cmd := exec.Command("pkgutil", "--expand-full", f.Name(), destDir)
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to expand pkg %s: %w (stderr: %s)", f.Name(), err, stderr.String())
	}

	// Security: verify all extracted paths and symlink targets are inside destDir
	if err := validateExtractedPaths(destDir); err != nil {
		return err
	}

	slog.Debug("pkg archive extracted", "dest", destDir)
	return nil
}

// validateExtractedPaths walks the extracted directory and verifies that
// all symlink targets resolve to paths inside destDir (path traversal check).
func validateExtractedPaths(destDir string) error {
	absDestDir, err := filepath.Abs(destDir)
	if err != nil {
		return fmt.Errorf("failed to resolve destDir: %w", err)
	}

	return filepath.WalkDir(absDestDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Check symlinks for path traversal
		if d.Type()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return fmt.Errorf("failed to read symlink %s: %w", path, err)
			}

			// Resolve relative symlink targets against the symlink's directory
			if !filepath.IsAbs(linkTarget) {
				linkTarget = filepath.Join(filepath.Dir(path), linkTarget)
			}

			resolved, err := filepath.Abs(linkTarget)
			if err != nil {
				return fmt.Errorf("failed to resolve symlink target %s: %w", linkTarget, err)
			}

			if !isInsideDir(absDestDir, resolved) {
				return fmt.Errorf("invalid symlink target in pkg: %s -> %s (escapes %s)", path, linkTarget, absDestDir)
			}
		}

		return nil
	})
}
