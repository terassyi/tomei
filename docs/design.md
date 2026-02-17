# Tomei Design Document

**Version:** v1beta1
**Date:** 2026-02-10

## 1. Overview

`tomei` is a declarative development environment setup tool. It manages local tools, language runtimes, and system packages through Spec/State reconciliation.

### Design Philosophy

`tomei` takes a declarative, idempotent approach: you describe the desired state in CUE manifests and `tomei` reconciles it. There is no sandboxing — tools and runtimes are installed directly into the real environment. Rather than building nix-level complexity, `tomei` delegates to existing tools like `go install`, `cargo install`, and `rustup` wherever possible.

## Installer Patterns

`tomei` supports two installer patterns. Every resource uses one or the other.

### Delegation pattern

Delegates the actual work to an external command.

```
go install <package>@<version>
cargo install <package>
```

`tomei` instructs *what* to install; the external tool handles *how*.

### Download pattern

`tomei` downloads, verifies, extracts, and places files directly.

```
GitHub Releases binary → checksum verify → extract → symlink
go.dev tarball → checksum verify → extract
Aqua registry tool → resolve metadata → download → symlink
```

Choosing between patterns is a per-resource decision. Go runtime uses download (tarball from go.dev). Rust runtime uses delegation (rustup bootstrap). Tools can use either pattern depending on their source.

## 3. Resource Model

Resources are classified by privilege level and role.

```
User privilege (tomei apply):
├── Runtime              Language runtime (Go, Rust, Node.js)
├── Tool                 Individual CLI tool
├── ToolSet              Set of tools with shared configuration
├── Installer            User-level installer definition (aqua, brew, binstall)
└── InstallerRepository  Third-party tool metadata repository

System privilege (sudo tomei apply --system):
├── SystemInstaller          Package manager definition (apt)
├── SystemPackageRepository  Third-party apt repository
└── SystemPackageSet         Set of system packages
```

Each resource has `apiVersion`, `kind`, `metadata`, and `spec`. The full field reference is in [CUE Schema Reference](cue-schema.md).

### Dependency relationships

