package download

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Downloader defines the interface for downloading and verifying artifacts.
type Downloader interface {
	// Download downloads a file from the given URL to destPath.
	// Returns the path to the downloaded file.
	Download(ctx context.Context, url, destPath string) (string, error)

	// Verify verifies the checksum of a file by fetching the checksum file from URL.
	// It tries to fetch <originalURL>.sha256 and verifies against the downloaded file.
	// If the checksum file is not found, verification is skipped.
	Verify(ctx context.Context, filePath, originalURL string) error
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

// HashAlgorithm represents a checksum hash algorithm.
type HashAlgorithm string

const (
	HashAlgorithmSHA256 HashAlgorithm = "sha256"
	HashAlgorithmSHA512 HashAlgorithm = "sha512"
	HashAlgorithmMD5    HashAlgorithm = "md5"
)

// checksumAlgorithm defines a checksum algorithm with its suffix and hash constructor.
type checksumAlgorithm struct {
	algorithm HashAlgorithm
	suffix    string
	newHash   func() hash.Hash
}

// supportedAlgorithms defines the checksum algorithms to try, in order of preference.
var supportedAlgorithms = []checksumAlgorithm{
	{algorithm: HashAlgorithmSHA256, suffix: ".sha256", newHash: sha256.New},
	{algorithm: HashAlgorithmSHA512, suffix: ".sha512", newHash: sha512.New},
	{algorithm: HashAlgorithmMD5, suffix: ".md5", newHash: md5.New},
}

// Verify verifies the checksum of a file by fetching the checksum file from URL.
// It tries to fetch <originalURL>.sha256, .sha512, .md5 in order of preference.
// If no checksum file is found, verification is skipped.
func (d *httpDownloader) Verify(ctx context.Context, filePath, originalURL string) error {
	slog.Debug("verifying checksum", "file", filePath)

	// Try each algorithm in order
	for _, alg := range supportedAlgorithms {
		checksumURL := originalURL + alg.suffix
		expectedHash, err := d.fetchChecksum(ctx, checksumURL)
		if err != nil {
			// Try next algorithm
			continue
		}

		// Check if hash was parsed successfully
		if expectedHash == "" {
			slog.Warn("checksum file found but could not parse hash", "algorithm", alg.algorithm, "url", checksumURL)
			continue
		}

		slog.Debug("found checksum file", "algorithm", alg.algorithm, "url", checksumURL)

		// Found a checksum file, verify
		if err := d.verifyWithHash(filePath, expectedHash, alg.newHash); err != nil {
			return err
		}

		slog.Debug("checksum verified", "algorithm", alg.algorithm)
		return nil
	}

	// No checksum file found, skip verification
	slog.Debug("no checksum file found, skipping verification")
	return nil
}

// verifyWithHash verifies a file's checksum using the given hash function.
func (d *httpDownloader) verifyWithHash(filePath, expectedHash string, newHash func() hash.Hash) error {
	// Open file
	f, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	// Calculate checksum
	h := newHash()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}
	actualHash := hex.EncodeToString(h.Sum(nil))

	// Compare
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: expected %s, got %s", expectedHash, actualHash)
	}

	return nil
}

// fetchChecksum fetches and parses a checksum file.
// Returns the hash string or error if not found.
func (d *httpDownloader) fetchChecksum(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum file not found: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	return parseChecksumFile(string(body)), nil
}

// parseChecksumFile parses a checksum file content.
// Supports formats:
// - "<hash>"
// - "<hash>  <filename>"
// - "<hash> *<filename>"
func parseChecksumFile(content string) string {
	content = strings.TrimSpace(content)

	// Split by whitespace
	parts := strings.Fields(content)
	if len(parts) == 0 {
		return ""
	}

	// First field is the hash
	return parts[0]
}
