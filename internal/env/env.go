package env

import (
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/resource"
)

// Generate produces environment variable statements for the given runtimes and installers.
// It collects env vars from each runtime and builds a PATH statement
// with BinDir, ToolBinPath from runtimes, BinDir from installers, plus the user bin directory.
// PATH ordering: userBinDir > runtime BinDir/ToolBinPath > installer BinDir > $PATH.
func Generate(runtimes map[string]*resource.RuntimeState, installers map[string]*resource.InstallerState, userBinDir string, f Formatter) []string {
	var lines []string
	var pathDirs []string

	// Add user bin dir first (highest priority in PATH)
	pathDirs = append(pathDirs, toShellPath(userBinDir))

	// Process each runtime in sorted order (deterministic output)
	for _, name := range sortedKeys(runtimes) {
		rs := runtimes[name]

		// Export env vars in sorted key order
		for _, key := range sortedKeys(rs.Env) {
			lines = append(lines, f.ExportVar(key, toShellPath(rs.Env[key])))
		}

		// Collect PATH directories
		if rs.BinDir != "" {
			pathDirs = append(pathDirs, toShellPath(rs.BinDir))
		}
		if rs.ToolBinPath != "" && rs.ToolBinPath != rs.BinDir {
			pathDirs = append(pathDirs, toShellPath(rs.ToolBinPath))
		}
	}

	// Add installer BinDir entries (after runtimes, before $PATH)
	for _, name := range sortedKeys(installers) {
		if installers[name].BinDir != "" {
			pathDirs = append(pathDirs, toShellPath(installers[name].BinDir))
		}
	}

	// Deduplicate and add PATH statement
	pathDirs = dedupStrings(pathDirs)
	if len(pathDirs) > 0 {
		lines = append(lines, f.ExportPath(pathDirs))
	}

	return lines
}

// GenerateCUERegistry returns a CUE_REGISTRY export statement if cueModExists is true
// and cueRegistry is non-empty.
// This enables CUE tooling (cue eval, LSP) to resolve tomei module imports.
func GenerateCUERegistry(cueModExists bool, cueRegistry string, f Formatter) string {
	if !cueModExists || cueRegistry == "" {
		return ""
	}
	return f.ExportVar(config.EnvCUERegistry, cueRegistry)
}

// toShellPath converts an absolute path under $HOME to $HOME/... form for shell portability.
// e.g., "/home/user/go/bin" → "$HOME/go/bin"
// Paths not under $HOME are returned as-is.
func toShellPath(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && strings.HasPrefix(p, home+"/") {
		return shellHome + "/" + p[len(home)+1:]
	}
	if p == home {
		return shellHome
	}
	return p
}

// sortedKeys returns the keys of a map sorted alphabetically.
func sortedKeys[V any](m map[string]V) []string {
	keys := slices.Collect(maps.Keys(m))
	slices.Sort(keys)
	return keys
}

// dedupStrings removes duplicate strings while preserving order.
func dedupStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	result := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
