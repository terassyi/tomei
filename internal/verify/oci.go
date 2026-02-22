package verify

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	ociv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	protocommon "github.com/sigstore/protobuf-specs/gen/pb-go/common/v1"
	protorekor "github.com/sigstore/protobuf-specs/gen/pb-go/rekor/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"google.golang.org/protobuf/encoding/protojson"
)

const (
	// cosignSignatureKey is the annotation key for the base64-encoded ECDSA signature.
	cosignSignatureKey = "dev.cosignproject.cosign/signature"
	// cosignCertificateKey is the annotation key for the PEM-encoded Fulcio leaf certificate.
	cosignCertificateKey = "dev.sigstore.cosign/certificate"
	// cosignChainKey is the annotation key for the PEM-encoded certificate chain.
	cosignChainKey = "dev.sigstore.cosign/chain"
	// cosignBundleKey is the annotation key for the Rekor entry JSON (cosign v2 format).
	cosignBundleKey = "dev.sigstore.cosign/bundle"
)

// CosignSigTag returns the cosign signature tag for the given digest.
// Cosign stores signatures at sha256-<hex>.sig.
func CosignSigTag(digest ociv1.Hash) string {
	return strings.ReplaceAll(digest.String(), ":", "-") + ".sig"
}

// cosignSignature holds a parsed Sigstore bundle and its corresponding SimpleSigning payload.
type cosignSignature struct {
	Bundle               *bundle.Bundle
	SimpleSigningPayload []byte
}

// cosignSignatures holds the parsed signatures fetched from the registry
// along with the digest of the signed artifact (needed for verification binding).
type cosignSignatures struct {
	// ArtifactDigest is the digest of the OCI artifact that was signed.
	ArtifactDigest ociv1.Hash
	// Signatures is the list of parsed cosign signatures.
	Signatures []cosignSignature
}

// cosignRekorEntry represents the JSON structure of the "dev.sigstore.cosign/bundle" annotation
// in cosign v2 format. This contains a Rekor transparency log entry, not a protobuf bundle.
type cosignRekorEntry struct {
	SignedEntryTimestamp string `json:"SignedEntryTimestamp"`
	Payload              struct {
		Body           string `json:"body"`
		IntegratedTime int64  `json:"integratedTime"`
		LogIndex       int64  `json:"logIndex"`
		LogID          string `json:"logID"`
	} `json:"Payload"`
}

// rekorBodyMeta is used to extract KindVersion from the base64-decoded Rekor body.
type rekorBodyMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

// maxSignaturePayloadSize is the maximum allowed size for a cosign signature
// layer payload. Prevents memory exhaustion from malicious registries.
const maxSignaturePayloadSize = 1 << 20 // 1 MB

