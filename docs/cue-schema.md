# CUE Schema Reference

This document describes the CUE schema used by `tomei` manifests. The source of truth is [`internal/config/schema/schema.cue`](../internal/config/schema/schema.cue).

## Basics

Every resource in a `tomei` manifest belongs to `package tomei` and follows a common structure:

```cue
package tomei

myResource: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "<ResourceType>"
    metadata: {
        name:         "<resource-name>"      // lowercase alphanumeric, dots, hyphens, underscores
        description?: string                 // optional human-readable description
        labels?: {[string]: string}          // optional key-value pairs
    }
    spec: { ... }
}
```

`metadata.name` must match the pattern `^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`.

## Resource Types

### Runtime

Language runtime definition. Supports two installation patterns.

#### Download pattern

tomei downloads and extracts a tarball directly.

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Runtime"
metadata: name: "go"
spec: {
    type:    "download"
    version: "1.25.6"
    source: {
        url: "https://go.dev/dl/go\(spec.version).\(_env.os)-\(_env.arch).tar.gz"
        checksum: url: "https://go.dev/dl/?mode=json&include=all"
    }
    binaries:    ["go", "gofmt"]
    binDir:      "~/.local/share/tomei/runtimes/go/\(spec.version)/bin"
    toolBinPath: "~/go/bin"
    commands: {
        install: "go install {{.Package}}@{{.Version}}"
        remove:  "rm -f {{.BinPath}}"
    }
    env: {
        GOROOT: "~/.local/share/tomei/runtimes/go/\(spec.version)"
        GOBIN:  "~/go/bin"
    }
}
```

#### Delegation pattern

Delegates installation to an external script or tool (e.g., rustup, nvm).

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Runtime"
metadata: name: "rust"
spec: {
    type:    "delegation"
    version: "stable"
    bootstrap: {
        install:        "curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain {{.Version}}"
        check:          "rustc --version"
        remove:         "rustup self uninstall -y"
        resolveVersion: "rustup check 2>/dev/null | grep -oP 'stable-.*?: \\K[0-9.]+' || echo ''"
    }
    toolBinPath: "~/.cargo/bin"
    commands: {
        install: "cargo install {{.Package}}{{if .Version}} --version {{.Version}}{{end}}"
    }
    env: {
        CARGO_HOME:  "~/.cargo"
        RUSTUP_HOME: "~/.rustup"
    }
}
```

#### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.type` | `"download"` \| `"delegation"` | yes | Installation pattern |
| `spec.version` | string | yes | Version string (exact, `"stable"`, `"latest"`) |
| `spec.toolBinPath` | string | yes | Directory where tools installed via this runtime are placed |
| `spec.source` | [DownloadSource](#downloadsource) | download only | Download URL and checksum |
| `spec.bootstrap` | [RuntimeBootstrap](#runtimebootstrap) | delegation only | Install/check/remove commands for the runtime itself |
| `spec.binaries` | []string | no | Executable names in the runtime (e.g., `["go", "gofmt"]`) |
| `spec.binDir` | string | no | Directory containing runtime binaries |
| `spec.commands` | [CommandSet](#commandset) | no | Commands for installing tools via this runtime |
| `spec.env` | map[string]string | no | Environment variables (e.g., `GOROOT`, `GOBIN`) |

### Tool

Individual tool definition. Uses either `installerRef` or `runtimeRef` (mutually exclusive).

#### Via aqua registry

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "rg"
spec: {
    installerRef: "aqua"
    version:      "15.1.0"
    package:      "BurntSushi/ripgrep"
}
```

#### Via explicit download

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "gh"
spec: {
    installerRef: "download"
    version:      "2.62.0"
    source: {
        url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_\(_env.os)_\(_env.arch).tar.gz"
        checksum: url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_checksums.txt"
    }
}
```

#### Via runtime delegation

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "gopls"
spec: {
    runtimeRef: "go"
    package:    "golang.org/x/tools/gopls"
    version:    "v0.21.0"
}
```

#### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.installerRef` | string | no* | Reference to an Installer (e.g., `"aqua"`, `"download"`) |
| `spec.runtimeRef` | string | no* | Reference to a Runtime (e.g., `"go"`, `"rust"`) |
| `spec.repositoryRef` | string | no | Reference to an InstallerRepository |
| `spec.version` | string | no | Tool version |
| `spec.enabled` | bool | no | Default `true`. Set `false` to skip |
| `spec.source` | [DownloadSource](#downloadsource) | no | Explicit download source |
| `spec.package` | [Package](#package) | no | Package identifier for registry or delegation |

\* One of `installerRef` or `runtimeRef` is required.

### ToolSet

A set of tools sharing the same installer or runtime. Expanded into individual Tool resources at load time.

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "ToolSet"
metadata: name: "go-tools"
spec: {
    runtimeRef: "go"
    tools: {
        gopls:       {package: "golang.org/x/tools/gopls", version: "v0.21.0"}
        staticcheck: {package: "honnef.co/go/tools/cmd/staticcheck", version: "v0.6.0"}
    }
}
```

#### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.installerRef` | string | no | Shared installer for all tools |
| `spec.runtimeRef` | string | no | Shared runtime for all tools |
| `spec.repositoryRef` | string | no | Shared repository reference |
| `spec.tools` | map | yes | Tool definitions (same fields as Tool.spec minus installerRef/runtimeRef) |

### Installer

User-level installer definition. The `aqua` installer is provided as a builtin and does not need to be declared.

#### Delegation pattern (depends on a Tool)

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Installer"
metadata: name: "binstall"
spec: {
    type:    "delegation"
    toolRef: "cargo-binstall"
    commands: {
        install: "cargo binstall -y {{.Package}}{{if .Version}}@{{.Version}}{{end}}"
        check:   "cargo binstall --info {{.Package}}"
        remove:  "cargo uninstall {{.Package}}"
    }
}
```

#### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.type` | `"download"` \| `"delegation"` | yes | Installer pattern |
| `spec.runtimeRef` | string | no | Dependency on a Runtime |
| `spec.toolRef` | string | no | Dependency on a Tool |
| `spec.bootstrap` | [CommandSet](#commandset) | no | Self-installation commands |
| `spec.commands` | [CommandSet](#commandset) | delegation only | Commands for installing tools |

### InstallerRepository

Third-party tool metadata repository.

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "InstallerRepository"
metadata: name: "bitnami"
spec: {
    installerRef: "helm"
    source: {
        type: "delegation"
        url:  "https://charts.bitnami.com/bitnami"
        commands: {
            install: "helm repo add bitnami https://charts.bitnami.com/bitnami"
            check:   "helm repo list 2>/dev/null | grep -q ^bitnami"
            remove:  "helm repo remove bitnami"
        }
    }
}
```

#### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.installerRef` | string | yes | Reference to an Installer |
| `spec.source.type` | `"delegation"` \| `"git"` | yes | Repository source type |
| `spec.source.url` | HTTPS URL | git only | Repository URL |
| `spec.source.commands` | [CommandSet](#commandset) | delegation only | Repository management commands |

## Common Types

### DownloadSource

```cue
#DownloadSource: {
    url:          string & =~"^https://"    // HTTPS only
    checksum?: {
        value?:       string & =~"^sha256:[a-f0-9]{64}$"  // inline checksum
        url?:         string & =~"^https://"               // checksum file URL
        filePattern?: string                                // glob for matching in checksum file
    }
    archiveType?: "tar.gz" | "zip" | "raw"
    asset?:       string                    // GitHub release asset name
}
```

Provide either `checksum.value` (inline) or `checksum.url` (remote checksum file). When using `checksum.url`, the `filePattern` field can narrow matching within the file.

### Package

Accepts two forms:

```cue
// String shorthand
package: "BurntSushi/ripgrep"        // owner/repo (for aqua registry)
package: "golang.org/x/tools/gopls"  // module path (for go install)

// Object form
package: {owner: "BurntSushi", repo: "ripgrep"}
package: {name: "golang.org/x/tools/gopls"}
```

### CommandSet

```cue
#CommandSet: {
    install: string & !=""   // required
    check?:  string          // verify installation (exit 0 = installed)
    remove?: string          // uninstall command
}
```

Commands support Go template variables: `{{.Package}}`, `{{.Version}}`, `{{.Name}}`, `{{.BinPath}}`.

### RuntimeBootstrap

Extends CommandSet with version resolution support.

```cue
#RuntimeBootstrap: {
    install:         string & !=""   // required
    check?:          string          // required for delegation Runtimes
    remove?:         string
    resolveVersion?: string          // resolve aliases like "stable" to actual version
}
```

## Environment Overlay (`_env`)

`tomei` automatically injects an `_env` hidden field into every CUE file at load time. Use it to write platform-aware manifests.

### Available variables

| Variable | Type | Values | Description |
|----------|------|--------|-------------|
| `_env.os` | string | `"linux"`, `"darwin"` | Operating system |
| `_env.arch` | string | `"amd64"`, `"arm64"` | CPU architecture |
| `_env.headless` | bool | | Headless environment (container, SSH, CI, no display) |
| `_env.platform.os.go` | string | `"linux"`, `"darwin"` | Same as `_env.os` |
| `_env.platform.os.apple` | string | `"Linux"`, `"macOS"` | Apple-style naming |
| `_env.platform.arch.go` | string | `"amd64"`, `"arm64"` | Same as `_env.arch` |
| `_env.platform.arch.gnu` | string | `"x86_64"`, `"aarch64"` | GNU-style naming |

### Headless detection

`_env.headless` is `true` when any of the following conditions apply:

- Running in a container (Docker, Kubernetes, LXC, containerd)
- No `DISPLAY` or `WAYLAND_DISPLAY` set on Linux
- SSH session (`SSH_CLIENT` or `SSH_TTY` set)
- CI environment (`CI` variable set)

### Conditional configuration

Use CUE `if` expressions with `_env` to branch by platform:

```cue
package tomei

_ghVersion: "2.62.0"

gh: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "gh"
    spec: {
        installerRef: "download"
        version:      _ghVersion
        source: {
            if _env.os == "linux" {
                url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_linux_\(_env.arch).tar.gz"
            }
            if _env.os == "darwin" {
                url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_macOS_\(_env.arch).zip"
            }
            checksum: url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_checksums.txt"
        }
    }
}
```

URL interpolation using `_env`:

```cue
source: {
    url: "https://go.dev/dl/go\(spec.version).\(_env.os)-\(_env.arch).tar.gz"
}
```

Using alternative naming conventions:

```cue
source: {
    url: "https://example.com/tool-\(_env.platform.arch.gnu)-\(_env.platform.os.apple).tar.gz"
    // resolves to e.g. tool-x86_64-Linux.tar.gz or tool-aarch64-macOS.tar.gz
}
```

## Schema Management

`tomei init` places `schema.cue` in the current directory (or the directory specified by `--schema-dir`). This file provides type definitions for CUE language servers. To update `schema.cue` after upgrading `tomei`, run `tomei schema`.

The schema is versioned via `#APIVersion`. If the on-disk `schema.cue` has a different apiVersion than the binary, `tomei apply`, `tomei plan`, and `tomei validate` will exit with an error.

## Validation

`tomei` validates manifests against the embedded CUE schema at load time. Run `tomei validate <path>` to check manifests without applying.

Validation checks:

- CUE syntax errors
- Schema conformance (field types, required fields, enum values)
- `metadata.name` regex pattern
- HTTPS-only URLs
- Circular dependency detection in the resource graph

### Common errors

| Error | Cause |
|-------|-------|
| `field not allowed` | Unknown field in spec |
| `conflicting values` | Type mismatch (e.g., string where bool expected) |
| `incomplete value` | Required field missing |
| `circular dependency detected` | Resource dependency cycle |
