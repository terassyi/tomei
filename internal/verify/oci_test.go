package verify

import (
	"fmt"
	"net/http"
	"testing"

	ociv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCosignSigTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		digest ociv1.Hash
		want   string
	}{
		{
			name:   "sha256 digest",
			digest: ociv1.Hash{Algorithm: "sha256", Hex: "abc123def456"},
			want:   "sha256-abc123def456.sig",
		},
		{
			name:   "sha512 digest",
			digest: ociv1.Hash{Algorithm: "sha512", Hex: "deadbeef"},
			want:   "sha512-deadbeef.sig",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CosignSigTag(tt.digest)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsNotFoundError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "transport error 404",
			err:  &transport.Error{StatusCode: http.StatusNotFound},
			want: true,
		},
		{
			name: "transport error 403",
			err:  &transport.Error{StatusCode: http.StatusForbidden},
			want: false,
		},
		{
			name: "transport error 500",
			err:  &transport.Error{StatusCode: http.StatusInternalServerError},
			want: false,
		},
		{
			name: "non-transport error",
			err:  fmt.Errorf("network timeout"),
			want: false,
		},
		{
			name: "wrapped transport error 404",
			err:  fmt.Errorf("fetch failed: %w", &transport.Error{StatusCode: http.StatusNotFound}),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isNotFoundError(tt.err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseBundle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "invalid JSON",
			data:    []byte("not json"),
			wantErr: "failed to parse sigstore bundle JSON",
		},
		{
			name:    "empty JSON object",
			data:    []byte(`{}`),
			wantErr: "failed to create sigstore bundle",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := parseBundle(tt.data)
			require.Error(t, err)
			assert.Nil(t, b)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
