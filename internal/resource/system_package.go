package resource

import (
	"fmt"
	"time"
)

// SystemPackageRepositorySpec defines a third-party repository.
type SystemPackageRepositorySpec struct {
	InstallerRef string       `json:"installerRef"`
	Source       SourceConfig `json:"source"`
}

// Validate validates the SystemPackageRepositorySpec.
func (s *SystemPackageRepositorySpec) Validate() error {
	if s.InstallerRef == "" {
		return fmt.Errorf("installerRef is required")
	}
	if s.Source.URL == "" {
		return fmt.Errorf("source.url is required")
	}
	return nil
}

// Dependencies returns the resources this repository depends on.
func (s *SystemPackageRepositorySpec) Dependencies() []Ref {
	return []Ref{
		{Kind: KindSystemInstaller, Name: s.InstallerRef},
	}
}

// SystemPackageRepository is a concrete resource type for system package repositories.
type SystemPackageRepository struct {
	BaseResource
	SystemPackageRepositorySpec *SystemPackageRepositorySpec `json:"spec"`
}

// Kind returns the resource kind (can be called on nil).
func (*SystemPackageRepository) Kind() Kind { return KindSystemPackageRepository }

// Spec returns the spec as Spec interface.
func (s *SystemPackageRepository) Spec() Spec { return s.SystemPackageRepositorySpec }

// SourceConfig holds repository source configuration.
type SourceConfig struct {
	URL     string            `json:"url"`
	KeyURL  string            `json:"keyUrl,omitempty"`
	KeyHash string            `json:"keyHash,omitempty"`
	Options map[string]string `json:"options,omitempty"`
}

// SystemPackageSetSpec defines a set of system packages.
type SystemPackageSetSpec struct {
	InstallerRef  string   `json:"installerRef"`
	RepositoryRef string   `json:"repositoryRef,omitempty"`
	Packages      []string `json:"packages"`
}

// Validate validates the SystemPackageSetSpec.
func (s *SystemPackageSetSpec) Validate() error {
	if s.InstallerRef == "" {
		return fmt.Errorf("installerRef is required")
	}
	if len(s.Packages) == 0 {
		return fmt.Errorf("at least one package is required")
	}
	return nil
}

// Dependencies returns the resources this package set depends on.
func (s *SystemPackageSetSpec) Dependencies() []Ref {
	deps := []Ref{
		{Kind: KindSystemInstaller, Name: s.InstallerRef},
	}
	if s.RepositoryRef != "" {
		deps = append(deps, Ref{Kind: KindSystemPackageRepository, Name: s.RepositoryRef})
	}
	return deps
}

// SystemPackageSet is a concrete resource type for system package sets.
type SystemPackageSet struct {
	BaseResource
	SystemPackageSetSpec *SystemPackageSetSpec `json:"spec"`
}

// Kind returns the resource kind (can be called on nil).
func (*SystemPackageSet) Kind() Kind { return KindSystemPackageSet }

// Spec returns the spec as Spec interface.
func (s *SystemPackageSet) Spec() Spec { return s.SystemPackageSetSpec }

// SystemPackageRepositoryState represents the state of a repository.
type SystemPackageRepositoryState struct {
	InstallerRef   string       `json:"installerRef"`
	Source         SourceConfig `json:"source"`
	InstalledFiles []string     `json:"installedFiles"`
	UpdatedAt      time.Time    `json:"updatedAt"`
}

// SystemPackageSetState represents the state of installed system packages.
type SystemPackageSetState struct {
	InstallerRef      string            `json:"installerRef"`
	RepositoryRef     string            `json:"repositoryRef,omitempty"`
	Packages          []string          `json:"packages"`
	InstalledVersions map[string]string `json:"installedVersions"`
	UpdatedAt         time.Time         `json:"updatedAt"`
}
