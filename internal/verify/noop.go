package verify

import "context"

// noopVerifier is a Verifier that skips all verification.
// Used when verification is disabled (e.g. --ignore-cosign, vendor mode).
type noopVerifier struct {
	reason string
}

// NewNoopVerifier creates a Verifier that skips all verification with the given reason.
func NewNoopVerifier(reason string) Verifier {
	return &noopVerifier{reason: reason}
}

// Verify returns a skipped Result for each dependency.
func (v *noopVerifier) Verify(_ context.Context, deps []ModuleDependency) ([]Result, error) {
	results := make([]Result, len(deps))
	for i, dep := range deps {
		results[i] = Result{
			Module:     dep,
			Skipped:    true,
			SkipReason: v.reason,
		}
	}
	return results, nil
}
