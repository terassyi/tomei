package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"cuelang.org/go/mod/module"
	ociv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/sigstore/sigstore-go/pkg/bundle"
	"github.com/sigstore/sigstore-go/pkg/root"
	"github.com/sigstore/sigstore-go/pkg/tuf"
	sgverify "github.com/sigstore/sigstore-go/pkg/verify"
)

const (
	// expectedOIDCIssuer is the OIDC issuer for GitHub Actions keyless signing.
	expectedOIDCIssuer = "https://token.actions.githubusercontent.com"

	// expectedSANRegex matches the GitHub Actions workflow identity for terassyi/tomei.
	expectedSANRegex = `^https://github\.com/terassyi/tomei/`
)

var _ Verifier = (*SigstoreVerifier)(nil)

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
func (v *SigstoreVerifier) Verify(ctx context.Context, deps []module.Version) ([]Result, error) {
	results := make([]Result, 0, len(deps))

	for _, dep := range deps {
		result := v.verifyOne(ctx, dep)
		results = append(results, result)
	}

	return results, nil
}

// verifyOne verifies a single module dependency.
func (v *SigstoreVerifier) verifyOne(ctx context.Context, dep module.Version) Result {
	ref, err := v.refResolver.Resolve(dep)
	if err != nil {
		slog.Warn("cosign verification skipped: cannot resolve OCI reference",
			"module", dep.Path(),
			"version", dep.Version(),
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
			"module", dep.Path(),
			"version", dep.Version(),
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
			"module", dep.Path(),
			"version", dep.Version(),
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
		if err := v.verifySigstoreBundle(sig.Bundle, sig.SimpleSigningPayload, result.ArtifactDigest); err != nil {
			slog.Debug("cosign signature verification attempt failed",
				"module", dep.Path(),
				"error", err,
			)
			continue
		}

		slog.Info("cosign signature verified",
			"module", dep.Path(),
			"version", dep.Version(),
		)
		return Result{
			Module:   dep,
			Verified: true,
		}
	}

	// All signature verification attempts failed — warn and continue (soft-fail)
	slog.Warn("cosign signature verification failed for all signatures",
		"module", dep.Path(),
		"version", dep.Version(),
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
		v.trustedRoot, v.trustedRootErr = root.NewLiveTrustedRoot(tuf.DefaultOptions())
	})
	return v.trustedRoot, v.trustedRootErr
}

// verifySigstoreBundle verifies a parsed Sigstore bundle using the public-good
// Sigstore trusted root (Fulcio + Rekor). It checks certificate identity for
// the terassyi/tomei GitHub Actions workflow.
//
// For cosign v2 signatures (simpleSigningPayload != nil), verification binds
// the signature to the SimpleSigning payload, then verifies that the payload's
// docker-manifest-digest matches the actual artifact digest.
//
// For legacy protobuf bundles (simpleSigningPayload == nil), it falls back to
// direct artifact digest binding.
func (v *SigstoreVerifier) verifySigstoreBundle(b *bundle.Bundle, simpleSigningPayload []byte, artifactDigest ociv1.Hash) error {
	trustedRoot, err := v.getTrustedRoot()
	if err != nil {
		return fmt.Errorf("failed to fetch trusted root: %w", err)
	}

	// Create a verifier with certificate identity policy.
	// WithIntegratedTimestamps(1) verifies timestamps embedded in Rekor
	// transparency log entries, which is how GitHub Actions keyless signing works.
	verifierConfig, err := sgverify.NewVerifier(
		trustedRoot,
		sgverify.WithSignedCertificateTimestamps(1),
		sgverify.WithTransparencyLog(1),
		sgverify.WithIntegratedTimestamps(1),
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

	if simpleSigningPayload != nil {
		// Cosign v2: verify signature against SimpleSigning payload
		_, err = verifierConfig.Verify(b, sgverify.NewPolicy(
			sgverify.WithArtifact(bytes.NewReader(simpleSigningPayload)),
			sgverify.WithCertificateIdentity(certIdentity),
		))
		if err != nil {
			return fmt.Errorf("signature verification failed: %w", err)
		}

		// Verify that SimpleSigning payload references the correct artifact
		if err := verifySimpleSigningBinding(simpleSigningPayload, artifactDigest); err != nil {
			return fmt.Errorf("artifact binding verification failed: %w", err)
		}

		return nil
	}

	// Legacy protobuf bundle: fall back to direct artifact digest binding with warning
	slog.Warn("using legacy protobuf bundle without SimpleSigning payload — artifact binding is weaker")

	_, err = verifierConfig.Verify(b, sgverify.NewPolicy(
		sgverify.WithoutArtifactUnsafe(),
		sgverify.WithCertificateIdentity(certIdentity),
	))
	if err != nil {
		return fmt.Errorf("signature verification failed: %w", err)
	}

	return nil
}

// simpleSigningDoc represents the minimal structure of a SimpleSigning JSON payload
// for extracting the docker-manifest-digest.
type simpleSigningDoc struct {
	Critical struct {
		Image struct {
			DockerManifestDigest string `json:"docker-manifest-digest"`
		} `json:"image"`
	} `json:"critical"`
}

// verifySimpleSigningBinding checks that the SimpleSigning payload references
// the expected OCI artifact digest. This prevents signature transplant attacks
// where a valid signature from one artifact is reused for a different artifact.
func verifySimpleSigningBinding(payload []byte, expectedDigest ociv1.Hash) error {
	var doc simpleSigningDoc
	if err := json.Unmarshal(payload, &doc); err != nil {
		return fmt.Errorf("failed to parse SimpleSigning payload: %w", err)
	}

	actualDigest := doc.Critical.Image.DockerManifestDigest
	expected := expectedDigest.String()
	if actualDigest != expected {
		return fmt.Errorf("SimpleSigning digest mismatch: payload contains %q but artifact has %q", actualDigest, expected)
	}

	return nil
}
