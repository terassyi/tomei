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

Rather than reinventing package managers, tomei delegates to existing tools like, `go install`, `cargo install`. `tomei` orchestrates; they execute.

Native [aqua registry](https://github.com/aquaproj/aqua-registry) integration lets you install thousands of CLI tools by just specifying a package name and version.

[CUE](https://cuelang.org/) provides schema validation, platform-aware `@tag()` injection (`_os`, `_arch`, `_headless`), and type-safe manifest composition.

## Install

### GitHub Releases

Download a binary from the [Releases](https://github.com/terassyi/tomei/releases) page.

```bash
curl -Lo tomei.tar.gz https://github.com/terassyi/tomei/releases/latest/download/tomei_<version>_<os>_<arch>.tar.gz
tar xzf tomei.tar.gz
sudo mv tomei /usr/local/bin/
```

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

`runtime.cue` — install a Go runtime and tools via `go install` (runtime delegation):

```cue
package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

goRuntime: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Runtime"
	metadata: name: "go"
	spec: {
		type:    "download"
		version: "1.26.0"
		source: {
			url: "https://go.dev/dl/go\(spec.version).\(_os)-\(_arch).tar.gz"
			checksum: url: "https://go.dev/dl/?mode=json&include=all"
		}
		binaries: ["go", "gofmt"]
		binDir:      "~/go/bin"
		toolBinPath: "~/go/bin"
		env: {
			GOROOT: "~/.local/share/tomei/runtimes/go/\(spec.version)"
			GOBIN:  "~/go/bin"
		}
		commands: {
			install: "go install {{.Package}}@{{.Version}}"
			remove:  "rm -f {{.BinPath}}"
		}
	}
}

gopls: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "gopls"
	spec: {
		runtimeRef: "go"
		package:    "golang.org/x/tools/gopls"
		version:    "v0.21.0"
	}
}

staticcheck: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "staticcheck"
	spec: {
		runtimeRef: "go"
		package:    "honnef.co/go/tools/cmd/staticcheck"
		version:    "v0.6.0"
	}
}

goimports: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "goimports"
	spec: {
		runtimeRef: "go"
		package:    "golang.org/x/tools/cmd/goimports"
		version:    "v0.31.0"
	}
}
```

`tools.cue` — install CLI tools via aqua registry:

```cue
package tomei

ripgrep: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "rg"
	spec: {
		installerRef: "aqua"
		version:      "15.1.0"
		package:      "BurntSushi/ripgrep"
	}
}

fd: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "fd"
	spec: {
		installerRef: "aqua"
		version:      "v10.3.0"
		package:      "sharkdp/fd"
	}
}

jq: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "jq"
	spec: {
		installerRef: "aqua"
		version:      "jq-1.8.1"
		package:      "jqlang/jq"
	}
}

bat: {
	apiVersion: "tomei.terassyi.net/v1beta1"
	kind:       "Tool"
	metadata: name: "bat"
	spec: {
		installerRef: "aqua"
		version:      "v0.26.1"
		package:      "sharkdp/bat"
	}
}
```

### 3. Plan

`tomei plan` shows the dependency graph and execution order before applying:

```console
$ tomei plan .
Planning changes for [.]

Found 8 resource(s)

Dependency Graph:
Installer/aqua
├── Tool/bat (v0.26.1) [+ install]
├── Tool/fd (v10.3.0) [+ install]
├── Tool/jq (jq-1.8.1) [+ install]
└── Tool/rg (15.1.0) [+ install]
Runtime/go (1.26.0) [+ install]
├── Tool/goimports (v0.31.0) [+ install]
├── Tool/gopls (v0.21.0) [+ install]
└── Tool/staticcheck (v0.6.0) [+ install]

Execution Order:
  Layer 1: Runtime/go
  Layer 2: Tool/bat, Tool/fd, Tool/goimports, Tool/gopls, Tool/jq, Tool/rg, Tool/staticcheck

Summary: 8 to install, 0 to upgrade, 0 to remove
```

gopls, staticcheck, and goimports depend on the Go runtime, so they are scheduled in Layer 2. All Layer 2 resources are installed in parallel.

### 4. Apply

`tomei apply` reconciles your environment to match the manifests:

```console
$ tomei apply .
Applying user-level resources from [.]

Downloads:
  ✓ Runtime/go 1.26.0

Commands:
 => Tool/jq jq-1.8.1 (aqua install)
 => Tool/fd v10.3.0 (aqua install)
 => Tool/bat v0.26.1 (aqua install)
 => Tool/goimports v0.31.0 (go install)
 => Tool/gopls v0.21.0 (go install)
 => Tool/fd v10.3.0 done (1.5s)
 => Tool/rg 15.1.0 (aqua install)
 => Tool/jq jq-1.8.1 done (2.3s)
 => Tool/staticcheck v0.6.0 (go install)
 => Tool/bat v0.26.1 done (2.4s)
 => Tool/rg 15.1.0 done (1.7s)
 => Tool/goimports v0.31.0 done (21.8s)
 => Tool/staticcheck v0.6.0 done (32.4s)
 => Tool/gopls v0.21.0 done (45.9s)

Summary:
  ✓ Installed: 8

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
