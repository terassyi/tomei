package executor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/terassyi/tomei/internal/resource"
)

func TestActionContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		action resource.ActionType
		want   resource.ActionType
	}{
		{
			name:   "ActionUpgrade",
			action: resource.ActionUpgrade,
			want:   resource.ActionUpgrade,
		},
		{
			name:   "ActionReinstall",
			action: resource.ActionReinstall,
			want:   resource.ActionReinstall,
		},
		{
			name:   "ActionInstall",
			action: resource.ActionInstall,
			want:   resource.ActionInstall,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := WithAction(context.Background(), tt.action)
			got := ActionFromContext(ctx)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestActionContext_NotSet(t *testing.T) {
	t.Parallel()
	got := ActionFromContext(context.Background())
	assert.Equal(t, resource.ActionType(""), got)
}

func TestOldBinPathContext(t *testing.T) {
	t.Parallel()
	ctx := WithOldBinPath(context.Background(), "/home/user/.local/bin/old-name")
	got := OldBinPathFromContext(ctx)
	assert.Equal(t, "/home/user/.local/bin/old-name", got)
}

func TestOldBinPathContext_Empty(t *testing.T) {
	t.Parallel()
	got := OldBinPathFromContext(context.Background())
	assert.Empty(t, got)
}

func TestOldBinDirKindContext(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		kind resource.BinDirKind
		want resource.BinDirKind
	}{
		{name: "user", kind: resource.BinDirKindUser, want: resource.BinDirKindUser},
		{name: "system", kind: resource.BinDirKindSystem, want: resource.BinDirKindSystem},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := WithOldBinDirKind(context.Background(), tt.kind)
			assert.Equal(t, tt.want, OldBinDirKindFromContext(ctx))
		})
	}
}

func TestOldBinDirKindContext_Empty(t *testing.T) {
	t.Parallel()
	// When not set, the getter returns BinDirKindUser — the same default the
	// state-side ToolState.BinDirKindOrDefault uses for pre-SUB6 state files.
	assert.Equal(t, resource.BinDirKindUser, OldBinDirKindFromContext(context.Background()))
}
