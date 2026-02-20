package verify

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSigstoreVerifier_ValidRegistry(t *testing.T) {
	t.Parallel()

	sv, err := NewSigstoreVerifier("tomei.terassyi.net=ghcr.io/terassyi")
	require.NoError(t, err)
	assert.NotNil(t, sv)
	assert.NotNil(t, sv.refResolver)
}