- runtimeRef: Tool → Runtime (tool installed via runtime's commands)
- installerRef: Tool → Installer (tool installed via installer)
- toolRef: Installer → Tool (installer depends on a tool binary)
- repositoryRef: Tool → InstallerRepository

A Tool specifies either `runtimeRef` or `installerRef`, not both.

### Tool chain example

```
Runtime/rust → Tool/cargo-binstall → Installer/binstall → Tool/ripgrep
```

Each link in the chain is a dependency edge in the DAG.

## 4. Key Design Decisions

### State-based reconciliation

`tomei` persists the current environment state in `state.json`. On each `tomei apply`, it compares the desired state (CUE manifests) with the current state to determine the minimal set of actions (install, upgrade, remove). This makes apply idempotent — running it twice produces no changes the second time.

The state file uses advisory file locking (flock) to prevent concurrent execution, and atomic writes (write to tmp, rename) to prevent corruption.

### DAG-based execution

Resources form a directed acyclic graph based on their dependency relationships. Topological sort produces execution layers — groups of resources with no inter-dependencies. Resources within the same layer are executed in parallel (configurable 1–20 concurrency).

This approach naturally handles complex dependency chains like `Runtime → Tool → Installer → Tool` while maximizing parallelism where possible.

### Taint propagation

When a Runtime is upgraded, all Tools that depend on it (via `runtimeRef`) are marked as tainted. Tainted tools are reinstalled on the next apply. This ensures that tools compiled against a specific runtime version are rebuilt when the runtime changes.

### CUE as configuration language

CUE was chosen over YAML/JSON/TOML for several reasons:

CUE has built-in schema validation and type constraints, so configuration errors are caught at `tomei validate` time rather than at apply time. CUE `@tag()` attributes (`@tag(os)`, `@tag(arch)`, `@tag(headless)`) enable platform-aware manifests without templating. Multiple `.cue` files in the same package are automatically merged, and regex constraints in the schema enforce HTTPS-only URLs.

### Aqua registry integration

Rather than maintaining a separate tool registry, `tomei` integrates with the [aqua registry](https://github.com/aquaproj/aqua-registry). This provides access to thousands of tool definitions (download URLs, binary names, archive formats) without users needing to specify them manually.

## 5. Target Environments

```
OS:    linux, darwin (Windows is out of scope)
Arch:  amd64, arm64
Mode:  headless (server, CI, container, SSH), desktop (GUI)
```

## 6. Directory Structure

```
~/.config/tomei/           # Config (fixed path)
├── config.cue             # Path settings
└── *.cue                  # User manifests

./ (manifest directory)    # Where user runs tomei
├── cue.mod/module.cue     # CUE module declaration (placed by tomei cue init)
├── tomei_platform.cue     # Platform @tag() declarations (placed by tomei cue init)
└── *.cue                  # User manifests

~/.local/share/tomei/      # Data (configurable via config.cue)
├── state.json             # Current state
├── state.lock             # flock file
├── runtimes/<name>/<ver>/ # Installed runtimes
└── tools/<name>/<ver>/    # Installed tools

~/.local/bin/              # Symlinks (configurable via config.cue)

~/.cache/tomei/            # Cache
├── registry/aqua/         # Aqua registry (shallow git clone)
└── logs/                  # Installation logs (per session)
```

## 7. Security

- Checksum verification (SHA256) for all downloaded binaries
- HTTPS-only URLs enforced by CUE schema
- No shell injection — `exec.Command` with explicit arguments
- Atomic state writes (tmp + rename)

## 8. Schema Versioning

The CUE schema is published as part of the `tomei.terassyi.net@v0` module on the OCI registry (`ghcr.io/terassyi`). User manifests can `import "tomei.terassyi.net/schema"` for explicit type validation and editor completion via CUE LSP.

Presets (`tomei.terassyi.net/presets/{go,rust,aqua}`) import the schema module, so type constraints are enforced automatically when using presets. For manifests without preset imports, users can add `import "tomei.terassyi.net/schema"` and use `schema.#Tool &`, `schema.#Runtime &`, etc. to opt in to schema validation.

The schema is versioned via `#APIVersion` (currently `"tomei.terassyi.net/v1beta1"`).

**Versioning policy:**

- v1beta1 is frozen at the v0.1.0 release
- Schema changes require a new apiVersion (e.g., v1beta2, v1)
- Module version is independent of the tomei binary version (see [Module Publishing](module-publishing.md))

## 9. Implementation Status

Completed:

- Foundation: resource types, state management, CUE loader, DAG, CLI skeleton
- Tool installation: download pattern, aqua registry, checksum verification, symlinks
- Runtime management: Go, Rust, Node.js (download + delegation patterns)
- Runtime delegation: go install, cargo install, npm install -g
- Taint logic: runtime upgrade triggers tool reinstall
- Parallel execution: DAG-based engine with configurable concurrency, progress UI
- ToolSet expansion
- E2E test infrastructure (container-based, Ginkgo v2)
- Shell environment: `tomei env` for runtime PATH/env setup
- Runtime delegation: rustup/nvm bootstrap, version alias resolution
- InstallerRepository, CUE presets/overlay, GitHub token authentication
- Diagnostics: `tomei get`, `tomei logs`, `tomei state diff`, `tomei completion`, `tomei doctor`
- Performance: batch state writes per execution layer (StateCache)
- Schema management: init guard, apply confirmation prompt (`--yes`)
- CUE module ecosystem: `tomei cue init`, OCI registry resolution, `CUE_REGISTRY` in `tomei env`
- Schema import: presets import schema for single source of truth, `@tag()` for platform injection

## 10. Roadmap

### System privilege (deferred)

System-level package management via `sudo tomei apply --system`:

- **SystemInstaller**: Package manager definitions (apt as builtin)
- **SystemPackageRepository**: Third-party APT repositories with GPG key management
- **SystemPackageSet**: Sets of system packages

The CUE schema and resource types are already defined. Implementation requires privilege escalation handling and APT-specific installer logic.

### Private repository access

Authenticated downloads from private GitHub repositories. Public repository rate limiting is already addressed via `GITHUB_TOKEN` / `GH_TOKEN`.

## 11. Design Considerations

### Authentication & tokens

For private repository access and authenticated registry support, two approaches are under consideration:

**Option A: Include in Installer**

```cue
kind: "Installer"
metadata: name: "aqua"
spec: {
    type: "download"
    auth: {
        tokenEnvVar: "GITHUB_TOKEN"
    }
}
```

**Option B: Separate Credential resource**

```cue
kind: "Credential"
metadata: name: "github"
spec: {
    type:   "token"
    envVar: "GITHUB_TOKEN"
}

kind: "Installer"
metadata: name: "aqua"
spec: {
    type:          "download"
    credentialRef: "github"
}
```

Trade-offs: Option A is simpler. Option B is more flexible when multiple installers share the same authentication.

### CUE evaluation vs Go template

Command strings in manifests currently use Go `text/template` variables (`{{.Version}}`, `{{.Package}}`, etc.) for values that are resolved at execution time. Meanwhile, CUE's own features (`@tag()` injection, field references) handle values known at configuration load time.

This creates two variable substitution mechanisms in the same manifest. The boundary needs to be clarified:

- **CUE side**: values known at load time (OS, architecture, environment conditions)
- **Go template side**: values known at execution time (resolved version, package name, binary path)

Whether to unify these or keep the current split is an open question.

## Related Documents

- [Architecture](architecture.md) — implementation details for contributors
- [CUE Schema Reference](cue-schema.md) — full field reference for writing manifests
- [CUE Ecosystem Integration](cue-ecosystem.md) — OCI registry, `tomei cue init`, CUE tooling
- [Module Publishing](module-publishing.md) — versioning strategy, publish workflow
- [Releasing](releasing.md) — binary release process
- [Usage](usage.md) — command reference
