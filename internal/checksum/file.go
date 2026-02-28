package checksum

import (
	"bufio"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// FileFormat represents the format of a checksum file.
type FileFormat string

const (
	// FileFormatGNU is the GNU coreutils format: "<hash>  <filename>" or "<hash> *<filename>"
	// This is the default output format of sha256sum, sha512sum, etc.
	FileFormatGNU FileFormat = "gnu"

	// FileFormatBSD is the BSD format: "SHA256 (<filename>) = <hash>"
	// This is the output format of sha256sum --tag, or macOS shasum.
	FileFormatBSD FileFormat = "bsd"

	// FileFormatGoJSON is Go's download API JSON format.
	// Format: [{"version":"go1.x","files":[{"filename":"...","sha256":"..."}]}]
	// See: https://go.dev/dl/?mode=json
	FileFormatGoJSON FileFormat = "go_json"

	// FileFormatBareHash is a single bare hash value with no filename.
	// This is used by tools like starship that publish per-file checksum files
	// (e.g., "tool.tar.gz.sha256") containing only the hash value.
	FileFormatBareHash FileFormat = "bare_hash"

	// FileFormatUnknown is returned when the format cannot be determined.
	FileFormatUnknown FileFormat = "unknown"
)

// bsdPattern matches BSD-style checksum lines: "SHA256 (filename) = hash"
var bsdPattern = regexp.MustCompile(`^(SHA256|SHA512)\s+\((.+)\)\s+=\s+([a-fA-F0-9]+)$`)

// DetectFileFormat detects the format of a checksum file from its content.
func DetectFileFormat(content []byte) FileFormat {
	// Try Go JSON format first
	var releases []goRelease
	if json.Unmarshal(content, &releases) == nil && len(releases) > 0 {
		// Verify it looks like Go's dl API format
		if releases[0].Version != "" && releases[0].Files != nil {
			return FileFormatGoJSON
		}
	}

	// Check first non-empty line for format detection
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// Check BSD format: "SHA256 (filename) = hash"
		if bsdPattern.MatchString(line) {
			return FileFormatBSD
		}

		// Check GNU format: "hash  filename" or "hash *filename"
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			hash := parts[0]
			// GNU format has hash as first field (64 chars for SHA256, 128 for SHA512)
			if len(hash) == 64 || len(hash) == 128 {
				if isHexString(hash) {
					return FileFormatGNU
				}
			}
		}

		// Check bare hash format: single hex string with no filename (per-file checksum)
		if len(parts) == 1 {
			hash := parts[0]
			if (len(hash) == 64 || len(hash) == 128) && isHexString(hash) {
				if !hasMoreNonEmptyLines(scanner) {
					return FileFormatBareHash
				}
			}
		}

		// Could not determine format from first line
		return FileFormatUnknown
	}

	return FileFormatUnknown
}

// isHexString checks if a string contains only hexadecimal characters.
func isHexString(s string) bool {
	for _, c := range s {
		isDigit := c >= '0' && c <= '9'
		isLowerHex := c >= 'a' && c <= 'f'
		isUpperHex := c >= 'A' && c <= 'F'
		if !isDigit && !isLowerHex && !isUpperHex {
			return false
		}
	}
	return len(s) > 0
}

// hasMoreNonEmptyLines checks if the scanner has more non-empty lines remaining.
func hasMoreNonEmptyLines(scanner *bufio.Scanner) bool {
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) != "" {
			return true
		}
	}
	return false
}

// ParseFile parses a checksum file and extracts the hash for the given filename.
// Automatically detects the file format.
func ParseFile(content []byte, filename string) (Algorithm, Digest, error) {
	format := DetectFileFormat(content)
	switch format {
	case FileFormatGoJSON:
		return parseGoJSON(content, filename)
	case FileFormatBSD:
		return parseBSD(content, filename)
	case FileFormatGNU:
		return parseGNU(content, filename)
	case FileFormatBareHash:
		return parseBareHash(content, filename)
	default:
		return "", "", fmt.Errorf("unknown or unsupported checksum file format")
	}
}

