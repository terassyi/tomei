package verify

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
)

// cosignSignature represents a cosign signature extracted from an OCI registry.
type cosignSignature struct {
	// Base64Signature is the base64-encoded signature.
	Base64Signature string
	// Payload is the signed payload.
	Payload []byte
	// Bundle is the Sigstore bundle JSON (from the "dev.sigstore.cosign/bundle" annotation).
	Bundle []byte
}

// CosignSigTag returns the cosign signature tag for the given digest.
// Cosign stores signatures at sha256-<hex>.sig.
func CosignSigTag(digest v1.Hash) string {
	return strings.ReplaceAll(digest.String(), ":", "-") + ".sig"
}

// cosignSignatures holds the signatures fetched from the registry along with
// the digest of the signed artifact (needed for verification binding).
type cosignSignatures struct {
	// ArtifactDigest is the digest of the OCI artifact that was signed.
	ArtifactDigest v1.Hash
	// Signatures is the list of cosign signatures found.
	Signatures []cosignSignature
}

// maxSignaturePayloadSize is the maximum allowed size for a cosign signature
// layer payload. Prevents memory exhaustion from malicious registries.
const maxSignaturePayloadSize = 1 << 20 // 1 MB

// fetchCosignSignatures fetches cosign signatures for the given OCI reference.
// Returns nil result if no signatures exist (unsigned artifact).
func fetchCosignSignatures(ctx context.Context, ociRef string) (*cosignSignatures, error) {
	ref, err := name.ParseReference(ociRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse OCI reference %s: %w", ociRef, err)
	}

	// Get the manifest digest
	desc, err := remote.Head(ref, remote.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("failed to get manifest for %s: %w", ociRef, err)
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
		return nil, fmt.Errorf("failed to fetch signature image for %s: %w", ociRef, err)
	}

	// Extract signatures from layers
	manifest, err := sigImg.Manifest()
	if err != nil {
		return nil, fmt.Errorf("failed to get signature manifest: %w", err)
	}

	layers, err := sigImg.Layers()
	if err != nil {
		return nil, fmt.Errorf("failed to get signature layers: %w", err)
	}

	var sigs []cosignSignature
	for i, layer := range layers {
		if i >= len(manifest.Layers) {
			break
		}
		layerDesc := manifest.Layers[i]

		// Read the payload from the layer with size limit to prevent memory exhaustion
		reader, err := layer.Uncompressed()
		if err != nil {
			slog.Debug("failed to decompress signature layer", "index", i, "error", err)
			continue
		}
		payload, err := io.ReadAll(io.LimitReader(reader, maxSignaturePayloadSize+1))
		reader.Close()
		if err != nil {
			slog.Debug("failed to read signature layer", "index", i, "error", err)
			continue
		}
		if len(payload) > maxSignaturePayloadSize {
			slog.Warn("cosign signature layer exceeds size limit, skipping", "index", i)
			continue
		}

		sig := cosignSignature{
			Payload: payload,
		}

		// Extract annotations
		if annotations := layerDesc.Annotations; annotations != nil {
			sig.Base64Signature = annotations["dev.cosignproject.cosign/signature"]
			if bundleStr, ok := annotations["dev.sigstore.cosign/bundle"]; ok {
				sig.Bundle = []byte(bundleStr)
			}
		}

		sigs = append(sigs, sig)
	}

	return &cosignSignatures{
		ArtifactDigest: desc.Digest,
		Signatures:     sigs,
	}, nil
}

// isNotFoundError checks if an error is an HTTP 404 from the OCI registry.
func isNotFoundError(err error) bool {
	var transportErr *transport.Error
	if errors.As(err, &transportErr) {
		return transportErr.StatusCode == http.StatusNotFound
	}
	return false
}
