package cuemod

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"cuelang.org/go/mod/modfile"
	"github.com/terassyi/tomei/internal/verify"
)

// UpdateResult represents the result of updating a single dependency.
type UpdateResult struct {
	ModulePath string
	OldVersion string
	NewVersion string
	Updated    bool
}

// ParseModuleFile reads and parses cue.mod/module.cue from the given cue.mod directory.
// Unlike verify.ParseModuleFile, this returns a user-friendly error when the file is missing.
func ParseModuleFile(cueModDir string) (*modfile.File, error) {
	f, err := verify.ParseModuleFile(cueModDir)
	if err != nil {
		return nil, err
	}
	if f == nil {
		return nil, fmt.Errorf("module.cue not found in %s (run 'tomei cue init' first)", cueModDir)
	}
	return f, nil
}

// UpdateDeps updates first-party (tomei.terassyi.net) dependencies in the module file
// to the given latest version. Returns the list of update results.
func UpdateDeps(f *modfile.File, latestVersion string) ([]UpdateResult, error) {
	// Collect first-party module paths for deterministic order.
	var firstPartyPaths []string
	for modPath := range f.Deps {
		if verify.IsFirstParty(modPath) {
			firstPartyPaths = append(firstPartyPaths, modPath)
		}
	}

	if len(firstPartyPaths) == 0 {
		return nil, fmt.Errorf("no first-party tomei dependencies found in module.cue")
	}

	slices.Sort(firstPartyPaths)

	var results []UpdateResult
	for _, modPath := range firstPartyPaths {
		dep := f.Deps[modPath]
		oldVersion := dep.Version
		updated := oldVersion != latestVersion
		if updated {
			dep.Version = latestVersion
		}
		results = append(results, UpdateResult{
			ModulePath: modPath,
			OldVersion: oldVersion,
			NewVersion: latestVersion,
			Updated:    updated,
		})
	}

	return results, nil
}

// FormatModuleFile formats a parsed module file back to CUE source bytes.
func FormatModuleFile(f *modfile.File) ([]byte, error) {
	data, err := modfile.Format(f)
	if err != nil {
		return nil, fmt.Errorf("failed to format module.cue: %w", err)
	}
	return data, nil
}

// WriteModuleFileAtomic atomically writes module.cue in the given cue.mod directory.
// It writes to a temporary file first, then renames for atomicity.
func WriteModuleFileAtomic(cueModDir string, data []byte) error {
	moduleCuePath := filepath.Join(cueModDir, "module.cue")
	tmpPath := moduleCuePath + ".tmp"

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temporary module.cue: %w", err)
	}

	if err := os.Rename(tmpPath, moduleCuePath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename temporary module.cue: %w", err)
	}

	return nil
}

// HasVendoredModules returns true if vendored tomei modules exist under the directory.
func HasVendoredModules(dir string) bool {
	vendorPath := filepath.Join(dir, "cue.mod", "pkg", verify.FirstPartyPrefix)
	info, err := os.Stat(vendorPath)
	return err == nil && info.IsDir()
}

// AnyUpdated returns true if any dependency was actually updated.
func AnyUpdated(results []UpdateResult) bool {
	return slices.ContainsFunc(results, func(r UpdateResult) bool {
		return r.Updated
	})
}
