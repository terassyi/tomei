// Package verify provides cosign signature verification for CUE module OCI artifacts.
// It verifies that first-party modules (tomei.terassyi.net) are signed before CUE evaluation.
package verify

import (
	"context"
	"strings"
)

const (
	// FirstPartyPrefix is the module path prefix for first-party tomei modules.
	FirstPartyPrefix = "tomei.terassyi.net"
)

// ModuleDependency represents a CUE module dependency to verify.
type ModuleDependency struct {
	ModulePath string // e.g. "tomei.terassyi.net@v0"
	Version    string // e.g. "v0.0.3"
}

// Result represents the verification result for a single module.
type Result struct {
	Module     ModuleDependency
	Verified   bool
	Skipped    bool
	SkipReason string
}

// Verifier verifies cosign signatures of CUE module OCI artifacts.
type Verifier interface {
	// Verify checks the cosign signatures for the given module dependencies.
	// Returns a Result for each dependency.
	Verify(ctx context.Context, deps []ModuleDependency) ([]Result, error)
}

// IsFirstParty returns true if the module path is a first-party tomei module.
// It checks for the "tomei.terassyi.net" prefix followed by either
// a path separator, major version separator, or end of string.
func IsFirstParty(modulePath string) bool {
	if modulePath == "" {
		return false
	}
	if !strings.HasPrefix(modulePath, FirstPartyPrefix) {
		return false
	}
	// Ensure it's an exact prefix match, not a partial domain match
	rest := modulePath[len(FirstPartyPrefix):]
	return rest == "" || rest[0] == '/' || rest[0] == '@'
}
