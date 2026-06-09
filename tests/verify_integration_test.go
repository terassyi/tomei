//go:build integration

package tests

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"cuelang.org/go/mod/module"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	ociv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/static"
	"github.com/google/go-containerregistry/pkg/v1/types"
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

	// Build cosign v2 annotations: signature + PEM certificate + Rekor entry JSON.
	// These pass structural parsing (buildBundleFromCosignAnnotations) but fail
	// cryptographic verification because the certificate and signature are fake.
	dummyBytes := base64.StdEncoding.EncodeToString([]byte("test"))
	dummyCertPEM := "-----BEGIN CERTIFICATE-----\n" + dummyBytes + "\n-----END CERTIFICATE-----"
	rekorBody := validHashedrekordBodyB64(t)
	rekorJSON := fmt.Sprintf(`{
		"SignedEntryTimestamp": "%s",
		"Payload": {
			"body": "%s",
			"integratedTime": 1700000000,
			"logIndex": 1,
			"logID": "deadbeef"
		}
	}`, dummyBytes, rekorBody)

	sigLayer := static.NewLayer(sigPayload, types.OCILayer)
	sigImg := mutate.MediaType(empty.Image, types.OCIManifestSchema1)
	sigImg, err = mutate.Append(sigImg.(ociv1.Image), mutate.Addendum{
		Layer: sigLayer,
		Annotations: map[string]string{
			"dev.cosignproject.cosign/signature": "dGVzdC1zaWduYXR1cmU=",
			"dev.sigstore.cosign/certificate":    dummyCertPEM,
			"dev.sigstore.cosign/bundle":         rekorJSON,
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

// validHashedrekordBodyB64 returns a base64-encoded, signature-valid
// hashedrekord v0.0.1 entry body. sigstore-go v1.2.0 decodes and validates the
// Rekor body inside bundle.NewBundle, so a minimal stub no longer parses; the
// signature must verify against the recorded digest. The entry is
// self-contained — a generated key signs the digest and its certificate is
// embedded as the entry's public key.
func validHashedrekordBodyB64(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "rekor-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	digest := sha256.Sum256([]byte("rekor-test-artifact"))
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	require.NoError(t, err)

	body := fmt.Sprintf(
		`{"apiVersion":"0.0.1","kind":"hashedrekord","spec":{"data":{"hash":{"algorithm":"sha256","value":"%s"}},"signature":{"content":"%s","publicKey":{"content":"%s"}}}}`,
		hex.EncodeToString(digest[:]),
		base64.StdEncoding.EncodeToString(sig),
		base64.StdEncoding.EncodeToString(certPEM),
	)
	return base64.StdEncoding.EncodeToString([]byte(body))
}
