package extract

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ulikunitz/xz"
)

// ArchiveType represents the type of archive format for downloads.
type ArchiveType string

const (
	// ArchiveTypeTarGz represents a gzipped tar archive (.tar.gz, .tgz).
	ArchiveTypeTarGz ArchiveType = "tar.gz"

	// ArchiveTypeZip represents a ZIP archive (.zip).
	ArchiveTypeZip ArchiveType = "zip"

	// ArchiveTypeTarXz represents an xz-compressed tar archive (.tar.xz).
	ArchiveTypeTarXz ArchiveType = "tar.xz"

	// ArchiveTypeRaw represents a raw binary file (no compression).
	// Use this for direct binary downloads (e.g., jq-linux-amd64).
	ArchiveTypeRaw ArchiveType = "raw"
)

// NormalizeArchiveType normalizes an archive type string to a canonical ArchiveType constant.
// It handles common aliases (e.g., "tgz" → ArchiveTypeTarGz, "txz" → ArchiveTypeTarXz).
// Unrecognized values are passed through as-is (NewExtractor will reject them).
func NormalizeArchiveType(raw string) ArchiveType {
	switch strings.ToLower(raw) {
	case "tar.gz", "tgz":
		return ArchiveTypeTarGz
	case "tar.xz", "txz":
		return ArchiveTypeTarXz
	case "zip":
		return ArchiveTypeZip
	case "raw":
		return ArchiveTypeRaw
	default:
		return ArchiveType(raw)
	}
}

// DetectArchiveType detects the archive type from a URL or filename.
// Returns empty string if the type cannot be detected.
func DetectArchiveType(urlOrFilename string) ArchiveType {
	lower := filepath.Base(urlOrFilename)

	// Check for compound extensions first
	if hasSuffix(lower, ".tar.gz") || hasSuffix(lower, ".tgz") {
		return ArchiveTypeTarGz
	}
	if hasSuffix(lower, ".tar.xz") || hasSuffix(lower, ".txz") {
		return ArchiveTypeTarXz
	}
	if hasSuffix(lower, ".zip") {
		return ArchiveTypeZip
	}
	return ""
}

// hasSuffix checks if s ends with suffix (case-insensitive).
func hasSuffix(s, suffix string) bool {
	if len(s) < len(suffix) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

// Extractor defines the interface for extracting archives.
type Extractor interface {
	// Extract extracts an archive from the reader to the destination directory.
	// For tar.gz: accepts any io.Reader (true streaming)
	// For zip: requires io.ReaderAt (typically *os.File)
	Extract(r io.Reader, destDir string) error
}

// NewExtractor creates an Extractor for the given archive type.
func NewExtractor(archiveType ArchiveType) (Extractor, error) {
	switch archiveType {
	case ArchiveTypeTarGz:
		return &tarGzExtractor{}, nil
	case ArchiveTypeTarXz:
		return &tarXzExtractor{}, nil
	case ArchiveTypeZip:
		return &zipExtractor{}, nil
	case ArchiveTypeRaw:
		return &rawExtractor{}, nil
	default:
		return nil, fmt.Errorf("unsupported archive type: %s", archiveType)
	}
}

var (
	_ Extractor = (*tarGzExtractor)(nil)
	_ Extractor = (*tarXzExtractor)(nil)
	_ Extractor = (*zipExtractor)(nil)
	_ Extractor = (*rawExtractor)(nil)
)

// tarGzExtractor implements Extractor for tar.gz archives.
type tarGzExtractor struct{}

// Extract extracts a tar.gz archive from the reader to the destination directory.
func (e *tarGzExtractor) Extract(r io.Reader, destDir string) error {
	slog.Debug("extracting tar.gz archive", "dest", destDir)

	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gr.Close()

	return extractTar(gr, destDir)
}

// tarXzExtractor implements Extractor for tar.xz archives.
type tarXzExtractor struct{}

// Extract extracts a tar.xz archive from the reader to the destination directory.
func (e *tarXzExtractor) Extract(r io.Reader, destDir string) error {
	slog.Debug("extracting tar.xz archive", "dest", destDir)

	xr, err := xz.NewReader(r)
	if err != nil {
		return fmt.Errorf("failed to create xz reader: %w", err)
	}

	return extractTar(xr, destDir)
}

// extractTar extracts a tar archive from the decompressed reader to the destination directory.
func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)

	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		target := filepath.Join(destDir, hdr.Name)

		// Security: prevent path traversal
		if !isInsideDir(destDir, target) {
			return fmt.Errorf("invalid file path: %s", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
		case tar.TypeReg:
			if err := extractFile(tr, target, os.FileMode(hdr.Mode)); err != nil {
				return err
			}
		case tar.TypeSymlink:
			// Security: validate symlink target
			linkTarget := filepath.Join(filepath.Dir(target), hdr.Linkname)
			if !isInsideDir(destDir, linkTarget) {
				return fmt.Errorf("invalid symlink target: %s -> %s", hdr.Name, hdr.Linkname)
			}
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}
		}
	}

	return nil
}

