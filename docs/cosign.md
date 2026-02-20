# Cosign Signature Verification

## Overview

Tomei verifies the integrity and authenticity of its CUE module OCI artifacts using [cosign](https://github.com/sigstore/cosign) signatures. This protects against supply-chain attacks where a compromised OCI registry could serve tampered modules.

## How It Works

When `tomei apply`, `tomei plan`, or `tomei validate` loads CUE manifests that import first-party modules (`tomei.terassyi.net`), the loader:

1. Reads `cue.mod/module.cue` to extract module dependencies
2. Filters for first-party modules (`tomei.terassyi.net` prefix)
3. Resolves each dependency to its OCI reference (e.g. `ghcr.io/terassyi/tomei.terassyi.net:v0.0.3`)
4. Fetches and verifies the cosign signature for each OCI artifact
5. Proceeds with CUE evaluation only if verification passes

### Keyless Signing (CI)

Modules are signed in CI using GitHub Actions OIDC-based keyless signing:

- **Fulcio** issues short-lived signing certificates based on the GitHub Actions OIDC token
- **Rekor** records signatures in a transparency log for non-repudiation
- No long-lived signing keys are required

### Verification Policy

Verification checks:

- **OIDC Issuer:** `https://token.actions.githubusercontent.com`
- **Certificate SAN:** matches `https://github.com/terassyi/tomei/` (the publish workflow)

## Skipping Verification

Verification is automatically skipped in these cases:

| Condition | Reason |
|-----------|--------|
| `--ignore-cosign` flag | User explicitly disabled verification |
| `CUE_REGISTRY=none` | Vendor mode — modules are loaded from local `cue.mod/pkg/` |
| No `cue.mod/` directory | No module dependencies to verify |
| No first-party deps | Only third-party modules are imported |

### Manual Skip

```bash
# Skip cosign verification for all commands
tomei apply --ignore-cosign .
tomei plan --ignore-cosign .
tomei validate --ignore-cosign .
```

## Soft-Fail Mode

In the initial release, unsigned modules produce a **warning** but do not fail the command. This allows existing users to continue working while the signing infrastructure is deployed.

Once all published module versions have been signed, verification will switch to **hard-fail** mode where unsigned modules cause an error.

## Troubleshooting

### "cosign signature not found for module (unsigned)"

The module was published before cosign signing was enabled. Options:

1. Re-publish the module with signing enabled
2. Use `--ignore-cosign` to skip verification temporarily

### "failed to fetch signatures"

Network error when accessing the OCI registry. Check:

- Network connectivity to `ghcr.io`
- Authentication (`GITHUB_TOKEN` / `GH_TOKEN` for rate limits)

### "cosign verification skipped: vendor mode"

This is expected when using `CUE_REGISTRY=none`. Vendor mode loads modules from `cue.mod/pkg/` without registry access.

## Future

- Third-party module verification (custom certificate identity policies)
- Hard-fail mode for unsigned modules (after all versions are signed)
- Offline verification with cached trusted root
