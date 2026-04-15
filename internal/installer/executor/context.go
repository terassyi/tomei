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

type privilegedKey struct{}

// WithPrivileged returns a context indicating that commands should be
// executed with elevated privileges (sudo). Used by the command executor
// to prefix commands with "sudo -n sh -c" instead of "sh -c".
func WithPrivileged(ctx context.Context) context.Context {
	return context.WithValue(ctx, privilegedKey{}, true)
}

// PrivilegedFromContext reports whether the context indicates privileged execution.
func PrivilegedFromContext(ctx context.Context) bool {
	v, _ := ctx.Value(privilegedKey{}).(bool)
	return v
}
