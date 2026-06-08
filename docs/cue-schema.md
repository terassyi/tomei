# CUE Schema Reference

This document describes the CUE schema used by `tomei` manifests. The source of truth is [`cuemodule/schema/schema.cue`](../cuemodule/schema/schema.cue).

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
_os:   string @tag(os)
_arch: string @tag(arch)

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
| `spec.toolBinPath` | string | conditional | Directory where tools installed via this runtime are placed. Required when `spec.commands` is defined |
| `spec.source` | [DownloadSource](#downloadsource) | download only | Download URL and checksum |
| `spec.bootstrap` | [RuntimeBootstrap](#runtimebootstrap) | delegation only | Install/check/remove commands for the runtime itself |
| `spec.binaries` | []string | no | Executable names in the runtime (e.g., `["go", "gofmt"]`) |
| `spec.binDir` | string | no | Directory containing runtime binaries |
| `spec.commands` | [CommandSet](#commandset) | no | Commands for installing tools via this runtime |
| `spec.env` | map[string]string | no | Environment variables (e.g., `GOROOT`, `GOBIN`) |
| `spec.minimumReleaseAge` | string | no | Go duration (e.g. `"168h"`); not enforced for runtimes — exposed to the runtime's install/bootstrap commands as `{{.MinimumReleaseAge}}`. See [Minimum release age](#minimum-release-age) |

### Tool

Individual tool definition. Uses either `installerRef`, `runtimeRef`, or `commands` (mutually exclusive).

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
_os:   string @tag(os)
_arch: string @tag(arch)

apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "gh"
spec: {
    installerRef: "download"
    version:      "2.62.0"
    source: {
        url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_\(_os)_\(_arch).tar.gz"
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

##### Pinning to a git commit SHA (`sha`)

For `runtimeRef: "go"` tools, you can pin to a specific commit instead of
a tag/version. `go install pkg@<sha>` resolves the SHA through GOPROXY and
verifies the resulting module zip against the GOSUMDB transparency log,
so the install is reproducible and integrity-checked.

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "gopls"
spec: {
    runtimeRef: "go"
    package:    "golang.org/x/tools/gopls"
    sha:        "0123456789abcdef0123456789abcdef01234567"
}
```

- `sha` must be a 40-character lowercase hex SHA-1. Short SHAs and
  tag-like strings are rejected at validate time.
- `sha` and `version` are mutually exclusive.
- `sha` is currently supported only with `runtimeRef: "go"`. Other
  installers (aqua, cargo, npm, ...) have no equivalent SHA-pin path
  with comparable integrity guarantees and reject `sha` at validate time.
- The plan tree renders `sha` as a 12-character prefix + ellipsis (e.g.
  `(sha: 0123456789ab…)`); state retains the full SHA.

#### Via self-managed commands

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "Tool"
metadata: name: "claude"
spec: {
    commands: {
        install:        ["curl -fsSL https://cli.claude.ai/install.sh | sh"]
        update:         ["claude update"]
        check:          ["claude --version"]
        remove:         ["claude uninstall"]
        resolveVersion: ["claude --version 2>/dev/null | grep -oP '\\d+\\.\\d+\\.\\d+'"]
    }
}
```

#### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.installerRef` | string | no* | Reference to an Installer (e.g., `"aqua"`, `"download"`) |
| `spec.runtimeRef` | string | no* | Reference to a Runtime (e.g., `"go"`, `"rust"`) |
| `spec.commands` | [ToolCommandSet](#toolcommandset) | no* | Shell commands for self-managed tool installation |
| `spec.repositoryRef` | string | no | Reference to an InstallerRepository |
| `spec.version` | string | no | Tool version (mutually exclusive with `sha`) |
| `spec.sha` | string | no | 40-char lowercase hex git commit SHA for SHA-pinned install. Only allowed with `runtimeRef: "go"`. Mutually exclusive with `version`. |
| `spec.enabled` | bool | no | Default `true`. Set `false` to skip |
| `spec.source` | [DownloadSource](#downloadsource) | no | Explicit download source |
| `spec.package` | [Package](#package) | no | Package identifier for registry or delegation |
| `spec.binaryName` | string | no | Override binary name for both the placed binary and the symlink (e.g., `"kubectl-krew"` for krew). Affects `state.installPath` and `state.binPath`. Must match `^[a-zA-Z0-9][a-zA-Z0-9._-]*$` |
| `spec.privileged` | bool | no | Default `false`. Opt into elevated placement for download/registry/`commands` tools: the symlink is placed in the system bin directory (default `/usr/local/bin`) via a `sudo` fallback instead of `~/.local/bin`. Ignored for installer/name-delegation tools; **rejected with `runtimeRef`**. Requires `tomei apply --system` (or `--system-only`); without it the tool is skipped. See [design.md §11 Privileged Tools](./design.md#11-privileged-tools). |

\* Exactly one of `installerRef`, `runtimeRef`, or `commands` is required.

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
| `spec.tools` | map | yes | Tool definitions (same fields as Tool.spec minus installerRef/runtimeRef). Each tool supports `version`, `sha`, `enabled`, `source`, `package`, `binaryName`, `args` |

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
| `spec.runtimeRef` | string | no | Dependency on a Runtime (mutually exclusive with toolRef) |
| `spec.toolRef` | string | no | Dependency on a Tool for PATH injection (mutually exclusive with runtimeRef) |
| `spec.dependsOn` | `[...string]` | no | Additional tool dependencies for DAG ordering only (no PATH injection). Overlap with toolRef is tolerated and deduplicated |
| `spec.bootstrap` | [CommandSet](#commandset) | no | Self-installation commands |
| `spec.commands` | [CommandSet](#commandset) | delegation only | Commands for installing tools |
| `spec.binDir` | string | no | Directory where delegation installers place binaries. Used by `tomei env` to include in PATH. Must start with `~/` or `/`. Only meaningful for delegation type |
| `spec.minimumReleaseAge` | string | no | Go duration (e.g. `"168h"`); refuse installs whose upstream release is younger. Empty disables. See [Minimum release age](#minimum-release-age) |

#### Minimum release age

`minimumReleaseAge` is a supply-chain defense: refuse to install a tool version
whose upstream release is younger than the threshold, giving the community time
to flag a compromised release before you pull it. It is set on the
`Installer`/`Runtime` spec and applies to tools that reference it. See the
[threat model](design.md#minimum-release-age) for the rationale and limitations.

```cue
// Enable the gate for all aqua-installed tools by overriding the builtin
// "aqua" installer. The override MUST keep type: "download".
aqua: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Installer"
    metadata: name: "aqua"
    spec: {
        type:              "download"
        minimumReleaseAge: "168h" // 1 week
    }
}
```

- **Format**: a [Go duration](https://pkg.go.dev/time#ParseDuration) string.
  There is **no day unit** — use `"24h"` for a day and `"168h"` for a week
  (`"7d"` is invalid). Decimals are allowed (`"1.5h"`). An empty string, `"0"`,
  or `"0h"` all disable the gate; negative values are rejected at validate time.
- **Opt-in**: the gate is off unless you declare it. The builtin `aqua` and
  `download` installers carry no threshold, so you enable enforcement by
  declaring an override (as above) — or a custom `type: "download"` installer —
  that sets `minimumReleaseAge`. There is no global default.
- **Where tomei enforces.** Whether the gate is *active* depends on the
  installer's *type* (any `type: "download"` installer with a threshold), not its
  name. Which timestamp *source* is used is then chosen by the **tool**, not the
  installer: a registry `package` (owner/repo) uses the GitHub Releases
  `published_at`; otherwise an explicit `source.url` uses the HTTP `Last-Modified`
  header.

| Tool's install path | Enforcement |
|---|---|
| via a `type: "download"` installer (incl. the builtin `aqua`, when overridden with a threshold), with a registry `package` | tomei enforces — GitHub `published_at` (**best-effort**: see limitations) |
| via a `type: "download"` installer (incl. the builtin `download`), with an explicit `source.url` | tomei enforces — HTTP `Last-Modified` (**best-effort**: see limitations) |
| via a delegation installer (binstall, brew, custom) | **Not enforced by tomei** — the user's `commands.install` must honor the `{{.MinimumReleaseAge}}` template var |
| via `runtimeRef` (any runtime — `go`, `cargo`, `uv`, … — regardless of the runtime's `type`) | **Not enforced by tomei** — `runtimeRef` installs are never gated; the runtime's install commands must honor the var |
| Commands pattern (Tool with `commands:`) | Out of scope — not gated |

- For delegation paths the value is exposed only as the `{{.MinimumReleaseAge}}`
  [command template variable](#commandset) (rendered as the literal duration
  string, e.g. `168h`); tomei performs no release-age check itself. `tomei plan`
  and `tomei validate` emit a lint warning when the field is set but no install
  command references the variable. See [usage](usage.md#minimum-release-age-gate).

Tracking issue: [#257](https://github.com/terassyi/tomei/issues/257).

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

### SystemInstaller

System-level package manager. Requires `--system` to be applied. Only `apt` is currently wired into the engine; declaring other package managers can fail at apply time in several ways depending on the host:

- error contains `unknown package manager "<x>"` — `metadata.name` isn't one of the known identifiers (`apt`, `dnf`, `zypper`, `pacman`, `apk`)
- error contains `package manager "<x>" is not supported on this system` (with appended distro context such as `(ID=..., ID_LIKE=...)`) — the host's distro does not match
- error contains `no version function registered for package manager "<x>"` — the package manager is not yet wired up in tomei
- error contains `failed to get version for "<x>"` — the version probe itself failed (e.g., the package manager binary is missing or returned an unexpected output)
- error contains `system: installer: package manager validation unavailable (distro detection failed or unsupported platform)` — running on a host where distro detection isn't available (e.g., macOS, minimal containers)

`metadata.name` must match a known package manager identifier (`apt`, `dnf`, `zypper`, `pacman`, `apk`); identifiers other than `apt` are not currently supported by the engine.

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "SystemInstaller"
metadata: name: "apt"
spec: {
    pattern:    "delegation"
    privileged: true
    commands: {
        install: {command: "sudo apt-get install -y"}
        remove:  {command: "sudo apt-get remove -y"}
        check:   {command: "dpkg -s"}
    }
}
```

#### Fields

The CUE schema requires `spec.pattern`, `spec.privileged`, and `spec.commands` (with `commands` as an open struct). Beyond that, current Go-side validation only checks `spec.pattern`, and the engine validates `SystemInstaller` resources by checking that the named package manager exists on the host (see error list above). The concrete installer is APT-based and hardcodes its `apt-get` / `dpkg-query` invocations (see [SystemPackageRepository](#systempackagerepository) and [SystemPackageSet](#systempackageset)); it does **not** read `spec.commands.*`. The `commands.*` entries below are therefore inert at apply time. The schema only requires the `commands` object itself (an open struct); the `install` / `remove` / `check` subkeys are not enforced — they are the conventional shape shown in the example and document what a future non-`apt` installer would consume.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.pattern` | string | yes | Installer pattern. Currently only `"delegation"` is meaningful |
| `spec.privileged` | bool | yes | Whether package operations require elevated privileges |
| `spec.commands.install` | object | no | `{command: string}` — inert; the APT installer hardcodes `apt-get install` and ignores this |
| `spec.commands.remove` | object | no | `{command: string}` — inert; the APT installer hardcodes `apt-get remove` and ignores this |
| `spec.commands.check` | object | no | `{command: string}` — inert; the APT installer probes via `dpkg-query` and ignores this |
| `spec.commands.update` | string | no | Optional update command; inert for the APT installer (which runs `apt-get update` directly) |

Because the only wired-in installer (`apt`) hardcodes its package operations, these `commands.*` declarations are inert for `apt` today. They are kept as the conventional shape for forward compatibility; if a non-`apt` installer is added later, any Go template-variable conventions for these commands will be documented as part of that work.

### SystemPackageRepository

Third-party package repository (e.g., Docker, Kubernetes). The resource
is a discriminated union keyed by `spec.installerRef`: the matching
installer-specific block (today only `spec.apt`) supplies the source
configuration. Additional arms for dnf / apk / pacman are tracked in
[#213](https://github.com/terassyi/tomei/issues/213); the union shape
already accommodates them without future migrations of manifests that
use the existing `apt` arm.

The APT concrete installer places a per-repository GPG keyring at
`/usr/share/keyrings/<metadata.name>.gpg` and a one-line `.list` fragment
at `/etc/apt/sources.list.d/<metadata.name>.list`, then refreshes the APT
index. If the just-added repository fails to fetch
(`W: Failed to fetch …` against the configured URL), the helper rolls
back both files automatically so the host state does not regress.
Declared repositories run through this installer at apply time; the
engine wiring shipped in [#196](https://github.com/terassyi/tomei/issues/196).

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "SystemPackageRepository"
metadata: name: "docker"
spec: {
    installerRef: "apt"
    apt: {
        url:        "https://download.docker.com/linux/ubuntu"
        keyUrl:     "https://download.docker.com/linux/ubuntu/gpg"
        keyHash:    "sha256:1500c1f56fa9e26b9b8f42452a553675796ade0807cdce11975eb98170b3a570"
        suite:      "jammy"
        components: ["stable"]
        options: {
            arch: "amd64"
        }
    }
}
```

#### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.installerRef` | `"apt"` | yes | Selects the source-block arm. Today only `"apt"` is supported |
| `spec.apt.url` | HTTPS URL | yes | Repository base URL (the value before suite/components in the emitted `deb` line) |
| `spec.apt.keyUrl` | HTTPS URL | yes | URL of the ASCII-armored GPG public key. May legitimately be served from a different host than `url` (e.g. kubernetes's `pkgs.k8s.io` repo with a key on `packages.cloud.google.com`) |
| `spec.apt.keyHash` | string | yes | SHA-256 of the armored key in `sha256:<64-lowercase-hex>` form. Required as defense-in-depth: HTTPS alone protects only against passive MITM, not against CDN or upstream-mirror compromise |
| `spec.apt.suite` | string | yes | Distribution release (e.g. `jammy`, `noble`, `bookworm`). Single-suite by design. Flat repositories — APT's `deb URL ./` syntax where the suite token is the literal `./` — are unsupported; the schema rejects `./`, `/`, `.`, and `..` as suite values |
| `spec.apt.components` | `[...string]` | yes | One or more pool components (e.g. `["stable"]`, `["main", "contrib", "non-free"]`) |
| `spec.apt.options` | `{[string]: string}` | no | Bracketed sources.list options. Allowed keys: `arch`, `target`, `by-hash`, `pdiffs`, `check-valid-until`, `lang`. `signed-by` is auto-derived from `metadata.name` and must not be set here. `trusted=yes`, `allow-insecure`, `allow-weak`, `allow-downgrade-to-insecure` are rejected by both the CUE schema and `AptSource.Validate` because they disable or weaken signature verification |

The keyring is always installed to `/usr/share/keyrings/<metadata.name>.gpg`
and the rendered `signed-by` value matches that path verbatim. Custom
keyring locations (e.g. `/etc/apt/keyrings/`) are not currently supported
via the schema; the install destination is one source of truth.

> **Note:** The schema is a discriminated union keyed by `installerRef`.
> Today only the `apt` arm is implemented; dnf / apk / pacman arms are
> tracked in #213. Migrating manifests written against pre-#195 tomei:
>
> 1. Rename `spec.source` to `spec.apt`.
> 2. Confirm `keyUrl`, `keyHash`, `suite`, and `components` are all set
>    — they became required when the concrete installer landed in #195.
> 3. If `options` carried `signed-by`, remove it (auto-derived from
>    `metadata.name`). If it carried `trusted=yes`, `allow-insecure`,
>    `allow-weak`, or `allow-downgrade-to-insecure`, remove them — the
>    schema rejects these because they disable or weaken signature
>    verification.

### SystemPackage

Single-package shorthand for [SystemPackageSet](#systempackageset). A `SystemPackage` manifest is rewritten into a one-element `SystemPackageSet` at load time — `tomei plan` and `tomei apply` output show `SystemPackageSet/<name>`, never `SystemPackage/<name>`.

Use `SystemPackage` when you want to declare an OS package as its own resource (one resource per package, with its own `metadata.name` and dependency edges). Use [SystemPackageSet](#systempackageset) for batched declarations.

```cue
git: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "SystemPackage"
    metadata: name: "git"
    spec: {
        installerRef: "apt"
        package:      "git"
    }
}

// docker uses an upstream apt repository.
docker: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "SystemPackage"
    metadata: name: "docker"
    spec: {
        installerRef:  "apt"
        repositoryRef: "docker"
        package:       "docker-ce"
    }
}
```

#### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.installerRef` | string | yes | Reference to a [SystemInstaller](#systeminstaller) |
| `spec.repositoryRef` | string | no | Reference to a [SystemPackageRepository](#systempackagerepository) — adds a DAG edge so the repository is registered before the package is installed |
| `spec.package` | string | yes | Package name passed to the installer. Must be a non-whitespace token (`^\S+$` — no spaces, tabs, or newlines). Per-installer grammar (version pins like `nodejs=18.*`, multiarch suffixes like `libc6:i386`, group prefixes like `@core`) is accepted verbatim within that constraint |

`metadata.name` is a tomei resource identifier and is independent from `spec.package` — e.g., `name: "docker"` with `package: "docker-ce"`.

### SystemPackageSet

Set of system packages installed via a [SystemInstaller](#systeminstaller). The concrete installer (APT-based) runs `apt-get install` / `apt-get remove` and probes installed versions via `dpkg-query` after install. On non-apt hosts the resource fails with a clear "platform unsupported" error.

```cue
// Distribution-provided packages — no repositoryRef
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "SystemPackageSet"
metadata: name: "build-essential"
spec: {
    installerRef: "apt"
    packages: [
        "build-essential",
        "pkg-config",
        "libssl-dev",
    ]
}

// Packages from a third-party repository
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "SystemPackageSet"
metadata: name: "docker"
spec: {
    installerRef:  "apt"
    repositoryRef: "docker"   // matches the SystemPackageRepository above
    packages: [
        "docker-ce",
        "docker-ce-cli",
        "containerd.io",
    ]
}
```

#### Fields

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `spec.installerRef` | string | yes | Reference to a [SystemInstaller](#systeminstaller) |
| `spec.repositoryRef` | string | no | Reference to a [SystemPackageRepository](#systempackagerepository) — adds a DAG edge so the repository is registered before packages are installed |
| `spec.packages` | `[...string]` | yes | Package names to install. Each element must be a non-whitespace token (`^\S+$`); the list itself must be non-empty |

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
    archiveType?: "tar.gz" | "tar.xz" | "zip" | "raw"
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

Commands support Go template variables: `{{.Package}}`, `{{.Version}}`, `{{.Name}}`, `{{.BinPath}}`, `{{.MinimumReleaseAge}}` (the configured [minimum release age](#minimum-release-age), rendered as the literal duration string; tomei does not act on it for delegation paths).

### Aqua Template Variables

Aqua registry tools use Go templates for `source.url`, `source.asset`, `source.checksum.url`, and `files[].src`. The following variables are available:

| Variable | Description | Example |
|----------|-------------|---------|
| `{{.Version}}` | Package version (raw tag) | `v10.3.0` |
| `{{.SemVer}}` | Version with `version_prefix` stripped | `10.3.0` (when prefix is `v`) |
| `{{.OS}}` | OS name (after aqua replacements) | `linux`, `darwin` |
| `{{.Arch}}` | Architecture (after aqua replacements) | `amd64`, `x86_64` |
| `{{.Format}}` | Archive format | `tar.gz`, `zip` |
| `{{.Asset}}` | Rendered asset name | `fd-v10.3.0-x86_64-unknown-linux-gnu.tar.gz` |
| `{{.AssetWithoutExt}}` | Asset with archive extension stripped (`.tar.gz`, `.tar.xz`, `.zip`, etc.) | `fd-v10.3.0-x86_64-unknown-linux-gnu` |

Custom template functions: `trimV` (remove `v` prefix), `trimPrefix`, `trimSuffix`, `title` (capitalize first letter), `tolower`, `toupper`.

`{{.AssetWithoutExt}}` is useful in `files[].src` to reference paths inside archives, e.g., `{{.AssetWithoutExt}}/binary`.

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

### ToolCommandSet

Extends CommandSet with update and version resolution for self-managed tools.

```cue
#ToolCommandSet: {
    install:         [...string] & [_, ...]   // required
    update?:         [...string]              // optional update-in-place command
    check?:          [...string]              // verify installation
    remove?:         [...string]              // uninstall command
    resolveVersion?: [...string]              // capture installed version after install/update
}
```

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `install` | []string | yes | Command(s) to install the tool |
| `update` | []string | no | Command to update in-place. Falls back to `install` if not set |
| `check` | []string | no | Command to verify the tool is installed (exit 0 = success) |
| `remove` | []string | no | Command to uninstall the tool |
| `resolveVersion` | []string | no | Command to capture installed version. Supports `github-release:owner/repo:prefix`, `http-text:URL:regex`, or shell commands |

## Platform-Aware Manifests (`@tag()`)

`tomei` uses CUE's native `@tag()` feature to inject runtime environment values into manifests.

### Available tags

| Tag | Values | Description |
|-----|--------|-------------|
| `os` | `"linux"`, `"darwin"` | Operating system |
| `arch` | `"amd64"`, `"arm64"` | CPU architecture |
| `headless` | `true`, `false` | Headless environment |

### Headless detection

The `headless` tag is `true` when any of the following conditions apply:

- Running in a container (Docker, Kubernetes, LXC, containerd)
- No `DISPLAY` or `WAYLAND_DISPLAY` set on Linux
- SSH session (`SSH_CLIENT` or `SSH_TTY` set)
- CI environment (`CI` variable set)

### Usage

Declare `@tag()` in your CUE file to access environment values:

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
        }
        // ...
    }
}
```

### Conditional configuration

Use CUE `if` expressions with tag values to branch by platform:

```cue
package tomei

_os:   string @tag(os)
_arch: string @tag(arch)

_ghVersion: "2.62.0"

gh: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "gh"
    spec: {
        installerRef: "download"
        version:      _ghVersion
        source: {
            if _os == "linux" {
                url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_linux_\(_arch).tar.gz"
            }
            if _os == "darwin" {
                url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_macOS_\(_arch).zip"
            }
            checksum: url: "https://github.com/cli/cli/releases/download/v\(spec.version)/gh_\(spec.version)_checksums.txt"
        }
    }
}
```

### File-level platform branching (`@if()`)

CUE's `@if()` file-level attribute allows you to include or exclude entire files based on boolean tags. `tomei` automatically detects `@if()` references in your manifest files and injects the matching boolean tags for the current platform — no manual `-t` flags are needed. This is useful for separating platform-specific resources into dedicated files:

```cue
@if(darwin && arm64)

package tomei

// This file is only loaded on macOS.
brewTool: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "brew-tool"
    spec: {
        installerRef: "download"
        version:      "1.0.0"
        source: url: "https://example.com/tool_darwin.tar.gz"
    }
}
```

#### Available boolean tags

`tomei` recognizes the following identifiers in `@if()` attributes. When the condition matches the current environment, the tag is injected automatically by `tomei apply`, `tomei plan`, `tomei validate`, and `tomei cue eval/export`. Tags that do not match are **not** injected, so `@if(darwin && arm64)` files are silently excluded on Linux, and `@if(!headless)` files are included when `headless` is absent.

| Tag | Injected when |
|-----|---------------|
| `darwin` | OS is macOS (`runtime.GOOS == "darwin"`) |
| `linux` | OS is not macOS (non-darwin platforms fall back to `linux`) |
| `amd64` | Architecture is not arm64 (non-arm64 platforms fall back to `amd64`) |
| `arm64` | Architecture is arm64 (`runtime.GOARCH == "arm64"`) |
| `headless` | Headless environment detected (container, no display, SSH, CI) |

Other identifiers (e.g., `@if(windows)`) are ignored — the tag is never injected, so the file is always excluded.

#### Syntax

- `@if(darwin && arm64)` — include on macOS only
- `@if(!darwin)` — include on everything except macOS
- `@if(darwin && arm64)` — include on Apple Silicon only
- `@if(linux || darwin)` — include on Linux or macOS

> **Note:** Use `&&` for AND and `||` for OR. Do not use commas — `@if(darwin, arm64)` is not AND.

#### `@tag()` vs `@if()`

| Feature | `@tag()` | `@if()` |
|---------|----------|---------|
| Scope | Field-level value injection | File-level inclusion/exclusion |
| Use case | String interpolation (`\(_os)`) | Separate platform files |
| Propagation | Works in imported packages | Does **not** propagate to imports |

Both can be used together in the same project. Use `@tag()` for URL interpolation and `@if()` for file-level branching.

#### Limitations

- `@if()` must appear before the `package` declaration
- `@if()` does **not** propagate to imported packages — presets continue to use the parameter-passing pattern
- When using `cue eval` directly (not via `tomei cue eval`), boolean tags must be passed manually: `cue eval -t darwin manifests/`. `tomei` commands handle this automatically

### Using presets (recommended)

Presets that need platform information accept explicit `platform` parameters from the user manifest. The user declares `@tag()` variables and passes them to the preset:

```cue
package tomei

import gopreset "tomei.terassyi.net/presets/go"

_os:   string @tag(os)
_arch: string @tag(arch)

goRuntime: gopreset.#GoRuntime & {
    platform: { os: _os, arch: _arch }
    spec: version: "1.26.0"
}
```

### CUE tooling compatibility

Standard CUE tools work with the same tags:

```bash
cue eval -t os=linux -t arch=amd64 manifests/
```

## Schema Import

The schema package is available via the CUE module registry (OCI). User manifests can import it for explicit type validation:

```cue
package tomei

import "tomei.terassyi.net/schema"

myTool: schema.#Tool & {
    metadata: name: "jq"
    spec: {
        installerRef: "aqua"
        version:      "1.7.1"
        package:      "jqlang/jq"
    }
}
```

Available definitions: `schema.#Tool`, `schema.#ToolSet`, `schema.#Runtime`, `schema.#Installer`, `schema.#InstallerRepository`, `schema.#Resource`, etc.

Schema import is optional. When using presets (`tomei.terassyi.net/presets/*`), schema constraints are applied automatically because presets import the schema module. For manifests without presets, adding `schema.#Tool &` or `schema.#Runtime &` to resource definitions enables explicit validation.

## OCI Registry (Module Resolution)

`tomei` resolves CUE imports like `import "tomei.terassyi.net/presets/go"` via a CUE module registry (OCI).

### OCI registry resolution

`tomei` builds a `modconfig.Registry` with a built-in default: `tomei.terassyi.net=ghcr.io/terassyi`. The module `tomei.terassyi.net@v0` is published as an OCI artifact on `ghcr.io/terassyi`. When `CUE_REGISTRY` is not set, this default mapping is used. When `CUE_REGISTRY` is set by the user, it takes precedence.

For directory-mode loading (package-based CUE files), a `cue.mod/` directory is required. For single-file loading without imports, `cue.mod/` is not needed.

### Setting up a CUE module

Use `tomei cue init` to create the module structure, or manually create `cue.mod/module.cue`:

```cue
module: "manifests.local@v0"
language: version: "v0.9.0"
deps: {
    "tomei.terassyi.net@v0": v: "v0.1.10"
}
```

Then use imports with explicit platform parameters:

```cue
package tomei

import gopreset "tomei.terassyi.net/presets/go"

_os:   string @tag(os)
_arch: string @tag(arch)

goRuntime: gopreset.#GoRuntime & {
    platform: { os: _os, arch: _arch }
    spec: version: "1.26.0"
}
```

#### CUE tooling integration

For `cue eval` and LSP to resolve tomei imports, set `CUE_REGISTRY` via `eval $(tomei env)`:

```bash
eval $(tomei env)
cue eval -t os=linux -t arch=amd64 tools.cue
```

#### Platform parameterization

Presets that need platform information (e.g., Go) accept explicit `platform` parameters. The user's manifest declares `@tag()` variables and passes them to the preset. This approach works consistently in both modes — `@tag()` values are resolved at the top-level manifest, and platform information flows via explicit parameters rather than environment injection.

## Brew Preset

The `brew` preset provides Homebrew integration via the delegation pattern. Import it with `import "tomei.terassyi.net/presets/brew"`.

### Architecture

```
#Homebrew (Tool, commands pattern)     ← brew itself, self-managed
    ↓ toolRef: "homebrew"
#BrewInstaller (Installer, delegation) ← provides "brew install" command
    ↓ installerRef: "brew"
#Formula / #FormulaSet (Tool/ToolSet)  ← individual packages
```

### Definitions

| Definition | Kind | Description |
|-----------|------|-------------|
| `#Homebrew` | Tool | Homebrew package manager (commands pattern, self-installing) |
| `#BrewInstaller` | Installer | Delegation installer using `brew install` |
| `#Formula` | Tool | Single Homebrew formula |
| `#FormulaSet` | ToolSet | Set of Homebrew formulae |

The preset targets macOS (darwin/arm64) only. Brew prefix is `/opt/homebrew`. Use `@if(darwin && arm64)` to exclude brew resources on unsupported platforms.

### Usage

Use `@if(darwin && arm64)` to limit brew resources to macOS (recommended):

```cue
@if(darwin && arm64)

package tomei

import "tomei.terassyi.net/presets/brew"

homebrew: brew.#Homebrew

brewInstaller: brew.#BrewInstaller

brewTools: brew.#FormulaSet & {
    metadata: {
        name:        "brew-formulae"
        description: "Common Homebrew formulae"
    }
    spec: tools: {
        tree: {package: "tree"}
        wget: {package: "wget"}
    }
}
```

Single formula:

```cue
jq: brew.#Formula & {
    metadata: name: "jq"
    spec: package: "jq"
}
```

### Notes

- **Version is informational**: `brew install` does not support universal version pinning. The `version` field is recorded in state but not enforced.
- **Cask is out of scope**: Only formulae are supported. Cask support may be added in the future.
- **Remove is no-op for formulae**: Removing a formula from the manifest removes it from state, but `brew uninstall` is not called (same limitation as binstall).

## Validation

`tomei validate <path>` checks manifests without applying. When manifests use presets or explicitly import the schema, CUE-native type constraints are enforced at load time.

Validation checks:

- CUE syntax and evaluation errors
- Schema conformance via imports (field types, required fields, enum values)
- `metadata.name` regex pattern (enforced by schema)
- HTTPS-only URLs (enforced by schema)
- Circular dependency detection in the resource graph

### Common errors

| Error | Cause |
|-------|-------|
| `field not allowed` | Unknown field in spec |
| `conflicting values` | Type mismatch (e.g., string where bool expected) |
| `incomplete value` | Required field missing |
| `circular dependency detected` | Resource dependency cycle |
