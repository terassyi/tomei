package verify

import (
	"fmt"
	"strings"

	"cuelang.org/go/mod/modconfig"
)

// ReferenceResolver converts CUE module dependencies to OCI references.
type ReferenceResolver struct {
	resolver *modconfig.Resolver
}

// NewReferenceResolver creates a ReferenceResolver for the given CUE_REGISTRY value.
func NewReferenceResolver(cueRegistry string) (*ReferenceResolver, error) {
	resolver, err := modconfig.NewResolver(&modconfig.Config{
		CUERegistry: cueRegistry,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create registry resolver: %w", err)
	}
	return &ReferenceResolver{resolver: resolver}, nil
}

// Resolve converts a ModuleDependency to an OCI reference string (e.g. "ghcr.io/repo:tag").
func (r *ReferenceResolver) Resolve(dep ModuleDependency) (string, error) {
	basePath := splitModulePath(dep.ModulePath)

	loc, ok := r.resolver.ResolveToLocation(basePath, dep.Version)
	if !ok {
		return "", fmt.Errorf("cannot resolve module %s to registry location", dep.ModulePath)
	}

	return fmt.Sprintf("%s/%s:%s", loc.Host, loc.Repository, loc.Tag), nil
}

// splitModulePath strips the major version suffix (@vN) from a module path.
func splitModulePath(modulePath string) string {
	if i := strings.LastIndex(modulePath, "@"); i >= 0 {
		return modulePath[:i]
	}
	return modulePath
}
