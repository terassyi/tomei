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

## Bundle Format Compatibility

Tomei supports cosign v2 keyless signatures, which store signature components as individual OCI manifest layer annotations rather than a single protobuf bundle.

### Cosign v2 Annotations

Cosign v2 stores the following annotations on each signature layer:

| Key | Content |
|-----|---------|
| `dev.cosignproject.cosign/signature` | Base64-encoded ECDSA signature |
| `dev.sigstore.cosign/certificate` | PEM Fulcio leaf certificate |
| `dev.sigstore.cosign/chain` | PEM certificate chain |
| `dev.sigstore.cosign/bundle` | Rekor entry JSON (not a protobuf bundle) |

The signature layer content itself is a **SimpleSigning JSON payload** containing `{"critical":{"image":{"docker-manifest-digest":"sha256:..."}}}`, which binds the signature to the specific OCI artifact.

### Artifact Binding

Verification performs two-stage artifact binding:

1. **Cryptographic binding**: sigstore-go verifies the ECDSA signature was computed over the SimpleSigning payload
2. **Artifact binding**: The `docker-manifest-digest` in the SimpleSigning payload is compared against the actual OCI artifact digest from `remote.Head()`

This prevents signature transplant attacks where a valid signature from one artifact version could be reused for a different (tampered) artifact.

### Legacy Format

For forward compatibility, tomei also supports the protobuf bundle format (`application/vnd.dev.sigstore.bundle+json`). If cosign v2 annotation keys are not present on a layer, tomei falls back to parsing the `dev.sigstore.cosign/bundle` annotation as a protobuf bundle.

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

The module has no cosign signatures, or the signature annotations could not be parsed. Possible causes:

1. The module was published before cosign signing was enabled
2. The cosign version used for signing produces an unsupported annotation format

Options:

1. Re-publish the module with signing enabled (cosign v2.x recommended)
2. Use `--ignore-cosign` to skip verification temporarily

### "failed to fetch signatures"

Network error when accessing the OCI registry. Check:

- Network connectivity to `ghcr.io`
- Authentication (`GITHUB_TOKEN` / `GH_TOKEN` for rate limits)

### "cosign verification skipped: vendor mode"

This is expected when using `CUE_REGISTRY=none`. Vendor mode loads modules from `cue.mod/pkg/` without registry access.

## Manual Verification

The [Weekly Scenario Test](../.github/workflows/weekly-scenario.yaml) includes a `cosign-verify` job that automatically verifies cosign signatures against the live OCI registry on every scheduled run. This provides ongoing CI-level assurance that signature verification works end-to-end.

For local ad-hoc verification, use the following steps against the live registry:

```bash
# 1. Build tomei
make build

# 2. Set up a fresh CUE module directory
rm -rf /tmp/tomei-cue-test/cue.mod
./bin/tomei cue init --force /tmp/tomei-cue-test/

# 3. Run plan (triggers cosign verification during CUE module loading)
./bin/tomei plan /tmp/tomei-cue-test/
```

**Expected**: `INFO "cosign signature verified"` in the log output.

**Failure indicator**: `WARN "cosign signature not found for module (unsigned)"` means the bundle parsing or annotation format has regressed.

To compare with verification disabled:

```bash
./bin/tomei plan --ignore-cosign /tmp/tomei-cue-test/
```

## Future

- Third-party module verification (custom certificate identity policies)
- Hard-fail mode for unsigned modules (after all versions are signed)
- Offline verification with cached trusted root
