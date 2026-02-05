// Package aqua provides OS/Arch override functionality for aqua-registry packages.
package aqua

// applyOSOverrides applies OS/Arch-specific overrides to the package info.
// It returns a new PackageInfo with the first matching override applied.
// Matching is done by GOOS and GOArch fields (empty means "any").
func applyOSOverrides(info *PackageInfo, goos, goarch string) *PackageInfo {
	if len(info.Overrides) == 0 {
		return info
	}

	// Create a shallow copy
	result := *info

	for _, override := range info.Overrides {
		if matchesOS(override, goos, goarch) {
			// Apply override fields if they are set
			if override.Asset != "" {
				result.Asset = override.Asset
			}
			if override.Format != "" {
				result.Format = override.Format
			}
			if override.Replacements != nil {
				result.Replacements = override.Replacements
			}
			// Only apply the first matching override
			break
		}
	}

	return &result
}

// matchesOS checks if the override matches the given GOOS and GOARCH.
// Empty GOOS or GOArch in override means "match any".
func matchesOS(override Override, goos, goarch string) bool {
	goosMatch := override.GOOS == "" || override.GOOS == goos
	goarchMatch := override.GOArch == "" || override.GOArch == goarch
	return goosMatch && goarchMatch
}

// applyReplacement returns the replacement value for the given key.
// If no replacement is found, the original key is returned.
func applyReplacement(replacements map[string]string, key string) string {
	if replacements == nil {
		return key
	}
	if replacement, ok := replacements[key]; ok {
		return replacement
	}
	return key
}
