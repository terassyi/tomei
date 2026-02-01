package doctor

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/terassyi/toto/internal/resource"
)

// executableBits is the Unix permission bitmask for executable files (owner/group/other execute).
const executableBits os.FileMode = 0111

// scanForUnmanaged scans all paths and returns unmanaged tools.
func (d *Doctor) scanForUnmanaged() (map[string][]UnmanagedTool, error) {
	result := make(map[string][]UnmanagedTool)

	for category, binPath := range d.scanPaths {
		tools, err := d.scanPath(category, binPath)
		if err != nil {
			// Skip non-existent directories
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		if len(tools) > 0 {
			result[category] = tools
		}
	}

	return result, nil
}

// scanPath scans a single directory for unmanaged tools.
func (d *Doctor) scanPath(category, binPath string) ([]UnmanagedTool, error) {
	entries, err := os.ReadDir(binPath)
	if err != nil {
		return nil, err
	}

	var unmanaged []UnmanagedTool

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files
		if strings.HasPrefix(name, ".") {
			continue
		}

		fullPath := filepath.Join(binPath, name)

		// Check if it's executable
		info, err := os.Stat(fullPath)
		if err != nil {
			continue
		}

		// Skip directories
		if info.IsDir() {
			continue
		}

		// Skip non-executable files (on Unix)
		if info.Mode()&executableBits == 0 {
			continue
		}

		// Check if this tool is managed by toto
		if !d.isManagedTool(name, category) {
			unmanaged = append(unmanaged, UnmanagedTool{
				Name: name,
				Path: fullPath,
			})
		}
	}

	return unmanaged, nil
}

// isManagedTool checks if a tool is managed by toto.
func (d *Doctor) isManagedTool(name, category string) bool {
	if d.state == nil {
		return false
	}

	// For runtime categories, check if it's a runtime binary (e.g., go, gofmt)
	if category != resource.ProjectName {
		if d.isRuntimeBinary(name, category) {
			return true
		}
	}

	if d.state.Tools == nil {
		return false
	}

	tool, exists := d.state.Tools[name]
	if !exists {
		return false
	}

	// For toto category, check if the tool is managed with download pattern
	if category == resource.ProjectName {
		// If tool exists in state and has no runtimeRef, it's managed by toto directly
		return tool.RuntimeRef == ""
	}

	// For runtime categories, check if the tool uses this runtime
	return tool.RuntimeRef == category
}

// isRuntimeBinary checks if a binary name is a managed runtime binary for the given runtime.
func (d *Doctor) isRuntimeBinary(name, runtimeName string) bool {
	if d.state == nil || d.state.Runtimes == nil {
		return false
	}

	runtime, exists := d.state.Runtimes[runtimeName]
	if !exists {
		return false
	}

	return slices.Contains(runtime.Binaries, name)
}
