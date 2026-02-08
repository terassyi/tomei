package doctor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// detectConflicts finds tools that exist in multiple locations.
func (d *Doctor) detectConflicts() ([]Conflict, error) {
	// Build a map of tool name -> locations
	toolLocations := make(map[string][]string)

	for _, binPath := range d.scanPaths {
		entries, err := os.ReadDir(binPath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}

		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}

			fullPath := filepath.Join(binPath, name)
			info, err := os.Stat(fullPath)
			if err != nil {
				continue
			}

			if info.IsDir() {
				continue
			}

			if info.Mode()&executableBits == 0 {
				continue
			}

			toolLocations[name] = append(toolLocations[name], binPath)
		}
	}

	// Find conflicts (tools in multiple locations)
	var conflicts []Conflict
	for name, locations := range toolLocations {
		if len(locations) > 1 {
			resolvedTo := resolvePathFor(name)
			conflicts = append(conflicts, Conflict{
				Name:       name,
				Locations:  locations,
				ResolvedTo: resolvedTo,
			})
		}
	}

	return conflicts, nil
}

// resolvePathFor determines which path the shell would use for a command.
func resolvePathFor(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	return path
}
