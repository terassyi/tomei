package executor

import (
	"context"

	"github.com/terassyi/tomei/internal/resource"
)

type actionKey struct{}

// WithAction returns a context carrying the given action type.
func WithAction(ctx context.Context, action resource.ActionType) context.Context {
	return context.WithValue(ctx, actionKey{}, action)
}

// ActionFromContext extracts the action type from context, or the zero value.
func ActionFromContext(ctx context.Context) resource.ActionType {
	if v, ok := ctx.Value(actionKey{}).(resource.ActionType); ok {
		return v
	}
	return ""
}

type oldBinPathKey struct{}

// WithOldBinPath returns a context carrying the old BinPath for symlink cleanup.
func WithOldBinPath(ctx context.Context, binPath string) context.Context {
	return context.WithValue(ctx, oldBinPathKey{}, binPath)
}

// OldBinPathFromContext extracts the old BinPath from context, or empty string.
func OldBinPathFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(oldBinPathKey{}).(string); ok {
		return v
	}
	return ""
}

type oldPackagesKey struct{}

// WithOldPackages returns a context carrying the package list recorded in
// the prior state. Used by SystemPackageSet upgrade so the installer can
// uninstall packages that were dropped from the new spec — without this
// signal, the generic executor's "upgrade = install" flow leaves dropped
// packages on the host with no state tracking.
func WithOldPackages(ctx context.Context, packages []string) context.Context {
	return context.WithValue(ctx, oldPackagesKey{}, packages)
}

// OldPackagesFromContext returns the prior-state package list, or nil if
// the context does not carry one (i.e., this is a first-time Install, or
// the state type does not expose GetPackages).
func OldPackagesFromContext(ctx context.Context) []string {
	if v, ok := ctx.Value(oldPackagesKey{}).([]string); ok {
		return v
	}
	return nil
}
