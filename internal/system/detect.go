// Package system provides Linux distribution detection and system installer validation.
package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DistroInfo holds parsed /etc/os-release data.
type DistroInfo struct {
	ID        string   // e.g., "ubuntu", "debian", "fedora"
	IDLike    []string // e.g., ["debian"], ["rhel", "fedora"]
	VersionID string   // e.g., "22.04", "12"
	Name      string   // e.g., "Ubuntu 22.04.4 LTS"
}

// IDs returns ID followed by IDLike entries — the full family chain for matching.
func (d *DistroInfo) IDs() []string {
	ids := make([]string, 0, 1+len(d.IDLike))
	ids = append(ids, d.ID)
	ids = append(ids, d.IDLike...)
	return ids
}

// DetectDistro reads /etc/os-release (fallback: /usr/lib/os-release)
// and returns the parsed distribution info.
func DetectDistro() (*DistroInfo, error) {
	return detectDistroFrom("/")
}

// detectDistroFrom reads os-release from the given root directory.
// Accepts a custom root for testing with temp directories.
func detectDistroFrom(root string) (*DistroInfo, error) {
	paths := []string{
		filepath.Join(root, "etc", "os-release"),
		filepath.Join(root, "usr", "lib", "os-release"),
	}

	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read %s: %w", p, err)
		}
		info, err := parseOSRelease(string(data))
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", p, err)
		}
		return info, nil
	}

	return nil, fmt.Errorf("os-release not found: checked %s", strings.Join(paths, ", "))
}

// parseOSRelease parses KEY=VALUE pairs from os-release content.
func parseOSRelease(content string) (*DistroInfo, error) {
	fields := make(map[string]string)

	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		// Remove surrounding quotes
		value = strings.Trim(value, `"'`)
		fields[key] = value
	}

	id := fields["ID"]
	if id == "" {
		return nil, fmt.Errorf("os-release missing required ID field")
	}

	info := &DistroInfo{
		ID:        id,
		VersionID: fields["VERSION_ID"],
		Name:      fields["PRETTY_NAME"],
	}

	if idLike := fields["ID_LIKE"]; idLike != "" {
		info.IDLike = strings.Fields(idLike)
	}

	return info, nil
}
