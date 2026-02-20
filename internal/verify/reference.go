package verify

import (
	"fmt"

	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/module"
	"github.com/google/go-containerregistry/pkg/name"
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

// Resolve converts a module.Version to a validated OCI reference.
func (r *ReferenceResolver) Resolve(dep module.Version) (name.Reference, error) {
	loc, ok := r.resolver.ResolveToLocation(dep.BasePath(), dep.Version())
	if !ok {
		return nil, fmt.Errorf("cannot resolve module %s to registry location", dep)
	}

	ref, err := name.NewTag(fmt.Sprintf("%s/%s:%s", loc.Host, loc.Repository, loc.Tag))
	if err != nil {
		return nil, fmt.Errorf("invalid OCI reference for module %s: %w", dep, err)
	}
	return ref, nil
}
