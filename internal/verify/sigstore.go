package verify

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	protobundle "github.com/sigstore/protobuf-specs/gen/pb-go/bundle/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	sgverify "github.com/sigstore/sigstore-go/pkg/verify"
)

const (
	// expectedOIDCIssuer is the OIDC issuer for GitHub Actions keyless signing.
	expectedOIDCIssuer = "https://token.actions.githubusercontent.com"

	// expectedSANRegex matches the GitHub Actions workflow identity for terassyi/tomei.
	expectedSANRegex = `^https://github\.com/terassyi/tomei/`
)

// SigstoreVerifier verifies cosign signatures on OCI artifacts using sigstore-go.
// In production, it performs keyless verification via Fulcio + Rekor.
type SigstoreVerifier struct {
	refResolver *ReferenceResolver

	trustedRootOnce sync.Once
	trustedRoot     *root.LiveTrustedRoot
	trustedRootErr  error
}

// NewSigstoreVerifier creates a new SigstoreVerifier for the given CUE_REGISTRY value.
func NewSigstoreVerifier(cueRegistry string) (*SigstoreVerifier, error) {
	refResolver, err := NewReferenceResolver(cueRegistry)
	if err != nil {
		return nil, fmt.Errorf("failed to create reference resolver: %w", err)
	}
	return &SigstoreVerifier{
		refResolver: refResolver,
	}, nil
}

// Verify checks cosign signatures for the given module dependencies.
// For the initial release, unsigned modules produce a warning but do not fail (warn + continue).
// This will be changed to hard-fail after all published modules have been signed.
func (v *SigstoreVerifier) Verify(ctx context.Context, deps []ModuleDependency) ([]Result, error) {
	results := make([]Result, 0, len(deps))

	for _, dep := range deps {
		result := v.verifyOne(ctx, dep)
		results = append(results, result)
	}

	return results, nil
}

// verifyOne verifies a single module dependency.
func (v *SigstoreVerifier) verifyOne(ctx context.Context, dep ModuleDependency) Result {
	ref, err := v.refResolver.Resolve(dep)
	if err != nil {
		slog.Warn("cosign verification skipped: cannot resolve OCI reference",
			"module", dep.ModulePath,
			"version", dep.Version,
			"error", err,
		)
		return Result{
			Module:     dep,
			Skipped:    true,
			SkipReason: fmt.Sprintf("cannot resolve OCI reference: %v", err),
		}
	}

	// Fetch cosign signatures from the registry
	result, err := fetchCosignSignatures(ctx, ref)
	if err != nil {
		slog.Warn("cosign verification skipped: failed to fetch signatures",
			"module", dep.ModulePath,
			"version", dep.Version,
			"ref", ref,
			"error", err,
		)
		return Result{
			Module:     dep,
			Skipped:    true,
			SkipReason: fmt.Sprintf("failed to fetch signatures: %v", err),
		}
	}

	if result == nil || len(result.Signatures) == 0 {
		// No signatures found — warn and continue (initial release: soft-fail)
		slog.Warn("cosign signature not found for module (unsigned)",
			"module", dep.ModulePath,
			"version", dep.Version,
			"ref", ref,
		)
		return Result{
			Module:     dep,
			Skipped:    true,
			SkipReason: "no cosign signature found (unsigned module)",
		}
	}

	// Try to verify each signature, binding to the artifact digest
	for _, sig := range result.Signatures {
		if len(sig.Bundle) == 0 {
			continue
		}

		if err := v.verifySigstoreBundle(sig.Bundle, result.ArtifactDigest); err != nil {
			slog.Debug("cosign signature verification attempt failed",
				"module", dep.ModulePath,
				"error", err,
			)
			continue
		}

		slog.Info("cosign signature verified",
			"module", dep.ModulePath,
			"version", dep.Version,
		)
		return Result{
			Module:   dep,
			Verified: true,
		}
	}

	// All signature verification attempts failed — warn and continue (soft-fail)
	slog.Warn("cosign signature verification failed for all signatures",
		"module", dep.ModulePath,
		"version", dep.Version,
	)
	return Result{
		Module:     dep,
		Skipped:    true,
		SkipReason: "all cosign signature verification attempts failed",
	}
}

// getTrustedRoot returns the cached public-good Sigstore trusted root,
// fetching it on the first call.
func (v *SigstoreVerifier) getTrustedRoot() (*root.LiveTrustedRoot, error) {
	v.trustedRootOnce.Do(func() {
		v.trustedRoot, v.trustedRootErr = root.NewLiveTrustedRoot(nil)
	})
	return v.trustedRoot, v.trustedRootErr
}

// verifySigstoreBundle verifies a Sigstore bundle using the public-good Sigstore
// trusted root (Fulcio + Rekor). It checks certificate identity for the
// terassyi/tomei GitHub Actions workflow and binds the signature to the
// given artifact digest to prevent signature transplant attacks.
func (v *SigstoreVerifier) verifySigstoreBundle(bundleJSON []byte, artifactDigest v1.Hash) error {
	// Parse the protobuf bundle
	var pbBundle protobundle.Bundle
	if err := json.Unmarshal(bundleJSON, &pbBundle); err != nil {
		return fmt.Errorf("failed to parse sigstore bundle: %w", err)
	}

	b, err := bundle.NewBundle(&pbBundle)
	if err != nil {
		return fmt.Errorf("failed to create bundle: %w", err)
	}

	trustedRoot, err := v.getTrustedRoot()
	if err != nil {
		return fmt.Errorf("failed to fetch trusted root: %w", err)
	}

	// Create a verifier with certificate identity policy
	verifierConfig, err := sgverify.NewVerifier(
		trustedRoot,
		sgverify.WithSignedCertificateTimestamps(1),
		sgverify.WithTransparencyLog(1),
	)
	if err != nil {
		return fmt.Errorf("failed to create verifier: %w", err)
	}

	// Build certificate identity for GitHub Actions OIDC
	certIdentity, err := sgverify.NewShortCertificateIdentity(
		expectedOIDCIssuer,
		"",
		"",
		expectedSANRegex,
	)
	if err != nil {
		return fmt.Errorf("failed to create certificate identity: %w", err)
	}

	// Decode the artifact digest for binding the signature to the specific artifact.
	// This prevents signature transplant attacks where a valid signature from one
	// artifact version is reused for a different (tampered) artifact.
	digestBytes, err := hex.DecodeString(artifactDigest.Hex)
	if err != nil {
		return fmt.Errorf("failed to decode artifact digest: %w", err)
	}

	// Verify the bundle with artifact digest binding
	_, err = verifierConfig.Verify(b, sgverify.NewPolicy(
		sgverify.WithArtifactDigest(artifactDigest.Algorithm, digestBytes),
		sgverify.WithCertificateIdentity(certIdentity),
	))
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	return nil
}