// goRelease represents a Go release in the JSON API response.
type goRelease struct {
	Version string   `json:"version"`
	Stable  bool     `json:"stable"`
	Files   []goFile `json:"files"`
}

// goFile represents a file in a Go release.
type goFile struct {
	Filename string `json:"filename"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Kind     string `json:"kind"`
}

// parseGoJSON parses Go's dl API JSON format checksums.
// Format: [{"version":"go1.x","files":[{"filename":"...","sha256":"..."}]}]
func parseGoJSON(content []byte, filename string) (Algorithm, Digest, error) {
	var releases []goRelease
	if err := json.Unmarshal(content, &releases); err != nil {
		return "", "", fmt.Errorf("failed to parse JSON checksums: %w", err)
	}

	for _, release := range releases {
		for _, file := range release.Files {
			if file.Filename == filename && file.SHA256 != "" {
				return AlgorithmSHA256, Digest(file.SHA256), nil
			}
		}
	}

	return "", "", fmt.Errorf("checksum for %q not found in Go JSON checksums", filename)
}

// parseBSD parses BSD format checksums.
// Format: "SHA256 (filename) = hash"
func parseBSD(content []byte, filename string) (Algorithm, Digest, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		matches := bsdPattern.FindStringSubmatch(line)
		if matches == nil {
			continue
		}

		algo := matches[1]
		file := matches[2]
		hash := matches[3]

		if file == filename || filepath.Base(file) == filename {
			var algorithm Algorithm
			switch algo {
			case "SHA256":
				algorithm = AlgorithmSHA256
			case "SHA512":
				algorithm = AlgorithmSHA512
			default:
				return "", "", fmt.Errorf("unsupported algorithm in BSD format: %s", algo)
			}
			return algorithm, Digest(hash), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("failed to read checksum file: %w", err)
	}

	return "", "", fmt.Errorf("checksum for %q not found in BSD checksums file", filename)
}

// parseGNU parses GNU coreutils format checksums.
// Format: "<hash>  <filename>" or "<hash> *<filename>"
func parseGNU(content []byte, filename string) (Algorithm, Digest, error) {
	scanner := bufio.NewScanner(strings.NewReader(string(content)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		hash, file := parseGNULine(line)
		if file == filename || filepath.Base(file) == filename {
			algorithm := DetectAlgorithm(hash)
			if algorithm == "" {
				return "", "", fmt.Errorf("could not determine hash algorithm for %q", hash)
			}
			return algorithm, Digest(hash), nil
		}
	}

	if err := scanner.Err(); err != nil {
		return "", "", fmt.Errorf("failed to read checksum file: %w", err)
	}

	return "", "", fmt.Errorf("checksum for %q not found in GNU checksums file", filename)
}

// parseBareHash parses a bare hash checksum file (single hash value, no filename).
// The filename parameter is accepted for signature consistency but unused,
// as bare hash files are per-file checksums with no filename in the content.
func parseBareHash(content []byte, _ string) (Algorithm, Digest, error) {
	hash := strings.TrimSpace(string(content))
	if hash == "" {
		return "", "", fmt.Errorf("empty bare hash content")
	}

	algorithm := DetectAlgorithm(hash)
	if algorithm == "" {
		return "", "", fmt.Errorf("could not determine hash algorithm for bare hash %q", hash)
	}

	return algorithm, Digest(hash), nil
}

// parseGNULine parses a line from a GNU format checksums file.
// Supports formats:
//   - "<hash>  <filename>"
//   - "<hash> *<filename>"
//   - "<hash>  *<filename>"
func parseGNULine(line string) (hash, filename string) {
	parts := strings.Fields(line)
	if len(parts) < 2 {
		return "", ""
	}

	hash = parts[0]
	filename = parts[1]

	// Remove leading * from filename (binary mode indicator)
	filename = strings.TrimPrefix(filename, "*")

	return hash, filename
}
