package download

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/terassyi/toto/internal/checksum"
	"github.com/terassyi/toto/internal/resource"
)

// Downloader defines the interface for downloading and verifying artifacts.
type Downloader interface {
	// Download downloads a file from the given URL to destPath.
	// Returns the path to the downloaded file.
	Download(ctx context.Context, url, destPath string) (string, error)

	// Verify verifies the checksum of a downloaded file.
	// checksum can be nil (skip verification), have a direct value, or a URL to fetch.
	Verify(ctx context.Context, filePath string, checksum *resource.Checksum) error
}

// httpDownloader implements Downloader using HTTP.
type httpDownloader struct {
	client *http.Client
}

// NewDownloader creates a new Downloader.
func NewDownloader() Downloader {
	return &httpDownloader{
		client: http.DefaultClient,
	}
}

// Download downloads a file from the given URL to destPath.
// Returns the path to the downloaded file.
func (d *httpDownloader) Download(ctx context.Context, url, destPath string) (string, error) {
	slog.Debug("downloading file", "url", url, "dest", destPath)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	resp, err := d.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download: HTTP %d", resp.StatusCode)
	}

	// Create parent directory if needed
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Create destination file
	tmpPath := destPath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer func() {
		f.Close()
		os.Remove(tmpPath) // Clean up on error
	}()

	// Download
	if _, err := io.Copy(f, resp.Body); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	// Close file before rename
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("failed to close file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpPath, destPath); err != nil {
		return "", fmt.Errorf("failed to rename file: %w", err)
	}

	slog.Debug("download completed", "path", destPath)
	return destPath, nil
}

// Verify verifies the checksum of a downloaded file.
// checksum can be nil (skip verification), have a direct value, or a URL to fetch.
func (d *httpDownloader) Verify(ctx context.Context, filePath string, cs *resource.Checksum) error {
	if cs == nil {
		slog.Debug("no checksum specified, skipping verification")
		return nil
	}

	slog.Debug("verifying checksum", "file", filePath)

	var expectedHash string
	var algorithm checksum.Algorithm

	if cs.Value != "" {
		// Direct value: "sha256:abc123..." or "sha512:abc123..."
		alg, hash, err := checksum.Parse(cs.Value)
		if err != nil {
			return err
		}
		algorithm = alg
		expectedHash = hash
	} else if cs.URL != "" {
		// Fetch from URL
		filename := filepath.Base(filePath)
		if cs.FilePattern != "" {
			filename = cs.FilePattern
		}

		alg, hash, err := d.fetchChecksumFromURL(ctx, cs.URL, filename)
		if err != nil {
			return err
		}
		algorithm = alg
		expectedHash = hash
	} else {
		slog.Debug("no checksum value or URL specified, skipping verification")
		return nil
	}

	// Verify
	if err := checksum.Verify(filePath, algorithm, expectedHash); err != nil {
		return err
	}

	slog.Debug("checksum verified", "algorithm", algorithm)
	return nil
}

// fetchChecksumFromURL fetches a checksums file from URL and extracts the hash for the given filename.
func (d *httpDownloader) fetchChecksumFromURL(ctx context.Context, url, filename string) (checksum.Algorithm, string, error) {
	slog.Debug("fetching checksum file", "url", url, "filename", filename)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch checksum file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("failed to fetch checksum file: HTTP %d", resp.StatusCode)
	}

	// Parse checksums file
	// Format: "<hash>  <filename>" or "<hash> *<filename>"
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		hash, file := parseChecksumLine(line)
		if file == filename || filepath.Base(file) == filename {
			// Determine algorithm from hash length
			algorithm := checksum.DetectAlgorithm(hash)
			if algorithm == "" {
				return "", "", fmt.Errorf("could not determine hash algorithm for %q", hash)
			}
			slog.Debug("found checksum for file", "file", file, "algorithm", algorithm)
			return algorithm, hash, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("failed to read checksum file: %w", err)
	}

	return "", "", fmt.Errorf("checksum for %q not found in checksums file", filename)
}

// parseChecksumLine parses a line from a checksums file.
// Supports formats:
// - "<hash>  <filename>"
// - "<hash> *<filename>"
// - "<hash>  *<filename>"
func parseChecksumLine(line string) (hash, filename string) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "", ""
	}

	hash = parts[0]
	filename = parts[1]

	// Remove leading * from filename (BSD-style)
	filename = strings.TrimPrefix(filename, "*")

	return hash, filename
}
