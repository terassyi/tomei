# Real-World Example

Production-ready manifests combining presets and raw delegation patterns.
Demonstrates a full-stack development environment with multiple runtimes and tool ecosystems.

This example uses **non-vendored** CUE modules — dependencies are resolved from the OCI registry (`ghcr.io/terassyi`) at load time. Cosign signature verification is performed automatically for first-party modules.

> To use vendored (offline) mode instead, run `make vendor-cue` for `examples/minimal/` or set `CUE_REGISTRY=none` (requires `cue.mod/pkg/` to be populated manually).

## Directory Structure

```
real-world/
├── cue.mod/module.cue      # CUE module with deps (OCI registry resolution)
├── tomei_platform.cue      # Platform @tag() declarations (generated)
├── runtimes.cue            # Go, Rust, uv (Python), pnpm (Node.js)
├── k8s.cue                 # kubectl, kustomize, helm, kind, krew + installer
├── utility.cue             # bat, rg, fd, jq, yq, fzf
├── go.cue                  # gopls, staticcheck, goimports, cue (via go install)
├── rust.cue                # cargo-binstall + binstall installer
├── uv.cue                  # ruff, mypy, httpie, black (via uv)
├── node.cue                # prettier, ts-node, typescript, npm-check-updates (via pnpm)
└── krew.cue                # ctx, ns, neat, node-shell (via krew)
```

## Runtimes (4)

| Runtime | Type | Description |
|---------|------|-------------|
| Go | download (preset) | Official binary from go.dev |
| Rust | delegation (preset) | Bootstrapped via rustup |
| uv | delegation | Python package manager (astral.sh installer) |
| pnpm | delegation | Node.js package manager (standalone installer) |

## Tools (30)

| File | Installer | Tools |
|------|-----------|-------|
| `k8s.cue` | aqua | kubectl, kustomize, helm, kind |
| `k8s.cue` | aqua + krew (delegation) | krew, ctx, ns, neat, node-shell |
| `utility.cue` | aqua | bat, rg, fd, jq, yq, fzf |
| `go.cue` | go install | gopls, staticcheck, goimports, cue |
| `rust.cue` | rust (preset) | cargo-binstall + binstall installer |
| `uv.cue` | uv (delegation) | ruff, mypy, httpie, ansible |
| `node.cue` | pnpm (delegation) | prettier, ts-node, typescript, npm-check-updates |
| `krew.cue` | krew (delegation) | ctx, ns, neat, node-shell |

## Usage

```bash
# Initialize module dependencies (resolves latest versions from OCI registry)
tomei cue init --force examples/real-world/

# Apply manifests (CUE modules pulled from OCI registry + cosign verified)
tomei apply examples/real-world/
```

## Patterns Demonstrated

- **Preset import** — Go/Rust runtimes and aqua tools use `tomei.terassyi.net/presets/*`
- **Delegation runtime** — uv and pnpm bootstrap via shell scripts, then serve as tool installers
- **Delegation installer chain** — krew is installed via aqua, then used as an Installer for kubectl plugins
- **Cargo-binstall chain** — Rust runtime → cargo-binstall tool → binstall installer (preset)
