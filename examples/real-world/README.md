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
├── runtimes.cue            # Go, Rust, uv (Python), pnpm (Node.js), Deno, Bun
├── k8s.cue                 # kubectl, kustomize, helm, kind
├── utility.cue             # bat, rg, fd, jq, yq, fzf
├── go.cue                  # gopls, staticcheck, goimports, cue (via go install)
├── rust.cue                # cargo-binstall + binstall installer
├── uv.cue                  # ruff, mypy, httpie, ansible (via uv)
├── node.cue                # prettier, ts-node, typescript, npm-check-updates (via pnpm)
├── bun.cue                 # biome (via bun)
├── brew.cue                # Darwin/arm64-only (Homebrew formulae)
├── system_packages.cue     # apt SystemInstaller + build-deps (Phase 4, Debian/Ubuntu only)
└── privileged.cue          # lazygit via aqua + privileged: true (Phase 2)
```

## Runtimes

| Runtime | Type | Description |
|---------|------|-------------|
| Go | download (preset) | Official binary from go.dev |
| Rust | delegation (preset) | Bootstrapped via rustup |
| uv | delegation | Python package manager (astral.sh installer) |
| pnpm | delegation | Node.js package manager (standalone installer) |
| Deno | download (preset) | Official binary from dl.deno.land |
| Bun | download (preset) | Official binary from GitHub releases |

## Tools

| File | Installer | Tools |
|------|-----------|-------|
| `k8s.cue` | aqua | kubectl, kustomize, helm, kind |
| `utility.cue` | aqua | bat, rg, fd, jq, yq, fzf |
| `go.cue` | go install | gopls, staticcheck, goimports, cue |
| `rust.cue` | rust (preset) | cargo-binstall + binstall installer |
| `uv.cue` | uv (delegation) | ruff, mypy, httpie, ansible |
| `node.cue` | pnpm (delegation) | prettier, ts-node, typescript, npm-check-updates |
| `bun.cue` | bun (delegation) | biome |

## Privileged Tools (1, `--system`)

These tools are symlinked into the system bin directory (default `/usr/local/bin`) instead of `~/.local/bin`. Requires `tomei apply --system`.

| File | Installer | Tools |
|------|-----------|-------|
| `privileged.cue` | aqua | lazygit |

Apple Silicon macOS uses `/opt/homebrew` by default; tomei currently routes privileged symlinks to `/usr/local/bin` regardless. Overriding requires a recompile via `path.WithSystemBinDir` — there is no end-user knob today.

## System Packages (Debian/Ubuntu only)

Apt-managed packages installed under `tomei apply --system`. The manifest is gated with `@if(linux)` so non-Linux platforms ignore it cleanly, but the actual install runs through tomei's built-in APT backend (`internal/installer/apt`) — which only works on apt-based distros (Debian, Ubuntu). Other Linux distros (Fedora, Arch, Alpine, etc.) need a SystemInstaller bound to a different backend. The `spec.commands` block on the inline SystemInstaller is descriptive metadata; the backend owns the actual invocation.

| File | Installer | Packages |
|------|-----------|----------|
| `system_packages.cue` | apt (inline `SystemInstaller`) | pkg-config, libssl-dev, tree |

## Usage

```bash
# Initialize tomei state directory (~/.local/share/tomei)
tomei init

# Initialize module dependencies (resolves latest versions from OCI registry)
tomei cue init --force examples/real-world/

# Apply manifests (CUE modules pulled from OCI registry + cosign verified)
tomei apply examples/real-world/
```

## Running in a container

The Phase 4 SystemPackage example mutates apt and the Phase 2 privileged tool writes to `/usr/local/bin` — both are host-global operations. Use the bundled Ubuntu container at `examples/Dockerfile` to try them safely without touching your host.

```bash
# From the repo root: build the tomei binary + container image
make -C examples build

# Open an interactive shell as the `lipnoise` user inside the container.
# The Dockerfile copies examples/real-world/ to /home/lipnoise/examples/real-world/
# and grants NOPASSWD sudo so `--system` works without prompting.
make -C examples run

# Inside the container:
tomei init                                     # creates ~/.local/share/tomei state
tomei cue init --force examples/real-world/    # resolves CUE module deps

# User-mode apply: symlinks land in ~/.local/bin; privileged tools and
# system packages are skipped with a warning.
tomei apply examples/real-world/

# System-mode apply: lazygit lands in /usr/local/bin and build-deps is
# installed via apt-get. State stays per-user under ~/.local/share/tomei/.
tomei apply --system examples/real-world/
```

Notes:
- The container is ephemeral — exit the shell and `docker rm` runs automatically (`--rm`). Re-running `make run` starts a fresh container.
- Network egress is needed to download tools from GitHub Releases / apt repositories.
- `make -C examples clean` removes the built binary and the container image.

### GitHub token required for aqua tools

`tomei init` resolves the latest aqua-registry ref via `api.github.com` and then fetches registry content from `raw.githubusercontent.com`. Anonymous traffic from a fresh container reliably hits HTTP 403 within seconds because of GitHub's rate limits — without a token the aqua-based tools (k8s, utility, privileged) all fail with `aqua-registry resolver not configured`. `make run` already passes `GITHUB_TOKEN` / `GH_TOKEN` through if set on the host:

```bash
export GITHUB_TOKEN=ghp_...   # or use `gh auth token`
make -C examples run
```

`go install`-based tools, `uv`-based tools, `pnpm`-based tools, and the Phase 4 apt SystemPackageSet do not need the token — only the aqua/registry path does.

## Patterns Demonstrated

- **Preset import** — Go/Rust runtimes and aqua tools use `tomei.terassyi.net/presets/*`
- **Delegation runtime** — uv and pnpm bootstrap via shell scripts, then serve as tool installers
- **Cargo-binstall chain** — Rust runtime → cargo-binstall tool → binstall installer (preset)
- **Inline SystemInstaller** — apt is declared directly in `system_packages.cue` (Phase 4)
- **Privileged tool routing** — lazygit in `privileged.cue` is installed via aqua but symlinked into the system bin directory under `--system` (Phase 2)
