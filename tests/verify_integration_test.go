//go:build integration

package tests

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	ociv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/go-containerregistry/pkg/registry"
	"cuelang.org/go/mod/module"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/terassyi/tomei/internal/verify"
)

func TestFetchCosignSignatures_NoSignature(t *testing.T) {
	t.Parallel()

	// Start in-memory OCI registry
	reg := registry.New()
	srv := httptest.NewServer(reg)
	defer srv.Close()

	host := srv.Listener.Addr().String()

	// Push a test artifact
	img, err := random.Image(256, 1)
	require.NoError(t, err)

	ref, err := name.ParseReference(fmt.Sprintf("%s/test/module:v0.0.1", host))
	require.NoError(t, err)

	err = remote.Write(ref, img)
	require.NoError(t, err)

	// Create verifier with test registry
	refResolver, err := verify.NewReferenceResolver(fmt.Sprintf("tomei.terassyi.net=%s/test", host))
	require.NoError(t, err)

	dep := module.MustNewVersion("tomei.terassyi.net@v0", "v0.0.1")

	ociRef, err := refResolver.Resolve(dep)
	require.NoError(t, err)
	assert.Contains(t, ociRef.String(), host)

	// Verify with no signatures — should return skipped result
	v := verify.NewNoopVerifier("test")
	results, err := v.Verify(context.Background(), []module.Version{dep})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, results[0].Skipped)
}

func TestFetchCosignSignatures_WithSignature(t *testing.T) {
	t.Parallel()

	// Start in-memory OCI registry
	reg := registry.New()
	srv := httptest.NewServer(reg)
	defer srv.Close()

	host := srv.Listener.Addr().String()

	// Push a test artifact
	img, err := random.Image(256, 1)
	require.NoError(t, err)

	ref, err := name.ParseReference(fmt.Sprintf("%s/test/tomei.terassyi.net:v0.0.1", host))
	require.NoError(t, err)

	err = remote.Write(ref, img)
	require.NoError(t, err)

	// Get the digest of the pushed image
	desc, err := remote.Head(ref)
	require.NoError(t, err)

	// Create a cosign-like signature image and push to the .sig tag
	sigPayload := []byte(`{"critical":{"identity":{"docker-reference":"test"},"image":{"docker-manifest-digest":"sha256:test"},"type":"cosign container image signature"},"optional":{}}`)

	// Build a minimally valid Sigstore bundle in protobuf JSON format.
	// This passes structural validation (parseBundle) but fails
	// cryptographic verification because the certificate and signature are fake.
	dummyBytes := base64.StdEncoding.EncodeToString([]byte("test"))
	bundleJSON := []byte(fmt.Sprintf(`{
		"mediaType": "application/vnd.dev.sigstore.bundle.v0.3+json",
		"verificationMaterial": {
			"certificate": {
				"rawBytes": "%s"
			}
		},
		"messageSignature": {
			"messageDigest": {
				"algorithm": "SHA2_256",
				"digest": "%s"
			},
			"signature": "%s"
		}
	}`, dummyBytes, dummyBytes, dummyBytes))

	sigLayer := static.NewLayer(sigPayload, types.OCILayer)
	sigImg := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	sigImg, err = mutate.Append(sigImg.(ociv1.Image), mutate.Addendum{
		Layer: sigLayer,
		Annotations: map[string]string{
			"dev.cosignproject.cosign/signature": "dGVzdC1zaWduYXR1cmU=",
			"dev.sigstore.cosign/bundle":         string(bundleJSON),
		},
	})
	require.NoError(t, err)

	// Push signature to the sha256-<hex>.sig tag
	sigTag := verify.CosignSigTag(desc.Digest)
	sigRef, err := name.ParseReference(fmt.Sprintf("%s/test/tomei.terassyi.net:%s", host, sigTag))
	require.NoError(t, err)

	err = remote.Write(sigRef, sigImg)
	require.NoError(t, err)

	// Now verify we can find the signature via the SigstoreVerifier
	// (It won't actually verify the signature cryptographically,
	// but it should find it and attempt verification)
	sv, err := verify.NewSigstoreVerifier(fmt.Sprintf("tomei.terassyi.net=%s/test", host))
	require.NoError(t, err)

	dep := module.MustNewVersion("tomei.terassyi.net@v0", "v0.0.1")

	results, err := sv.Verify(context.Background(), []module.Version{dep})
	require.NoError(t, err)
	require.Len(t, results, 1)

	// The result will be skipped because the test bundle won't pass
	// actual Sigstore verification, but it should find the signature
	// and attempt verification rather than saying "no signature found"
	assert.True(t, results[0].Skipped)
	// Should NOT say "no cosign signature found" — it found one but verification failed
	assert.NotEqual(t, "no cosign signature found (unsigned module)", results[0].SkipReason)
}

