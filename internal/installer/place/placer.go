package place

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// Target contains information about the tool to be placed.
type Target struct {
	Name          string // Tool name (e.g., ripgrep)
	Version       string // Version (e.g., 14.1.1)
	BinaryName    string // Binary name for placement and symlink (e.g., rg)
	SrcBinaryName string // Binary name to search in archive (e.g., krew-linux_arm64); empty = BinaryName
}

// Result contains information about the placed tool.
type Result struct {
	BinaryPath string // Path to the placed binary
	LinkPath   string // Path to the symlink (set after Symlink)
}

// ValidateAction represents the action to take based on validation.
type ValidateAction int

const (
	ValidateActionInstall ValidateAction = iota // Binary does not exist -> install
	ValidateActionSkip                          // Binary exists with matching hash -> skip
	ValidateActionReplace                       // Binary exists with different hash -> replace
)

func (a ValidateAction) String() string {
	switch a {
	case ValidateActionInstall:
		return "install"
	case ValidateActionSkip:
		return "skip"
	case ValidateActionReplace:
		return "replace"
	default:
		return "unknown"
	}
}

// Placer defines the interface for placing binaries and managing symlinks.
type Placer interface {
	// BinaryPath returns the full path where the binary would be placed.
	BinaryPath(target Target) string

	// LinkPath returns the full path where the symlink would be created in binDir.
	LinkPath(target Target, binDir string) string

	// Validate checks the binary state and returns the required action.
	// expectedHash is the expected SHA256 hash of the binary.
	Validate(target Target, expectedHash string) (ValidateAction, error)

	// Place finds and places a binary from srcDir to the tools directory.
	// Returns the result containing the path to the placed binary.
	Place(srcDir string, target Target) (*Result, error)

	// Symlink creates a symlink in binDir pointing to the placed binary.
	// Returns the path to the created symlink. Rejects empty, relative, or
	// root binDir values to prevent silent cwd-relative symlinks.
	Symlink(target Target, binDir string) (string, error)

	// Cleanup removes a file or directory.
	// Does not return error if path does not exist.
	Cleanup(path string) error

	// ToolsDir returns the root tools directory path.
	ToolsDir() string
}

var _ Placer = (*filePlacer)(nil)

// filePlacer implements Placer.
type filePlacer struct {
	toolsDir string // e.g., ~/.local/share/tomei/tools
}

// NewPlacer creates a new Placer. The bin directory is per-call; the placer
// holds no default — see LinkPath / Symlink.
func NewPlacer(toolsDir string) Placer {
	return &filePlacer{toolsDir: toolsDir}
}

// BinaryPath returns the full path where the binary would be placed.
func (p *filePlacer) BinaryPath(target Target) string {
	return filepath.Join(p.toolsDir, target.Name, target.Version, target.BinaryName)
}

// LinkPath returns the full path where the symlink would be created in binDir.
// Pure path arithmetic — no validation; Symlink is where the syscall happens
// and where bad binDir values are rejected.
func (p *filePlacer) LinkPath(target Target, binDir string) string {
	return filepath.Join(binDir, target.BinaryName)
}

// Validate checks the binary state and returns the required action.
func (p *filePlacer) Validate(target Target, expectedHash string) (ValidateAction, error) {
	path := p.BinaryPath(target)
	slog.Debug("validating binary", "path", path, "expectedHash", expectedHash)

	// Check if binary exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		slog.Debug("binary does not exist", "action", ValidateActionInstall)
		return ValidateActionInstall, nil
	}

	// If no expected hash, skip (existence check only)
	if expectedHash == "" {
		slog.Debug("no expected hash, skipping", "action", ValidateActionSkip)
		return ValidateActionSkip, nil
	}

	// Calculate current hash
	currentHash, err := p.calculateHash(path)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate hash: %w", err)
	}

	// Compare hashes
	if currentHash == expectedHash {
		slog.Debug("binary hash matches", "action", ValidateActionSkip)
		return ValidateActionSkip, nil
	}

	slog.Debug("binary hash mismatch", "currentHash", currentHash, "expectedHash", expectedHash, "action", ValidateActionReplace)
	return ValidateActionReplace, nil
}

