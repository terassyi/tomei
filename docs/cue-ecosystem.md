# CUE Ecosystem Integration

This document describes how `tomei` integrates with the CUE module ecosystem via OCI registries.

## Overview

`tomei` publishes its presets and schema as a CUE module (`tomei.terassyi.net@v0`) to an OCI registry (`ghcr.io/terassyi`). This enables:

- **CUE tooling compatibility**: `cue eval`, `cue vet`, and CUE LSP can resolve `tomei.terassyi.net/*` imports
- **Standard module resolution**: follows the CUE module ecosystem conventions
- **Single binary deployment**: `tomei apply` resolves modules via Go libraries (`cuelang.org/go`) without requiring the `cue` CLI

## Architecture

```
Development machine (CUE CLI, LSP)    Target machine (tomei only)
┌─────────────────────────┐           ┌──────────────────────────────┐
│ cue eval / LSP           │           │ tomei apply                  │
│   ↓                      │           │   ↓                          │
│ CUE_REGISTRY env var     │           │ Built-in default:            │
│ (via eval $(tomei env))  │           │ tomei.terassyi.net           │
│   ↓                      │           │ = ghcr.io/terassyi           │
│ OCI registry             │           │   ↓                          │
│ (ghcr.io/terassyi)       │           │ modconfig.NewRegistry()      │
│   ↓                      │           │   ↓                          │
│ Module resolution        │           │ OCI pull (Go library)        │
└─────────────────────────┘           └──────────────────────────────┘
```

## Registry Resolution

`tomei apply` does **not** depend on the `CUE_REGISTRY` environment variable. It constructs registry configuration programmatically:

- When `CUE_REGISTRY` is not set: uses the built-in default `tomei.terassyi.net=ghcr.io/terassyi`
- When `CUE_REGISTRY` is set: uses the user-provided configuration (CUE standard mechanism)

This means `tomei apply` works on a fresh machine with no setup beyond internet connectivity.

## User Experience

### Basic flow (development machine)

```bash
# 1. Initialize CUE module
$ tomei cue init
Created cue.mod/module.cue
Created tomei_platform.cue

  CUE tooling (cue eval, LSP) requires:
    eval $(tomei env)

# 2. Write manifest (CUE LSP completion works)
$ cat tools.cue
package tomei
import gopreset "tomei.terassyi.net/presets/go"
goRuntime: gopreset.#GoRuntime & {
    platform: { os: _os, arch: _arch }
    spec: version: "1.25.6"
}

# 3. Distribute via git
$ git add cue.mod/ tools.cue tomei_platform.cue && git commit && git push
```

### Target machine (no CUE CLI, no CUE_REGISTRY)

```bash
$ git clone <repo> && cd <repo>
$ tomei apply
# → tomei resolves tomei.terassyi.net via built-in ghcr.io/terassyi mapping
# → CUE Go library pulls + caches the module
✓ go 1.25.6 installed
```

### Platform specification

```cue
// Default: tomei apply resolves automatically via @tag()
goRuntime: gopreset.#GoRuntime & {
    platform: { os: _os, arch: _arch }
    spec: version: "1.25.6"
}

// Explicit (cross-compile etc.)
goRuntime: gopreset.#GoRuntime & {
    platform: { os: "darwin", arch: "arm64" }
    spec: version: "1.25.6"
}
```

```bash
# With cue eval
cue eval -t os=linux -t arch=amd64 tools.cue
```

### CUE_REGISTRY in tomei env

```bash
$ eval $(tomei env)
export PATH="..."
export GOROOT="..."
export CUE_REGISTRY="tomei.terassyi.net=ghcr.io/terassyi"
# → cue eval / LSP can resolve tomei presets
```

## Generated Files

### `tomei cue init` generates:

**cue.mod/module.cue:**
```cue
module: "manifests.local@v0"
language: version: "v0.9.0"
deps: {
    "tomei.terassyi.net@v0": v: "v0.0.1"
}
```

**tomei_platform.cue:**
```cue
package tomei

// Platform values resolved by tomei apply.
// For cue eval: cue eval -t os=linux -t arch=amd64
_os:       string @tag(os)
_arch:     string @tag(arch)
_headless: bool | *false @tag(headless,type=bool)
```

## Two Resolution Modes

### Virtual module overlay (no `cue.mod/`)

When no `cue.mod/` directory exists, `tomei` creates a virtual CUE module in memory. Presets and schema embedded in the binary are served via the CUE overlay mechanism. This is the default behavior and requires no setup.

### OCI registry resolution (with `cue.mod/`)

When `cue.mod/` exists, `tomei` uses `modconfig.NewRegistry()` to create a CUE registry that resolves imports via OCI. The virtual overlay is skipped. This enables standard CUE tooling integration.

## Module Publishing

The `tomei.terassyi.net@v0` module is published to `ghcr.io/terassyi` via:

1. `hack/publish-module/main.go` — assembles the module from `presets/` and `schema/`
2. `.github/workflows/publish-module.yaml` — triggers on release, pushes to ghcr.io

The module zip is created using `modzip.CreateFromDir` and pushed via `modregistry.Client.PutModule`.
