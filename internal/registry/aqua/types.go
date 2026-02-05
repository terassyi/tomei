// Package aqua provides types and functions for interacting with aqua-registry.
//
// The package definition types (PackageInfo, FileSpec, ChecksumSpec, VersionOverride, Override)
// are ported from aqua's registry configuration schema.
//
// Reference:
//   - aqua source: https://github.com/aquaproj/aqua/blob/main/pkg/config/registry/package_info.go
//   - aqua-registry: https://github.com/aquaproj/aqua-registry
//   - Documentation: https://aquaproj.github.io/docs/reference/registry-config/
//
// See NOTICE file for attribution.
package aqua

import (
	"fmt"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// RegistryRef represents a reference to an aqua-registry version (tag).
// Format: "vX.Y.Z" (e.g., "v4.465.0")
type RegistryRef string

// String returns the string representation of the registry ref.
func (r RegistryRef) String() string {
	return string(r)
}

// IsEmpty returns true if the registry ref is empty.
func (r RegistryRef) IsEmpty() bool {
	return r == ""
}

// Validate checks if the registry ref is valid.
// A valid ref must:
//   - Start with "v"
//   - Be a valid semver (e.g., "v4.465.0")
func (r RegistryRef) Validate() error {
	if r.IsEmpty() {
		return fmt.Errorf("registry ref is empty")
	}

	s := string(r)
	if !strings.HasPrefix(s, "v") {
		return fmt.Errorf("registry ref must start with 'v': %s", s)
	}

	// Remove 'v' prefix and validate as semver
	version := strings.TrimPrefix(s, "v")
	if _, err := semver.NewVersion(version); err != nil {
		return fmt.Errorf("invalid registry ref format (expected vX.Y.Z): %s", s)
	}

	return nil
}

// The following types are ported from aqua's registry configuration.
// Source: https://github.com/aquaproj/aqua/blob/main/pkg/config/registry/package_info.go
// License: MIT (https://github.com/aquaproj/aqua/blob/main/LICENSE)

// PackageInfo represents a package definition from aqua registry.yaml.
type PackageInfo struct {
	Type             string            `yaml:"type"`
	RepoOwner        string            `yaml:"repo_owner"`
	RepoName         string            `yaml:"repo_name"`
	Description      string            `yaml:"description,omitempty"`
	Asset            string            `yaml:"asset,omitempty"`
	URL              string            `yaml:"url,omitempty"`
	Format           string            `yaml:"format,omitempty"`
	Files            []FileSpec        `yaml:"files,omitempty"`
	Replacements     map[string]string `yaml:"replacements,omitempty"`
	Checksum         *ChecksumSpec     `yaml:"checksum,omitempty"`
	VersionOverrides []VersionOverride `yaml:"version_overrides,omitempty"`
	SupportedEnvs    []string          `yaml:"supported_envs,omitempty"`
	Overrides        []Override        `yaml:"overrides,omitempty"`
}

// FileSpec specifies a file to install from the archive.
type FileSpec struct {
	Name string `yaml:"name"`
	Src  string `yaml:"src,omitempty"`
}

// ChecksumSpec specifies checksum verification settings.
type ChecksumSpec struct {
	Enabled   bool   `yaml:"enabled,omitempty"`
	Type      string `yaml:"type,omitempty"`      // e.g., "github_release"
	Asset     string `yaml:"asset,omitempty"`     // checksum file asset name template
	Algorithm string `yaml:"algorithm,omitempty"` // e.g., "sha256"
}

// VersionOverride specifies version-specific configuration overrides.
type VersionOverride struct {
	VersionConstraint string            `yaml:"version_constraint"`
	Asset             string            `yaml:"asset,omitempty"`
	Format            string            `yaml:"format,omitempty"`
	Checksum          *ChecksumSpec     `yaml:"checksum,omitempty"`
	Replacements      map[string]string `yaml:"replacements,omitempty"`
	Overrides         []Override        `yaml:"overrides,omitempty"`
	SupportedEnvs     []string          `yaml:"supported_envs,omitempty"`
	Rosetta2          bool              `yaml:"rosetta2,omitempty"`
}

// Override specifies OS/Arch-specific configuration overrides.
type Override struct {
	GOOS         string            `yaml:"goos,omitempty"`
	GOArch       string            `yaml:"goarch,omitempty"`
	Format       string            `yaml:"format,omitempty"`
	Asset        string            `yaml:"asset,omitempty"`
	Replacements map[string]string `yaml:"replacements,omitempty"`
}
