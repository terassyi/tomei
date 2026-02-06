package download

import "context"

// OutputCallback is called for each line of command output.
type OutputCallback func(line string)

// Callback is a type constraint for callback functions that can be stored in context.
type Callback interface {
	ProgressCallback | OutputCallback
}

type callbackKey[T Callback] struct{}

// WithCallback returns a context with the given callback.
func WithCallback[T Callback](ctx context.Context, cb T) context.Context {
	return context.WithValue(ctx, callbackKey[T]{}, cb)
}

// CallbackFromContext extracts the callback from context, or the zero value.
func CallbackFromContext[T Callback](ctx context.Context) T {
	if cb, ok := ctx.Value(callbackKey[T]{}).(T); ok {
		return cb
	}
	var zero T
	return zero
}
