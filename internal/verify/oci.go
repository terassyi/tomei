package verify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	ociv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"google.golang.org/protobuf/encoding/protojson"
)

// CosignSigTag returns the cosign signature tag for the given digest.
// Cosign stores signatures at sha256-<hex>.sig.
func CosignSigTag(digest ociv1.Hash) string {
	return strings.ReplaceAll(digest.String(), ":", "-") + ".sig"
}

// cosignSignatures holds the parsed Sigstore bundles fetched from the registry
// along with the digest of the signed artifact (needed for verification binding).
type cosignSignatures struct {
	// ArtifactDigest is the digest of the OCI artifact that was signed.
	ArtifactDigest ociv1.Hash
	// Bundles is the list of parsed Sigstore bundles from cosign signature annotations.
	Bundles []*bundle.Bundle
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

	// Extract Sigstore bundles from manifest layer annotations
	manifest, err := sigImg.Manifest()
	if err != nil {
		return nil, fmt.Errorf("failed to get signature manifest: %w", err)
	}

	var bundles []*bundle.Bundle
	for i, layerDesc := range manifest.Layers {
		if annotations := layerDesc.Annotations; annotations != nil {
			if bundleStr, ok := annotations["dev.sigstore.cosign/bundle"]; ok {
				if len(bundleStr) > maxSignaturePayloadSize {
					slog.Warn("cosign bundle annotation exceeds size limit, skipping", "index", i)
					continue
				}
				b, err := parseBundle([]byte(bundleStr))
				if err != nil {
					slog.Debug("failed to parse sigstore bundle from annotation", "index", i, "error", err)
					continue
				}
				bundles = append(bundles, b)
			}
		}
	}

	return &cosignSignatures{
		ArtifactDigest: desc.Digest,
		Bundles:        bundles,
	}, nil
}

// parseBundle parses protobuf JSON into a validated Sigstore bundle.
// Uses protojson (not encoding/json) because protobuf oneof fields
// require protobuf-aware JSON deserialization.
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
