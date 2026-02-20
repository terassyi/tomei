package download

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/terassyi/tomei/internal/checksum"
	tomeiErrors "github.com/terassyi/tomei/internal/errors"
	"github.com/terassyi/tomei/internal/resource"
)

// ProgressCallback is called during download to report progress.
// total is -1 if Content-Length is unknown.
type ProgressCallback func(downloaded, total int64)

// Downloader defines the interface for downloading and verifying artifacts.
type Downloader interface {
	// Download downloads a file from the given URL to destPath.
	// Returns the path to the downloaded file.
	Download(ctx context.Context, url, destPath string) (string, error)

	// DownloadWithProgress downloads a file with progress callback.
	DownloadWithProgress(ctx context.Context, url, destPath string, callback ProgressCallback) (string, error)

	// Verify verifies the checksum of a downloaded file.
	// checksum can be nil (skip verification), have a direct value, or a URL to fetch.
	Verify(ctx context.Context, filePath string, checksum *resource.Checksum) error
}

// httpDownloader implements Downloader using HTTP.
type httpDownloader struct {
	client *http.Client
}

// NewDownloader creates a new Downloader with the default HTTP client.
func NewDownloader() Downloader {
	return &httpDownloader{
		client: http.DefaultClient,
	}
}

// NewDownloaderWithClient creates a new Downloader with the given HTTP client.
func NewDownloaderWithClient(client *http.Client) Downloader {
	if client == nil {
		client = http.DefaultClient
	}
	return &httpDownloader{
		client: client,
	}
}

// Download downloads a file from the given URL to destPath.
// Returns the path to the downloaded file.
func (d *httpDownloader) Download(ctx context.Context, url, destPath string) (string, error) {
	return d.DownloadWithProgress(ctx, url, destPath, nil)
}

// DownloadWithProgress downloads a file with optional progress callback.
func (d *httpDownloader) DownloadWithProgress(ctx context.Context, url, destPath string, callback ProgressCallback) (string, error) {
	slog.Debug("downloading file", "url", url, "dest", destPath)

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	resp, err := d.client.Do(req)
	if err != nil {
		return "", &tomeiErrors.Error{
			Category: tomeiErrors.CategoryNetwork,
			Code:     tomeiErrors.CodeNetworkFailed,
			Message:  fmt.Sprintf("failed to download from %s", url),
			Cause:    err,
		}
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return "", &tomeiErrors.Error{
			Category: tomeiErrors.CategoryNetwork,
			Code:     tomeiErrors.CodeHTTPError,
			Message:  fmt.Sprintf("failed to download: HTTP %d", resp.StatusCode),
			Details:  map[string]any{"url": url, "status_code": resp.StatusCode},
		}
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

	// Download with progress
	total := resp.ContentLength
	var reader io.Reader = resp.Body

	if callback != nil {
		reader = &progressReader{
			reader:   resp.Body,
			total:    total,
			callback: callback,
		}
	}

	if _, err := io.Copy(f, reader); err != nil {
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

// progressReader wraps an io.Reader and reports progress.
type progressReader struct {
	reader     io.Reader
	total      int64
	downloaded int64
	callback   ProgressCallback
}

func (r *progressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.downloaded += int64(n)
		r.callback(r.downloaded, r.total)
	}
	return n, err
}

// Verify verifies the checksum of a downloaded file.
// checksum can be nil (skip verification), have a direct value, or a URL to fetch.
func (d *httpDownloader) Verify(ctx context.Context, filePath string, cs *resource.Checksum) error {
	if cs == nil {
		slog.Debug("no checksum specified, skipping verification")
		return nil
	}

	slog.Debug("verifying checksum", "file", filePath)

	var expectedHash checksum.Digest
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
// Supports two formats:
//   - Standard text format: "<hash>  <filename>" or "<hash> *<filename>"
//   - Go JSON format: [{"version":"go1.x","files":[{"filename":"...","sha256":"..."}]}]
func (d *httpDownloader) fetchChecksumFromURL(ctx context.Context, url, filename string) (checksum.Algorithm, checksum.Digest, error) {
	slog.Debug("fetching checksum file", "url", url, "filename", filename)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := d.client.Do(req)
	if err != nil {
		return "", "", &tomeiErrors.Error{
			Category: tomeiErrors.CategoryNetwork,
			Code:     tomeiErrors.CodeNetworkFailed,
			Message:  fmt.Sprintf("failed to fetch checksum file from %s", url),
			Cause:    err,
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", &tomeiErrors.Error{
			Category: tomeiErrors.CategoryNetwork,
			Code:     tomeiErrors.CodeHTTPError,
			Message:  fmt.Sprintf("failed to fetch checksum file: HTTP %d", resp.StatusCode),
			Details:  map[string]any{"url": url, "status_code": resp.StatusCode},
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read checksum file: %w", err)
	}

	algo, hash, err := checksum.ParseFile(body, filename)
	if err != nil {
		return "", "", err
	}

	slog.Debug("found checksum", "file", filename, "algorithm", algo, "format", checksum.DetectFileFormat(body))
	return algo, hash, nil
}