// fetchCosignSignatures fetches cosign signatures for the given OCI reference.
// Returns nil result if no signatures exist (unsigned artifact).
func fetchCosignSignatures(ctx context.Context, ref name.Reference) (*cosignSignatures, error) {
	// Get the manifest digest
	desc, err := remote.Head(ref, remote.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest for %s: %w", ref, err)
	}

	// Build the cosign signature tag
	sigTag := CosignSigTag(desc.Digest)
	repo := ref.Context()
	sigRef := repo.Tag(sigTag)

	// Fetch the signature image
	sigImg, err := remote.Image(sigRef, remote.WithContext(ctx))
	if err != nil {
		// Only treat "not found" as unsigned; other errors (network, auth) should propagate.
		if isNotFoundError(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to fetch signature image for %s: %w", ref, err)
	}

	// Extract signatures from manifest layer annotations
	manifest, err := sigImg.Manifest()
	if err != nil {
		return nil, fmt.Errorf("failed to get signature manifest: %w", err)
	}

	var signatures []cosignSignature
	for i, layerDesc := range manifest.Layers {
		if annotations := layerDesc.Annotations; annotations != nil {
			// Check for cosign v2 annotations (signature key present)
			if _, hasSig := annotations[cosignSignatureKey]; hasSig {
				// Fetch the layer content (SimpleSigning payload)
				layer, err := sigImg.LayerByDigest(layerDesc.Digest)
				if err != nil {
					slog.Debug("failed to fetch signature layer", "index", i, "error", err)
					continue
				}
				rc, err := layer.Uncompressed()
				if err != nil {
					slog.Debug("failed to read signature layer", "index", i, "error", err)
					continue
				}
				payload, err := io.ReadAll(io.LimitReader(rc, maxSignaturePayloadSize))
				rc.Close()
				if err != nil {
					slog.Debug("failed to read signature payload", "index", i, "error", err)
					continue
				}

				b, err := buildBundleFromCosignAnnotations(annotations, payload)
				if err != nil {
					slog.Debug("failed to build bundle from cosign v2 annotations", "index", i, "error", err)
					continue
				}
				signatures = append(signatures, cosignSignature{
					Bundle:               b,
					SimpleSigningPayload: payload,
				})
				continue
			}

			// Fallback: try protobuf bundle format (forward compatibility)
			if bundleStr, ok := annotations[cosignBundleKey]; ok {
				if len(bundleStr) > maxSignaturePayloadSize {
					slog.Warn("cosign bundle annotation exceeds size limit, skipping", "index", i)
					continue
				}
				b, err := parseBundle([]byte(bundleStr))
				if err != nil {
					slog.Debug("failed to parse sigstore bundle from annotation", "index", i, "error", err)
					continue
				}
				signatures = append(signatures, cosignSignature{
					Bundle: b,
				})
			}
		}
	}

	return &cosignSignatures{
		ArtifactDigest: desc.Digest,
		Signatures:     signatures,
	}, nil
}

// buildBundleFromCosignAnnotations constructs a v0.1 Sigstore protobuf Bundle from
// cosign v2 layer annotations and the SimpleSigning payload.
//
// Cosign v2 stores signature components as individual annotations rather than a
// single protobuf bundle. This function reassembles them into the standard bundle
// format that sigstore-go can verify.
func buildBundleFromCosignAnnotations(annotations map[string]string, payload []byte) (*bundle.Bundle, error) {
	// 1. Decode signature
	sigB64, ok := annotations[cosignSignatureKey]
	if !ok {
		return nil, fmt.Errorf("missing %s annotation", cosignSignatureKey)
	}
	sigBytes, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signature base64: %w", err)
	}

	// 2. Parse leaf certificate
	certPEM, ok := annotations[cosignCertificateKey]
	if !ok {
		return nil, fmt.Errorf("missing %s annotation", cosignCertificateKey)
	}
	certs, err := parsePEMCertificates(certPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to parse leaf certificate: %w", err)
	}

	// 3. Parse chain (optional — append to certs if present)
	if chainPEM, ok := annotations[cosignChainKey]; ok && chainPEM != "" {
		chainCerts, err := parsePEMCertificates(chainPEM)
		if err != nil {
			slog.Debug("failed to parse certificate chain, using leaf only", "error", err)
		} else {
			certs = append(certs, chainCerts...)
		}
	}

	// 4. Parse Rekor entry JSON
	rekorJSON, ok := annotations[cosignBundleKey]
	if !ok {
		return nil, fmt.Errorf("missing %s annotation", cosignBundleKey)
	}
	var rekorEntry cosignRekorEntry
	if err := json.Unmarshal([]byte(rekorJSON), &rekorEntry); err != nil {
		return nil, fmt.Errorf("failed to parse rekor entry JSON: %w", err)
	}

	// 5. Decode SET (Signed Entry Timestamp)
	set, err := base64.StdEncoding.DecodeString(rekorEntry.SignedEntryTimestamp)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signed entry timestamp: %w", err)
	}

	// 6. Decode logID (hex → bytes)
	logIDBytes, err := hex.DecodeString(rekorEntry.Payload.LogID)
	if err != nil {
		return nil, fmt.Errorf("failed to decode log ID: %w", err)
	}

	// 7. Decode body (base64 → bytes)
	bodyBytes, err := base64.StdEncoding.DecodeString(rekorEntry.Payload.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to decode rekor body: %w", err)
	}

	// 8. Parse KindVersion from body
	var bodyMeta rekorBodyMeta
	if err := json.Unmarshal(bodyBytes, &bodyMeta); err != nil {
		return nil, fmt.Errorf("failed to parse rekor body for KindVersion: %w", err)
	}

	// 9. Compute message digest (SHA256 of SimpleSigning payload)
	digest := sha256.Sum256(payload)

	// 10. Assemble protobuf Bundle (v0.1 format with InclusionPromise)
	pb := &protobundle.Bundle{
		MediaType: "application/vnd.dev.sigstore.bundle+json;version=0.1",
		VerificationMaterial: &protobundle.VerificationMaterial{
			Content: &protobundle.VerificationMaterial_X509CertificateChain{
				X509CertificateChain: &protocommon.X509CertificateChain{
					Certificates: certs,
				},
			},
			TlogEntries: []*protorekor.TransparencyLogEntry{
				{
					LogIndex: rekorEntry.Payload.LogIndex,
					LogId: &protocommon.LogId{
						KeyId: logIDBytes,
					},
					KindVersion: &protorekor.KindVersion{
						Kind:    bodyMeta.Kind,
						Version: bodyMeta.APIVersion,
					},
					IntegratedTime:    rekorEntry.Payload.IntegratedTime,
					InclusionPromise:  &protorekor.InclusionPromise{SignedEntryTimestamp: set},
					CanonicalizedBody: bodyBytes,
				},
			},
		},
		Content: &protobundle.Bundle_MessageSignature{
			MessageSignature: &protocommon.MessageSignature{
				MessageDigest: &protocommon.HashOutput{
					Algorithm: protocommon.HashAlgorithm_SHA2_256,
					Digest:    digest[:],
				},
				Signature: sigBytes,
			},
		},
	}

	b, err := bundle.NewBundle(pb)
	if err != nil {
		return nil, fmt.Errorf("failed to create sigstore bundle from cosign annotations: %w", err)
	}
	return b, nil
}