// calculateHash calculates the SHA256 hash of a file.
func (p *filePlacer) calculateHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// Place finds and places a binary from srcDir to the tools directory.
func (p *filePlacer) Place(srcDir string, target Target) (*Result, error) {
	searchName := target.BinaryName
	if target.SrcBinaryName != "" {
		searchName = target.SrcBinaryName
	}

	destDir := filepath.Join(p.toolsDir, target.Name, target.Version)
	slog.Debug("placing binary", "src", srcDir, "dest", destDir, "binary", target.BinaryName, "search", searchName)

	// Find binary in srcDir
	srcPath, err := findBinary(srcDir, searchName)
	if err != nil {
		return nil, err
	}

	// Create destDir
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create destination directory: %w", err)
	}

	destPath := filepath.Join(destDir, target.BinaryName)

	// Copy binary to destDir (preserving permissions)
	if err := copyFile(srcPath, destPath); err != nil {
		return nil, fmt.Errorf("failed to copy binary: %w", err)
	}

	// A tool binary must be runnable. Some archives carry a non-executable mode
	// (zip stores 0644; gz/raw carry none) which copyFile preserves, so ensure
	// the executable bit here (#273). Fail loudly — a non-runnable install is useless.
	if err := EnsureExecutable(destPath); err != nil {
		return nil, fmt.Errorf("failed to make binary executable: %w", err)
	}

	slog.Debug("binary placed", "path", destPath)
	return &Result{BinaryPath: destPath}, nil
}

// Symlink creates a symlink in binDir pointing to the placed binary.
//
// For privileged installs that target the system bin directory, callers
// should use place.InstallSymlink instead — it escalates via `sudo -n ln`
// on permission error. The tool installer (SUB5 #228) routes through that
// helper when Tool.IsPrivileged() is true; this method handles the user
// arm only.
func (p *filePlacer) Symlink(target Target, binDir string) (string, error) {
	// Reject empty / relative / root binDir before any syscall — a relative
	// linkPath would resolve against the cwd of `tomei apply`, silently
	// planting the symlink in whatever directory the operator ran from.
	if err := validateBinDir(binDir); err != nil {
		return "", err
	}

	srcPath := filepath.Join(p.toolsDir, target.Name, target.Version, target.BinaryName)
	slog.Debug("creating symlink", "src", srcPath, "binDir", binDir, "linkName", target.BinaryName)

	// Verify source exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return "", fmt.Errorf("source binary not found: %s", srcPath)
	}

	// Create binDir
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create bin directory: %w", err)
	}

	linkPath := filepath.Join(binDir, target.BinaryName)

	// Remove existing symlink if present
	if _, err := os.Lstat(linkPath); err == nil {
		if err := os.Remove(linkPath); err != nil {
			return "", fmt.Errorf("failed to remove existing symlink: %w", err)
		}
	}

	// Create symlink
	if err := os.Symlink(srcPath, linkPath); err != nil {
		return "", fmt.Errorf("failed to create symlink: %w", err)
	}

	slog.Debug("symlink created", "link", linkPath, "target", srcPath)
	return linkPath, nil
}

// Cleanup removes a file or directory.
func (p *filePlacer) Cleanup(path string) error {
	slog.Debug("cleaning up", "path", path)

	if err := os.RemoveAll(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to cleanup: %w", err)
	}

	return nil
}

// ToolsDir returns the root tools directory path.
func (p *filePlacer) ToolsDir() string {
	return p.toolsDir
}

// validateBinDir rejects empty / non-absolute / "/" binDir values before any
// syscall. Mirrors the three checks in validateLinkPath (symlink.go) but with
// binDir-named error messages so failures from Placer methods are unambiguous.
func validateBinDir(binDir string) error {
	if binDir == "" {
		return errors.New("placer: binDir is empty")
	}
	if !filepath.IsAbs(binDir) {
		return fmt.Errorf("placer: binDir %q is not absolute", binDir)
	}
	if filepath.Clean(binDir) == "/" {
		return errors.New("placer: binDir resolves to root directory")
	}
	return nil
}

// findBinary searches for a binary in srcDir and its subdirectories.
func findBinary(srcDir, binaryName string) (string, error) {
	var found string

	err := filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if d.Name() == binaryName {
			found = path
			return filepath.SkipAll
		}
		return nil
	})

	if err != nil {
		return "", fmt.Errorf("failed to search for binary: %w", err)
	}

	if found == "" {
		return "", fmt.Errorf("binary not found: %s", binaryName)
	}

	return found, nil
}

// EnsureExecutable sets the executable bit on path for all classes (owner/group/other),
// preserving the existing read/write bits, when it is not already executable. It is
// idempotent: a no-op when path already has any execute bit. A tool binary is meant to be
// run, so this is safe regardless of the archive's stored mode (#273).
func EnsureExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("ensure executable: %w", err)
	}
	if info.Mode()&0o111 != 0 {
		return nil
	}
	if err := os.Chmod(path, info.Mode()|0o111); err != nil {
		return fmt.Errorf("ensure executable: %w", err)
	}
	return nil
}

// copyFile copies a file preserving permissions.
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	srcInfo, err := srcFile.Stat()
	if err != nil {
		return err
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	return nil
}
