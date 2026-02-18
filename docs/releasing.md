# Releasing

## Overview

`tomei` releases are built and published by [goreleaser](https://goreleaser.com/) via GitHub Actions. A release is triggered by pushing a `v*` tag.

## Targets

goreleaser cross-compiles for the following platforms:

- linux/amd64
- linux/arm64
- darwin/arm64

Each archive contains the `tomei` binary, `LICENSE`, `NOTICE`, and `README.md`. SHA-256 checksums are generated for all archives.

## Version Information

The following values are injected at build time via `-ldflags`:

| Variable | Source |
|----------|--------|
| `main.version` | Git tag (e.g. `0.1.0`) |
| `main.commit` | Short commit hash |
| `main.buildDate` | Build timestamp |

These are the same variables used by `make build`.

## Release Flow

### 1. Tag and push

```bash
git tag v0.1.0
git push origin v0.1.0
```

### 2. Automated pipeline

The tag push triggers `.github/workflows/release.yaml`:

1. CI runs first (build, test, lint, integration test, E2E)
2. On CI success, goreleaser builds archives and publishes a GitHub Release

### 3. Manual re-release

The workflow also supports `workflow_dispatch` for manual triggers. It verifies that CI has already passed for the given tag before proceeding.

## Dry Run

To test the release locally without publishing:

```bash
goreleaser release --snapshot --clean
```

## CUE Module Release

The CUE module (`tomei.terassyi.net@v0`) is released separately from the binary. See [Module Publishing](module-publishing.md) for details.

### Coordinated release

When releasing both the binary and the CUE module, tag the CUE module first so that `tomei cue init` resolves the correct version:

```bash
# 1. CUE module
git tag tomei-cue-v0.1.0
git push origin tomei-cue-v0.1.0
# Then trigger workflow_dispatch on "Publish CUE Module" workflow

# 2. Binary
git tag v0.1.0
git push origin v0.1.0
```

## Configuration

- goreleaser: [`.goreleaser.yaml`](../.goreleaser.yaml)
- GitHub Actions: [`.github/workflows/release.yaml`](../.github/workflows/release.yaml)
- CUE publish: [`.github/workflows/publish-module.yaml`](../.github/workflows/publish-module.yaml)
