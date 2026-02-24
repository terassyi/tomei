# tomei

[![CI](https://github.com/terassyi/tomei/actions/workflows/ci.yaml/badge.svg)](https://github.com/terassyi/tomei/actions/workflows/ci.yaml)
[![Release](https://img.shields.io/github/v/release/terassyi/tomei)](https://github.com/terassyi/tomei/releases)
[![Go](https://img.shields.io/github/go-mod/go-version/terassyi/tomei)](https://go.dev/)
[![License](https://img.shields.io/github/license/terassyi/tomei)](LICENSE)
[![Go Report Card](https://goreportcard.com/badge/github.com/terassyi/tomei)](https://goreportcard.com/report/github.com/terassyi/tomei)

A declarative, idempotent development environment setup tool powered by [CUE](https://cuelang.org/).

The name "tomei" comes from the Japanese word "透明" — transparent. What you write is what you get, with nothing hidden in between.

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

You can try tomei without installing anything on your machine using a clean Ubuntu container:

```bash
make -C examples build
make -C examples run
```

### 1. Initialize

`tomei init` sets up the required directories, state file, and aqua registry:

```console
$ tomei init --yes
Initializing tomei...

Directories:
  ✓ ~/.config/tomei
  ✓ ~/.local/share/tomei
  ✓ ~/.local/bin

Schema:
  ✓ Available via import "tomei.terassyi.net/schema"

State:
  ✓ ~/.local/share/tomei/state.json

Registry:
  ✓ aqua-registry v4.467.0

Initialization complete!
```

### 2. Write manifests

Manifests use [CUE](https://cuelang.org/) with presets and schema imports for type-safe, platform-aware definitions. Run `tomei cue init` first to set up the CUE module.

`runtimes.cue` — install runtimes via presets:

```cue
package tomei

import (
	gopreset "tomei.terassyi.net/presets/go"
	"tomei.terassyi.net/presets/rust"
)

goRuntime: gopreset.#GoRuntime & {
	platform: {os: _os, arch: _arch}
	spec: version: "1.26.0"
}

rustRuntime: rust.#RustRuntime & {
	spec: version: "stable"
}
```

`tools.cue` — install tools via ToolSet presets:

```cue
package tomei

import (
	gopreset "tomei.terassyi.net/presets/go"
	"tomei.terassyi.net/presets/aqua"
)

goTools: gopreset.#GoToolSet & {
	metadata: name: "go-tools"
	spec: tools: {
		gopls:       {package: "golang.org/x/tools/gopls", version: "v0.21.1"}
		staticcheck: {package: "honnef.co/go/tools/cmd/staticcheck", version: "v0.7.0"}
	}
}

cliTools: aqua.#AquaToolSet & {
	metadata: name: "cli-tools"
	spec: tools: {
		rg: {package: "BurntSushi/ripgrep", version: "15.1.0"}
		fd: {package: "sharkdp/fd", version: "v10.3.0"}
		jq: {package: "jqlang/jq", version: "1.8.1"}
	}
}
```

For raw CUE examples without presets, see [`examples/minimal/`](examples/minimal/).

### 3. Plan

`tomei plan` shows the dependency graph and execution order before applying:

```console
$ tomei plan .
Planning changes for [.]

Found 7 resource(s)

Dependency Graph:
Installer/aqua
├── ToolSet/cli-tools [+ install]
Runtime/go (1.26.0) [+ install]
├── ToolSet/go-tools [+ install]
Runtime/rust (stable) [+ install]
Tool/mise [+ install]

Execution Order:
  Layer 1: Runtime/go, Runtime/rust, Tool/mise
  Layer 2: ToolSet/cli-tools, ToolSet/go-tools

Summary: 7 to install, 0 to upgrade, 0 to remove
```

Commands-pattern tools (like `mise`) have no dependencies, so they run in Layer 1 alongside runtimes. ToolSets that depend on runtimes or installers are scheduled in Layer 2. Resources within the same layer are installed in parallel.

### 4. Apply

`tomei apply` reconciles your environment to match the manifests:

```console
$ tomei apply .
Applying user-level resources from [.]

Downloads:
  ✓ Runtime/go 1.26.0

Commands:
 => Runtime/rust stable (rustup bootstrap)
 => Tool/mise (commands install)
 => Tool/mise done (1.2s)
 => Runtime/rust stable done (8.4s)
 => ToolSet/cli-tools (aqua install)
 => ToolSet/go-tools (go install)
 => ToolSet/cli-tools done (3.1s)
 => ToolSet/go-tools done (42.7s)

Summary:
  ✓ Installed: 7

Apply complete!
```

Running `tomei apply` again is idempotent — no changes are made if the state is already up to date.

### Circular dependency detection

tomei detects circular dependencies at validation time:

```console
$ tomei validate circular.cue
Validating configuration...

Resources:
  ✓ Installer/installer-a
  ✓ Tool/tool-b

Dependencies:
  ✗ circular dependency detected: [Installer/installer-a Tool/tool-b Installer/installer-a]

✗ Validation failed
```

## Commands

| Command | Description |
|---------|-------------|
| `tomei init` | Initialize directories, state file, and aqua registry |
| `tomei apply` | Reconcile environment to match manifests |
| `tomei plan` | Show dependency graph and execution order |
| `tomei validate` | Validate manifests without applying |
| `tomei get` | Display installed resources (table/wide/JSON) |
| `tomei env` | Output runtime environment variables for shell |
| `tomei logs` | Show installation logs from the last apply |
| `tomei doctor` | Diagnose the environment |
| `tomei state diff` | Compare state backups |
| `tomei completion` | Generate shell completion scripts |
| `tomei uninit` | Remove tomei directories and state |
| `tomei version` | Print the version |
| `tomei cue init` | Initialize a CUE module for tomei manifests |
| `tomei cue scaffold` | Generate a manifest scaffold for a resource kind |
| `tomei cue eval` | Evaluate CUE manifests with `@tag()` injection |
| `tomei cue export` | Export CUE manifests as JSON |

## CUE Subcommands

### Scaffold

`tomei cue scaffold` generates a manifest template for a given resource kind:

```console
$ tomei cue scaffold tool
package tomei

import "tomei.terassyi.net/schema"

myTool: schema.#Tool & {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "my-tool"
    spec: {
        installerRef: "aqua"
        version:      "1.0.0"
        // package:      "owner/repo"
        // runtimeRef:   "go"
        // source: {
        //     url: "https://example.com/tool.tar.gz"
        // }
        // commands: {
        //     install: ["curl -fsSL https://example.com/install.sh | sh"]
        // }
        // args: ["--flag"]
    }
}
```

Supported kinds: `tool`, `toolset`, `runtime`.

### Eval / Export

`tomei cue eval` evaluates manifests with `@tag()` injection and prints the CUE output. `tomei cue export` does the same but outputs JSON:

```console
$ tomei cue eval .
$ tomei cue export .
```

These commands inject `@tag(os)`, `@tag(arch)`, and `@tag(headless)` automatically, just like `tomei apply`.

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
