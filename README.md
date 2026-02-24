# tomei

[![CI](https://github.com/terassyi/tomei/actions/workflows/ci.yaml/badge.svg)](https://github.com/terassyi/tomei/actions/workflows/ci.yaml)
[![Release](https://img.shields.io/github/v/release/terassyi/tomei)](https://github.com/terassyi/tomei/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/terassyi/tomei)](https://go.dev/)
[![License](https://img.shields.io/github/license/terassyi/tomei)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/terassyi/tomei)](https://goreportcard.com/report/github.com/terassyi/tomei)

A declarative, idempotent development environment setup tool powered by [CUE](https://cuelang.org/).

> [!TIP]
> The name "tomei" comes from the Japanese word **"透明"** — transparent. What you write is what you get, with nothing hidden in between.

![demo](demo/demo.gif)

## Design

Write the desired state in CUE manifests, run `tomei apply`, and the result is always the same no matter how many times you run it.

No containers, no virtual filesystems, no shims. `tomei` sets up your real environment directly.

Rather than reinventing package managers, tomei delegates to existing tools like `go install`, `cargo install`. For tools with their own installer scripts — like `mise` or `uv` — you can define install/update/remove commands directly. `tomei` orchestrates; they execute.

Native [aqua registry](https://github.com/aquaproj/aqua-registry) integration lets you install thousands of CLI tools by just specifying a package name and version.

[CUE](https://cuelang.org/) provides schema validation, platform-aware `@tag()` injection (`_os`, `_arch`, `_headless`), and type-safe manifest composition.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/terassyi/tomei/main/install.sh | sh
```

The script detects your OS and architecture, downloads the binary, verifies the SHA-256 checksum, and installs it to `~/.local/bin`.

## Getting Started

```bash
# Initialize
tomei init

# Set up CUE module
tomei cue init

# Write manifests, then apply
tomei plan .
tomei apply .

# Add runtime env vars to your shell
eval "$(tomei env)"
```

## How to Write CUE Manifests

Manifests are [CUE](https://cuelang.org/) files in `package tomei`. Run `tomei cue init` to create the `cue.mod/` directory, which enables schema imports and preset resolution via OCI registry. For details, see:

- [CUE Schema Reference](docs/cue-schema.md)
- [CUE Ecosystem Integration](docs/cue-ecosystem.md)

### Presets

Presets provide ready-made definitions for common runtimes and tools. Available presets:

| Import | Provides |
|--------|----------|
| `tomei.terassyi.net/presets/go` | `#GoRuntime`, `#GoTool`, `#GoToolSet` |
| `tomei.terassyi.net/presets/rust` | `#RustRuntime`, `#CargoBinstall`, `#BinstallInstaller`, `#BinstallToolSet` |
| `tomei.terassyi.net/presets/aqua` | `#AquaTool`, `#AquaToolSet` |
| `tomei.terassyi.net/presets/node` | `#PnpmRuntime` |
| `tomei.terassyi.net/presets/python` | `#UvRuntime` |
| `tomei.terassyi.net/presets/deno` | `#DenoRuntime` |
| `tomei.terassyi.net/presets/bun` | `#BunRuntime` |

**Runtimes:**

```cue
package tomei

import (
	gopreset "tomei.terassyi.net/presets/go"
	"tomei.terassyi.net/presets/rust"
)

goRuntime: gopreset.#GoRuntime & {
	platform: {os: _os, arch: _arch}
	spec: version: "1.26.0"  // or "latest"
}

rustRuntime: rust.#RustRuntime & {
	spec: version: "stable"
}
```

**Tools via ToolSet:**

```cue
package tomei

import (
	gopreset "tomei.terassyi.net/presets/go"
	"tomei.terassyi.net/presets/aqua"
)

goTools: gopreset.#GoToolSet & {
	metadata: name: "go-tools"
	spec: tools: {
		gopls:       {package: "golang.org/x/tools/gopls", version: "latest"}
		staticcheck: {package: "honnef.co/go/tools/cmd/staticcheck", version: "latest"}
	}
}

cliTools: aqua.#AquaToolSet & {
	metadata: name: "cli-tools"
	spec: tools: {
		rg: {package: "BurntSushi/ripgrep", version: "latest"}
		jq: {package: "jqlang/jq", version: "latest"}
	}
}
```

### Platform Tags

CUE `@tag()` attributes are automatically injected by `tomei apply`, `tomei plan`, and `tomei cue eval`:

| Tag | Type | Example values |
|-----|------|----------------|
| `@tag(os)` | `string` | `linux`, `darwin` |
| `@tag(arch)` | `string` | `amd64`, `arm64` |
| `@tag(headless)` | `bool` | `true`, `false` |

Declare them in a platform file:

```cue
package tomei

_os:       string @tag(os)
_arch:     string @tag(arch)
_headless: bool | *false @tag(headless,type=bool)
```

### Version

Use a pinned version (`"1.26.0"`) for reproducibility, or `"latest"` for auto-resolution. Running `tomei apply --sync` re-resolves `"latest"` versions.

### Scaffold

`tomei cue scaffold` generates a starting template:

```bash
tomei cue scaffold tool      # Tool template
tomei cue scaffold toolset   # ToolSet template
tomei cue scaffold runtime   # Runtime template
```

For raw CUE examples without presets, see [`examples/minimal/`](examples/minimal/). For a full multi-runtime setup, see [`examples/real-world/`](examples/real-world/).

## Shell Integration

Add to your shell profile to set up runtime environment variables:

```bash
# bash / zsh
eval "$(tomei env)"

# fish
tomei env --shell fish | source
```

Enable shell completion:

```bash
# bash
source <(tomei completion bash)

# zsh
tomei completion zsh > "${fpath[1]}/_tomei"

# fish
tomei completion fish | source
```

## Documentation

- [Design](docs/design.md)
- [Architecture](docs/architecture.md)
- [CUE Schema Reference](docs/cue-schema.md)
- [CUE Ecosystem Integration](docs/cue-ecosystem.md)
- [Module Publishing](docs/module-publishing.md)
- [Releasing](docs/releasing.md)
- [Usage](docs/usage.md)
- [Examples](examples/)

## Acknowledgements

- [aqua](https://aquaproj.github.io/) and [aqua-registry](https://github.com/aquaproj/aqua-registry) — tomei uses the aqua registry as its primary tool metadata source. Thanks to the aqua project for maintaining a comprehensive registry of CLI tools.

## License

This project is licensed under the MIT License. See [LICENSE](LICENSE) and [NOTICE](NOTICE) for details.
