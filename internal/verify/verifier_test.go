package verify

import (
	"context"
	"testing"

	"cuelang.org/go/mod/module"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsFirstParty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		modulePath string
		want       bool
	}{
		{
			name:       "first-party module with major version",
			modulePath: "tomei.terassyi.net@v0",
			want:       true,
		},
		{
			name:       "first-party submodule",
			modulePath: "tomei.terassyi.net/schema@v0",
			want:       true,
		},
		{
			name:       "first-party presets submodule",
			modulePath: "tomei.terassyi.net/presets/go@v0",
			want:       true,
		},
		{
			name:       "third-party module",
			modulePath: "example.com@v0",
			want:       false,
		},
		{
			name:       "empty module path",
			modulePath: "",
			want:       false,
		},
		{
			name:       "partial match prefix",
			modulePath: "tomei.terassyi.net.evil.com@v0",
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsFirstParty(tt.modulePath)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNoopVerifier(t *testing.T) {
	t.Parallel()

	reason := "testing"
	v := NewNoopVerifier(reason)

	deps := []module.Version{
		module.MustNewVersion("tomei.terassyi.net@v0", "v0.0.3"),
		module.MustNewVersion("tomei.terassyi.net/presets/go@v0", "v0.0.3"),
	}

	results, err := v.Verify(context.Background(), deps)
	require.NoError(t, err)
	require.Len(t, results, len(deps))

	for i, r := range results {
		assert.Equal(t, deps[i].Path(), r.Module.Path())
		assert.Equal(t, deps[i].Version(), r.Module.Version())
		assert.False(t, r.Verified)
		assert.True(t, r.Skipped)
		assert.Equal(t, reason, r.SkipReason)
	}
}

func TestNoopVerifier_EmptyDeps(t *testing.T) {
	t.Parallel()

	v := NewNoopVerifier("no deps")
	results, err := v.Verify(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}
