package verify

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"testing"
	"time"

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

// generateTestCert generates a self-signed PEM certificate for testing.
func generateTestCert(t *testing.T) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))
}

// buildTestRekorJSON builds a valid cosignRekorEntry JSON string for testing.
func buildTestRekorJSON(t *testing.T) string {
	t.Helper()

	bodyJSON := `{"apiVersion":"0.0.1","kind":"hashedrekord"}`
	entry := cosignRekorEntry{
		SignedEntryTimestamp: base64.StdEncoding.EncodeToString([]byte("test-set")),
	}
	entry.Payload.Body = base64.StdEncoding.EncodeToString([]byte(bodyJSON))
	entry.Payload.IntegratedTime = 1700000000
	entry.Payload.LogIndex = 42
	entry.Payload.LogID = "deadbeef"

	data, err := json.Marshal(entry)
	require.NoError(t, err)
	return string(data)
}

func TestBuildBundleFromCosignAnnotations(t *testing.T) {
	t.Parallel()

	certPEM := generateTestCert(t)
	chainPEM := generateTestCert(t)
	rekorJSON := buildTestRekorJSON(t)
	payload := []byte(`{"critical":{"image":{"docker-manifest-digest":"sha256:abc123"}}}`)

	tests := []struct {
		name        string
		annotations map[string]string
		payload     []byte
		wantErr     string
	}{
		{
			name: "all annotations valid",
			annotations: map[string]string{
				cosignSignatureKey:   base64.StdEncoding.EncodeToString([]byte("test-sig")),
				cosignCertificateKey: certPEM,
				cosignChainKey:       chainPEM,
				cosignBundleKey:      rekorJSON,
			},
			payload: payload,
		},
		{
			name: "valid without chain",
			annotations: map[string]string{
				cosignSignatureKey:   base64.StdEncoding.EncodeToString([]byte("test-sig")),
				cosignCertificateKey: certPEM,
				cosignBundleKey:      rekorJSON,
			},
			payload: payload,
		},
		{
			name: "missing signature",
			annotations: map[string]string{
				cosignCertificateKey: certPEM,
				cosignBundleKey:      rekorJSON,
			},
			payload: payload,
			wantErr: "missing " + cosignSignatureKey,
		},
		{
			name: "missing certificate",
			annotations: map[string]string{
				cosignSignatureKey: base64.StdEncoding.EncodeToString([]byte("test-sig")),
				cosignBundleKey:    rekorJSON,
			},
			payload: payload,
			wantErr: "missing " + cosignCertificateKey,
		},
		{
			name: "missing rekor bundle",
			annotations: map[string]string{
				cosignSignatureKey:   base64.StdEncoding.EncodeToString([]byte("test-sig")),
				cosignCertificateKey: certPEM,
			},
			payload: payload,
			wantErr: "missing " + cosignBundleKey,
		},
		{
			name: "invalid signature base64",
			annotations: map[string]string{
				cosignSignatureKey:   "not-valid-base64!!!",
				cosignCertificateKey: certPEM,
				cosignBundleKey:      rekorJSON,
			},
			payload: payload,
			wantErr: "failed to decode signature base64",
		},
		{
			name: "invalid certificate PEM",
			annotations: map[string]string{
				cosignSignatureKey:   base64.StdEncoding.EncodeToString([]byte("test-sig")),
				cosignCertificateKey: "not-a-pem-block",
				cosignBundleKey:      rekorJSON,
			},
			payload: payload,
			wantErr: "failed to parse leaf certificate",
		},
		{
			name: "invalid rekor JSON",
			annotations: map[string]string{
				cosignSignatureKey:   base64.StdEncoding.EncodeToString([]byte("test-sig")),
				cosignCertificateKey: certPEM,
				cosignBundleKey:      "not-json",
			},
			payload: payload,
			wantErr: "failed to parse rekor entry JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b, err := buildBundleFromCosignAnnotations(tt.annotations, tt.payload)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, b)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, b)

			// Verify the bundle has v0.1 media type
			assert.Equal(t, "application/vnd.dev.sigstore.bundle+json;version=0.1", b.MediaType)
		})
	}
}

func TestParsePEMCertificates(t *testing.T) {
	t.Parallel()

	cert1 := generateTestCert(t)
	cert2 := generateTestCert(t)

	tests := []struct {
		name      string
		pemData   string
		wantCount int
		wantErr   string
	}{
		{
			name:      "single certificate",
			pemData:   cert1,
			wantCount: 1,
		},
		{
			name:      "multiple certificates",
			pemData:   cert1 + cert2,
			wantCount: 2,
		},
		{
			name:    "empty string",
			pemData: "",
			wantErr: "no CERTIFICATE blocks found",
		},
		{
			name:    "no certificate blocks",
			pemData: "not a PEM block at all",
			wantErr: "no CERTIFICATE blocks found",
		},
		{
			name: "non-CERTIFICATE PEM block",
			pemData: string(pem.EncodeToMemory(&pem.Block{
				Type:  "RSA PRIVATE KEY",
				Bytes: []byte("fake-key-data"),
			})),
			wantErr: "no CERTIFICATE blocks found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			certs, err := parsePEMCertificates(tt.pemData)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Nil(t, certs)
				return
			}
			require.NoError(t, err)
			assert.Len(t, certs, tt.wantCount)
			for _, c := range certs {
				assert.NotEmpty(t, c.RawBytes)
			}
		})
	}
}

func TestVerifySimpleSigningBinding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		payload        []byte
		expectedDigest ociv1.Hash
		wantErr        string
	}{
		{
			name:    "matching digest",
			payload: []byte(`{"critical":{"image":{"docker-manifest-digest":"sha256:abc123"}}}`),
			expectedDigest: ociv1.Hash{
				Algorithm: "sha256",
				Hex:       "abc123",
			},
		},
		{
			name:    "mismatched digest",
			payload: []byte(`{"critical":{"image":{"docker-manifest-digest":"sha256:abc123"}}}`),
			expectedDigest: ociv1.Hash{
				Algorithm: "sha256",
				Hex:       "different",
			},
			wantErr: "SimpleSigning digest mismatch",
		},
		{
			name:    "invalid JSON",
			payload: []byte(`not json`),
			expectedDigest: ociv1.Hash{
				Algorithm: "sha256",
				Hex:       "abc123",
			},
			wantErr: "failed to parse SimpleSigning payload",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := verifySimpleSigningBinding(tt.payload, tt.expectedDigest)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}