// parsePEMCertificates parses PEM-encoded certificate data into protobuf X509Certificate entries.
// Returns an error if no CERTIFICATE blocks are found.
func parsePEMCertificates(pemData string) ([]*protocommon.X509Certificate, error) {
	var certs []*protocommon.X509Certificate
	rest := []byte(pemData)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}
		certs = append(certs, &protocommon.X509Certificate{
			RawBytes: block.Bytes,
		})
	}
	if len(certs) == 0 {
		return nil, errors.New("no CERTIFICATE blocks found in PEM data")
	}
	return certs, nil
}

// parseBundle parses protobuf JSON into a validated Sigstore bundle.
// Uses protojson (not encoding/json) because protobuf oneof fields
// require protobuf-aware JSON deserialization.
// Kept for forward compatibility with future protobuf bundle format.
func parseBundle(data []byte) (*bundle.Bundle, error) {
	var pb protobundle.Bundle
	if err := protojson.Unmarshal(data, &pb); err != nil {
		return nil, fmt.Errorf("failed to parse sigstore bundle JSON: %w", err)
	}
	b, err := bundle.NewBundle(&pb)
	if err != nil {
		return nil, fmt.Errorf("failed to create sigstore bundle: %w", err)
	}
	return b, nil
}

// isNotFoundError checks if an error is an HTTP 404 from the OCI registry.
func isNotFoundError(err error) bool {
	var transportErr *transport.Error
	if errors.As(err, &transportErr) {
		return transportErr.StatusCode == http.StatusNotFound
	}
	return false
}
