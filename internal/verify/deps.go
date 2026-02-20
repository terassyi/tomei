package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"cuelang.org/go/mod/modfile"
)

// ExtractFirstPartyDeps reads cue.mod/module.cue from the given cue.mod directory
// and returns the list of first-party (tomei.terassyi.net) module dependencies.
// Returns nil (no error) if the directory does not exist.
func ExtractFirstPartyDeps(cueModDir string) ([]ModuleDependency, error) {
	moduleCuePath := filepath.Join(cueModDir, "module.cue")

	data, err := os.ReadFile(moduleCuePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read module.cue: %w", err)
	}

	f, err := modfile.Parse(data, moduleCuePath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse module.cue: %w", err)
	}

	var deps []ModuleDependency
	for modPath, dep := range f.Deps {
		if IsFirstParty(modPath) {
			deps = append(deps, ModuleDependency{
				ModulePath: modPath,
				Version:    dep.Version,
			})
		}
	}

	// Sort for deterministic output
	slices.SortFunc(deps, func(a, b ModuleDependency) int {
		return strings.Compare(a.ModulePath, b.ModulePath)
	})

	return deps, nil
}
