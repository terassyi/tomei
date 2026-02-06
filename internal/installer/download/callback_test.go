package download

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCallbackFromContext_Progress(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(ctx context.Context) context.Context
		wantNil bool
	}{
		{
			name:    "returns nil when no callback set",
			setup:   func(ctx context.Context) context.Context { return ctx },
			wantNil: true,
		},
		{
			name: "returns callback when set",
			setup: func(ctx context.Context) context.Context {
				return WithCallback(ctx, ProgressCallback(func(downloaded, total int64) {}))
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setup(context.Background())
			cb := CallbackFromContext[ProgressCallback](ctx)
			if tt.wantNil {
				assert.Nil(t, cb)
			} else {
				assert.NotNil(t, cb)
			}
		})
	}
}

func TestCallbackFromContext_Output(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(ctx context.Context) context.Context
		wantNil bool
	}{
		{
			name:    "returns nil when no callback set",
			setup:   func(ctx context.Context) context.Context { return ctx },
			wantNil: true,
		},
		{
			name: "returns callback when set",
			setup: func(ctx context.Context) context.Context {
				return WithCallback(ctx, OutputCallback(func(line string) {}))
			},
			wantNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.setup(context.Background())
			cb := CallbackFromContext[OutputCallback](ctx)
			if tt.wantNil {
				assert.Nil(t, cb)
			} else {
				assert.NotNil(t, cb)
			}
		})
	}
}

func TestCallbackFromContext_ProgressInvocable(t *testing.T) {
	var called bool
	var gotDownloaded, gotTotal int64

	ctx := WithCallback(context.Background(), ProgressCallback(func(downloaded, total int64) {
		called = true
		gotDownloaded = downloaded
		gotTotal = total
	}))

	cb := CallbackFromContext[ProgressCallback](ctx)
	assert.NotNil(t, cb)

	cb(100, 200)
	assert.True(t, called)
	assert.Equal(t, int64(100), gotDownloaded)
	assert.Equal(t, int64(200), gotTotal)
}

func TestCallbackFromContext_OutputInvocable(t *testing.T) {
	var called bool
	var gotLine string

	ctx := WithCallback(context.Background(), OutputCallback(func(line string) {
		called = true
		gotLine = line
	}))

	cb := CallbackFromContext[OutputCallback](ctx)
	assert.NotNil(t, cb)

	cb("hello")
	assert.True(t, called)
	assert.Equal(t, "hello", gotLine)
}

func TestCallbackFromContext_TypeIsolation(t *testing.T) {
	ctx := WithCallback(context.Background(), ProgressCallback(func(downloaded, total int64) {}))

	// ProgressCallback is set, but OutputCallback should be nil
	assert.NotNil(t, CallbackFromContext[ProgressCallback](ctx))
	assert.Nil(t, CallbackFromContext[OutputCallback](ctx))
}
