package place

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// PlaceTarget contains information about the tool to be placed.
type PlaceTarget struct {
	Name       string // Tool name (e.g., ripgrep)
	Version    string // Version (e.g., 14.1.1)
	BinaryName string // Binary name (e.g., rg)
}

// PlaceResult contains information about the placed tool.
type PlaceResult struct {
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
	// Validate checks the binary state and returns the required action.
	// expectedHash is the expected SHA256 hash of the binary.
	Validate(target PlaceTarget, expectedHash string) (ValidateAction, error)

	// Place finds and places a binary from srcDir to the tools directory.
	// Returns the result containing the path to the placed binary.
	Place(srcDir string, target PlaceTarget) (*PlaceResult, error)

	// Symlink creates a symlink in binDir pointing to the placed binary.
	// Returns the path to the created symlink.
	Symlink(target PlaceTarget) (string, error)

	// Cleanup removes a file or directory.
	// Does not return error if path does not exist.
	Cleanup(path string) error
}

// filePlacer implements Placer.
type filePlacer struct {
	toolsDir string // e.g., ~/.local/share/toto/tools
	binDir   string // e.g., ~/.local/bin
}

// NewPlacer creates a new Placer.
func NewPlacer(toolsDir, binDir string) Placer {
	return &filePlacer{
		toolsDir: toolsDir,
		binDir:   binDir,
	}
}

// binaryPath returns the path to the binary for the target.
func (p *filePlacer) binaryPath(target PlaceTarget) string {
	return filepath.Join(p.toolsDir, target.Name, target.Version, target.BinaryName)
}

// Validate checks the binary state and returns the required action.
func (p *filePlacer) Validate(target PlaceTarget, expectedHash string) (ValidateAction, error) {
	path := p.binaryPath(target)
	slog.Debug("validating binary", "path", path, "expectedHash", expectedHash)

	// Check if binary exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		slog.Debug("binary does not exist", "action", ValidateActionInstall)
		return ValidateActionInstall, nil
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
func (p *filePlacer) Place(srcDir string, target PlaceTarget) (*PlaceResult, error) {
	destDir := filepath.Join(p.toolsDir, target.Name, target.Version)
	slog.Debug("placing binary", "src", srcDir, "dest", destDir, "binary", target.BinaryName)

	// Find binary in srcDir
	srcPath, err := findBinary(srcDir, target.BinaryName)
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

	slog.Debug("binary placed", "path", destPath)
	return &PlaceResult{BinaryPath: destPath}, nil
}

// Symlink creates a symlink in binDir pointing to the placed binary.
func (p *filePlacer) Symlink(target PlaceTarget) (string, error) {
	srcPath := filepath.Join(p.toolsDir, target.Name, target.Version, target.BinaryName)
	slog.Debug("creating symlink", "src", srcPath, "binDir", p.binDir, "linkName", target.BinaryName)

	// Verify source exists
	if _, err := os.Stat(srcPath); os.IsNotExist(err) {
		return "", fmt.Errorf("source binary not found: %s", srcPath)
	}

	// Create binDir
	if err := os.MkdirAll(p.binDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create bin directory: %w", err)
	}

	linkPath := filepath.Join(p.binDir, target.BinaryName)

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
