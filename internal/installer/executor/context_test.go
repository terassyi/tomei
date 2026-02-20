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
