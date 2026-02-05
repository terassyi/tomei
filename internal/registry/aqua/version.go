// Package aqua provides version override functionality for aqua-registry packages.
package aqua

import (
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// semverPattern matches semver constraint expressions like semver("< 1.0.0") or semver(">= 2.0.0").
var semverPattern = regexp.MustCompile(`^semver\("([^"]+)"\)$`)

// matchVersionConstraint checks if the given version matches the constraint.
// Returns true if:
//   - constraint is "true" or empty string
//   - constraint is semver("...") and version satisfies the constraint
func matchVersionConstraint(constraint, version string) bool {
	// "true" or empty always matches
	if constraint == "true" || constraint == "" {
		return true
	}

	// Check for semver("...") format
	matches := semverPattern.FindStringSubmatch(constraint)
	if len(matches) != 2 {
		// Unknown constraint format, don't match
		return false
	}

	// Parse semver constraint
	semverConstraint := matches[1]
	c, err := semver.NewConstraint(semverConstraint)
	if err != nil {
		return false
	}

	// Parse version (strip "v" prefix if present)
	versionStr := strings.TrimPrefix(version, "v")
	v, err := semver.NewVersion(versionStr)
	if err != nil {
		return false
	}

	return c.Check(v)
}

// ApplyVersionOverrides applies version-specific overrides to the package info.
// It returns a new PackageInfo with the first matching override applied.
// If no override matches, the original info is returned unchanged.
func ApplyVersionOverrides(info *PackageInfo, version string) *PackageInfo {
	if len(info.VersionOverrides) == 0 {
		return info
	}

	// Create a shallow copy
	result := *info

	for _, override := range info.VersionOverrides {
		if matchVersionConstraint(override.VersionConstraint, version) {
			// Apply override fields if they are set
			if override.Asset != "" {
				result.Asset = override.Asset
			}
			if override.Format != "" {
				result.Format = override.Format
			}
			if override.Checksum != nil {
				result.Checksum = override.Checksum
			}
			if override.Replacements != nil {
				result.Replacements = override.Replacements
			}
			if override.Overrides != nil {
				result.Overrides = override.Overrides
			}
			if override.SupportedEnvs != nil {
				result.SupportedEnvs = override.SupportedEnvs
			}
			// Only apply the first matching override
			break
		}
	}

	return &result
}
