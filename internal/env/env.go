package env

import (
	"os"
	"sort"
	"strings"

	"github.com/terassyi/tomei/internal/config"
	"github.com/terassyi/tomei/internal/resource"
)

// Generate produces environment variable statements for the given runtimes.
// It collects env vars from each runtime and builds a PATH statement
// with BinDir, ToolBinPath from runtimes plus the user bin directory.
func Generate(runtimes map[string]*resource.RuntimeState, userBinDir string, f Formatter) []string {
	var lines []string
	var pathDirs []string

	// Add user bin dir first (highest priority in PATH)
	pathDirs = append(pathDirs, toShellPath(userBinDir))

	// Sort runtime names for deterministic output
	names := make([]string, 0, len(runtimes))
	for name := range runtimes {
		names = append(names, name)
	}
	sort.Strings(names)

	// Process each runtime in sorted order
	for _, name := range names {
		rs := runtimes[name]

		// Sort env keys for deterministic output
		keys := make([]string, 0, len(rs.Env))
		for key := range rs.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		for _, key := range keys {
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
