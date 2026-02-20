package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

// ExtractFirstPartyDeps reads cue.mod/module.cue from the given cue.mod directory
// and returns the list of first-party (tomei.terassyi.net) module dependencies.
// Returns nil (no error) if the directory does not exist.
func ExtractFirstPartyDeps(cueModDir string) ([]module.Version, error) {
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

	var deps []module.Version
	for modPath, dep := range f.Deps {
		if IsFirstParty(modPath) {
			v, err := module.NewVersion(modPath, dep.Version)
			if err != nil {
				return nil, fmt.Errorf("invalid module version %s@%s: %w", modPath, dep.Version, err)
			}
			deps = append(deps, v)
		}
	}

	// Sort for deterministic output
	slices.SortFunc(deps, module.Version.Compare)

	return deps, nil
}
