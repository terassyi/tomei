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
