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
    "tomei.terassyi.net@v0": v: "<latest from ghcr.io>"
}
```

The `deps` version is resolved at `tomei cue init` time by querying the OCI registry (ghcr.io) for the latest published tag. This pins the exact patch version for reproducibility. To update, run `cue mod tidy`.

**tomei_platform.cue:**
```cue
package tomei

// Platform values resolved by tomei apply.
// For cue eval: cue eval -t os=linux -t arch=amd64
_os:       string @tag(os)
_arch:     string @tag(arch)
_headless: bool | *false @tag(headless,type=bool)
```

## CUE Subcommands

`tomei` provides CUE subcommands that integrate with the tomei registry and `@tag()` configuration.

### tomei cue scaffold

Generate manifest scaffolds for any resource kind:

```bash
$ tomei cue scaffold tool
package tomei

import "tomei.terassyi.net/schema"

myTool: schema.#Tool & {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "my-tool"
    spec: { ... }
}

# Without schema import (for environments without cue.mod/)
$ tomei cue scaffold tool --bare
```

Supported kinds: `tool`, `runtime`, `installer`, `installer-repository`, `toolset`.

### tomei cue eval / export

Evaluate CUE manifests with tomei's registry and `@tag()` configuration automatically applied:

```bash
# CUE text output (like cue eval, but with tomei config)
$ tomei cue eval ./manifests/

# JSON output (like cue export, but with tomei config)
$ tomei cue export ./manifests/
```

Unlike plain `cue eval` / `cue export`, these commands:
- Configure the OCI registry automatically (no `CUE_REGISTRY` setup needed)
- Inject `@tag()` values (`os`, `arch`, `headless`) from the current platform
- Exclude `config.cue` from evaluation

This is useful for debugging manifests and verifying that `@tag()` values resolve correctly before running `tomei apply`.

## Module Resolution

`tomei` uses `modconfig.NewRegistry()` to create a CUE registry that resolves imports via OCI. The default registry mapping is `tomei.terassyi.net=ghcr.io/terassyi`. When `CUE_REGISTRY` is set by the user, it takes precedence.

For directory-mode loading (package-based CUE files), a `cue.mod/` directory is required. Use `tomei cue init` to generate it. For single-file loading without imports, `cue.mod/` is not needed.

## Module Publishing

See [Module Publishing](module-publishing.md) for versioning strategy, tag conventions, and publish workflow.
