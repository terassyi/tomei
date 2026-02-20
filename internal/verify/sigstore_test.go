package verify

import (
	"testing"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVerifySigstoreBundle_InvalidJSON(t *testing.T) {
	t.Parallel()

	v := &SigstoreVerifier{}
	digest := v1.Hash{Algorithm: "sha256", Hex: "abcdef0123456789"}

	err := v.verifySigstoreBundle([]byte("not json"), digest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse sigstore bundle")
}

func TestVerifySigstoreBundle_EmptyJSON(t *testing.T) {
	t.Parallel()

	v := &SigstoreVerifier{}
	digest := v1.Hash{Algorithm: "sha256", Hex: "abcdef0123456789"}

	// Valid JSON but empty object — NewBundle should fail or getTrustedRoot will fail.
	// The empty protobuf bundle will fail at bundle.NewBundle.
	err := v.verifySigstoreBundle([]byte(`{}`), digest)
	require.Error(t, err)
}

func TestNewSigstoreVerifier_ValidRegistry(t *testing.T) {
	t.Parallel()

	sv, err := NewSigstoreVerifier("tomei.terassyi.net=ghcr.io/terassyi")
	require.NoError(t, err)
	assert.NotNil(t, sv)
	assert.NotNil(t, sv.refResolver)
}