// zipExtractor implements Extractor for zip archives.
type zipExtractor struct{}

// Extract extracts a zip archive from the reader to the destination directory.
// The reader must implement io.ReaderAt (e.g., *os.File or *bytes.Reader).
func (e *zipExtractor) Extract(r io.Reader, destDir string) error {
	slog.Debug("extracting zip archive", "dest", destDir)

	// zip.Reader requires io.ReaderAt and size
	ra, ok := r.(io.ReaderAt)
	if !ok {
		return fmt.Errorf("zip extraction requires io.ReaderAt, got %T", r)
	}

	// Get size
	size, err := getReaderSize(r)
	if err != nil {
		return fmt.Errorf("failed to get reader size: %w", err)
	}

	zr, err := zip.NewReader(ra, size)
	if err != nil {
		return fmt.Errorf("failed to create zip reader: %w", err)
	}

	for _, f := range zr.File {
		// Skip macOS metadata directories injected by ZIP tools
		if isOSMetadataPath(f.Name) {
			continue
		}

		target := filepath.Join(destDir, f.Name)

		// Security: prevent path traversal
		if !isInsideDir(destDir, target) {
			return fmt.Errorf("invalid file path: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, f.Mode()); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open file in archive: %w", err)
		}

		if err := extractFile(rc, target, f.Mode()); err != nil {
			rc.Close()
			return err
		}
		rc.Close()
	}

	slog.Debug("zip archive extracted", "dest", destDir)
	return nil
}

// getReaderSize returns the size of the reader.
func getReaderSize(r io.Reader) (int64, error) {
	switch v := r.(type) {
	case *os.File:
		info, err := v.Stat()
		if err != nil {
			return 0, err
		}
		return info.Size(), nil
	case interface{ Len() int }:
		return int64(v.Len()), nil
	case io.Seeker:
		// Seek to end to get size, then seek back
		current, err := v.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, err
		}
		size, err := v.Seek(0, io.SeekEnd)
		if err != nil {
			return 0, err
		}
		_, err = v.Seek(current, io.SeekStart)
		if err != nil {
			return 0, err
		}
		return size, nil
	default:
		return 0, fmt.Errorf("cannot determine size for %T", r)
	}
}

// extractFile extracts a single file from an archive.
func extractFile(r io.Reader, target string, mode os.FileMode) error {
	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create file
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// isOSMetadataPath returns true if the archive entry path belongs to an
// OS-specific metadata tree that should be skipped during extraction.
// Currently handles __MACOSX/, which macOS ZIP creation tools inject.
func isOSMetadataPath(name string) bool {
	return name == "__MACOSX" || name == "__MACOSX/" || strings.HasPrefix(name, "__MACOSX/")
}

// isInsideDir checks if target path is inside the base directory.
// This prevents path traversal attacks.
func isInsideDir(baseDir, target string) bool {
	rel, err := filepath.Rel(baseDir, target)
	if err != nil {
		return false
	}
	// Check for path traversal (../)
	return rel != ".." && !filepath.IsAbs(rel) && len(rel) > 0 && rel[0] != '.'
}

// rawExtractor implements Extractor for raw binary files.
type rawExtractor struct{}

// Extract copies a raw binary file to the destination directory.
// The binary is named after the base name of destDir (the tool name).
func (e *rawExtractor) Extract(r io.Reader, destDir string) error {
	slog.Debug("extracting raw binary", "dest", destDir)

	// Create destination directory
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Use the directory name as the binary name
	binName := filepath.Base(destDir)
	target := filepath.Join(destDir, binName)

	// Create the binary file with executable permissions
	f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return fmt.Errorf("failed to create binary file: %w", err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("failed to write binary file: %w", err)
	}

	slog.Debug("raw binary extracted", "target", target)
	return nil
}
