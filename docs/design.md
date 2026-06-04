# Tomei Design Document

**Version:** v1beta1
**Date:** 2026-02-10

## 1. Overview

`tomei` is a declarative development environment setup tool. It manages local tools, language runtimes, and system packages through Spec/State reconciliation.

### Design Philosophy

`tomei` takes a declarative, idempotent approach: you describe the desired state in CUE manifests and `tomei` reconciles it. There is no sandboxing — tools and runtimes are installed directly into the real environment. Rather than building nix-level complexity, `tomei` delegates to existing tools like `go install`, `cargo install`, and `rustup` wherever possible.

## Installer Patterns

`tomei` supports three installer patterns. Every resource uses one of them.

### Delegation pattern

Delegates the actual work to an external command.

```
go install <package>@<version>
go install <package>@<sha>        # via spec.sha (Go only)
cargo install <package>
```

`tomei` instructs *what* to install; the external tool handles *how*.

For `runtimeRef: "go"` tools, `spec.sha` pins the install to a specific
git commit SHA instead of a tag. The SHA flows through `GOPROXY` and is
verified against the `GOSUMDB` transparency log — this is why SHA pinning
is currently limited to Go; other ecosystems lack equivalent end-to-end
integrity for arbitrary commits. See [usage.md](usage.md) for the user-
facing guide and [cue-schema.md](cue-schema.md) for the field definition.

### Download pattern

`tomei` downloads, verifies, extracts, and places files directly.

```
GitHub Releases binary → checksum verify → extract → symlink
go.dev tarball → checksum verify → extract
Aqua registry tool → resolve metadata → download → symlink
```

### Commands pattern

The tool manages its own installation, update, and removal via shell commands
defined directly on the Tool spec. No runtime or installer dependency is needed.

```
curl -fsSL https://cli.claude.ai/install.sh | sh    # install
claude update                                         # update
claude uninstall                                      # remove
```

Used for tools that provide their own installer scripts and self-update mechanisms.
Commands-pattern tools bypass tomei's checksum verification and HTTPS-only validation
for downloads — security of the installation is the user's responsibility.

Choosing between patterns is a per-resource decision. Go runtime uses download (tarball from go.dev). Rust runtime uses delegation (rustup bootstrap). Tools can use either pattern depending on their source. Self-managed tools (e.g., Claude CLI) use the commands pattern.

## 3. Resource Model

Resources are classified by privilege level and role.

```
User privilege (tomei apply):
├── Runtime              Language runtime (Go, Rust, Node.js)
├── Tool                 Individual CLI tool
├── ToolSet              Set of tools with shared configuration
├── Installer            User-level installer definition (aqua, brew, binstall)
└── InstallerRepository  Third-party tool metadata repository

System privilege (tomei apply --system):
├── SystemInstaller          Package manager definition (apt)
├── SystemPackageRepository  Third-party apt repository
└── SystemPackageSet         Set of system packages
```

`tomei` itself runs as the invoking user. Manifests must use `sudo` explicitly for steps that require elevation; tomei does not wrap commands. With `--system`, tomei pre-acquires the sudo timestamp (`sudo -v`) and keeps it refreshed so those explicit `sudo` calls typically don't re-prompt. Do not run `sudo tomei apply` — running the whole tool as root may operate on root's home directory or create root-owned files under the configured Tomei data directory (by default `~/.local/share/tomei/`), causing permission issues for subsequent unprivileged invocations.

Each resource has `apiVersion`, `kind`, `metadata`, and `spec`. A Tool specifies exactly one of `runtimeRef`, `installerRef`, or `commands`. The full field reference is in [CUE Schema Reference](cue-schema.md).

### Dependency relationships

- runtimeRef: Tool → Runtime (tool installed via runtime's commands)
- installerRef: Tool → Installer (tool installed via installer)
- commands: Tool (self-managed, no dependencies — first execution layer)
- toolRef: Installer → Tool (installer depends on a tool binary, PATH injection)
- dependsOn: Installer → Tool (additional DAG ordering dependencies, no PATH injection)
- repositoryRef: Tool → InstallerRepository

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
├── state.json             # Current state (user resources)
├── state.lock             # flock file
├── runtimes/<name>/<ver>/ # Installed runtimes
├── tools/<name>/<ver>/    # Installed tools
└── system/
    ├── state.json         # Current state (system resources)
    └── state.lock         # flock file

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
- Taint UI visibility: phase-aware engine events, taint reinstall and removal in TUI and plan
- Commands pattern: self-managed tools with install/update/check/remove/resolveVersion
- Cosign signature verification: pre-flight OCI artifact verification via sigstore
- Self-update: `tomei upgrade` command (GitHub Releases download, checksum verify, binary replace)
- CUE `@if()` boolean platform tags: `@if(darwin)`, `@if(arm64)`, `@if(headless)` for file-level branching
- Disabled resource filtering: `enabled: false` resources excluded from ExpandSets and shown as "skip" in `tomei plan`
- Aqua template variable `AssetWithoutExt` for `files[].src` path references
- System package management: SystemInstaller validation (APT) via per-user state, sudo delegation, distro detection (Debian/Ubuntu family)
- Privileged download/registry tools route through SystemBinDir

## 10. System Package Management

### Execution model

`tomei` runs as the invoking user. Manifests written for `--system` mode must use `sudo` explicitly on commands that need elevation; tomei does not wrap commands itself. With `--system`, tomei pre-acquires the sudo timestamp (`sudo -v`) and keeps it refreshed in the background, so subsequent explicit `sudo …` calls in manifests typically do not re-prompt for a password.

```
Right:  tomei apply --system     # tomei runs as user; manifests use sudo where needed
Wrong:  sudo tomei apply --system # may create root-owned files under the Tomei data directory
```

### Per-user state

System resource state lives under `<dataDir>/system/state.json` (by default, `~/.local/share/tomei/system/state.json`). Each user maintains an independent view of system package state.

This is a deliberate trade-off for the tool's target use case (single-developer dev environment setup). It is **not** suitable for coordinated multi-user system administration:

- **No cross-user coordination.** State files are independent. Once `SystemPackageSet` reconciliation is implemented (see Roadmap below), dropping a package from user A's manifest would cause tomei to run a system-wide removal (e.g., `apt-get remove vim`) — affecting user B who also relied on the package. Idempotent installs (`apt-get install`) are safe to overlap; removals are not.
- **No drift detection.** Reconciliation compares the declared spec against tomei's own state file. Out-of-band changes (manual `apt install`, distro upgrades, packages installed by other tools) are invisible.

For shared servers, use a configuration management tool (Ansible, Chef, Puppet) instead.

### Dependency graph

Following the convention from §3, arrows point from a resource to the dependents it enables:

```
SystemInstaller → SystemPackageRepository → SystemPackageSet
                ↘ SystemPackageSet
```

- `SystemPackageRepository.spec.installerRef` and `SystemPackageSet.spec.installerRef` reference a `SystemInstaller`
- `SystemPackageSet.spec.repositoryRef` (optional) references a `SystemPackageRepository`

### Implementation status

| Resource | Status |
|----------|--------|
| `SystemInstaller` (`apt`) | Validates that `apt-get` exists on the host and captures its version |
| `SystemInstaller` (other) | Schema accepts `dnf`/`zypper`/`pacman`/`apk`, but only `apt` is currently wired. Other package managers fail validation either with "package manager is not supported on this system" (distro mismatch) or "no version function registered" (supported by distro detection but not yet wired in the engine) |
| `SystemPackageRepository` | Concrete installer wired (APT). Places the GPG keyring under `/usr/share/keyrings/<name>.gpg`, writes the source entry to `/etc/apt/sources.list.d/<name>.list`, and runs `apt-get update`. On hosts where distro detection fails or no supported package manager is present, install fails with `system: repository "<name>": requires a supported Linux package manager (apt) on this host`; Remove returns successfully with a warn log so stale state can be drained without touching the originating host's files. |
| `SystemPackageSet` | Concrete installer wired (APT). Runs `apt-get install` / `apt-get remove` and probes installed versions via `dpkg-query` after install. On hosts where distro detection fails or no supported package manager is present, install fails with `system: package "<name>": requires a supported Linux package manager (apt) on this host`; Remove returns successfully with a warn log so stale state can be drained without touching the originating host's packages. |

`tomei plan --system` and `tomei apply --system` recognize all three kinds and execute through their respective concrete installers.

`SystemInstaller` removal does not uninstall the OS package manager — it only clears the state entry.

## 11. Privileged Tools

Tools may set `spec.privileged: true` to opt into elevation semantics. The behavior depends on the install pattern; the new placement behavior introduced in this phase is documented below.

Without `--system`, `tomei plan` keeps privileged tools in its output and marks them as `skip`. `tomei apply` filters them out of execution and logs a summary noting that privileged tools require a sudo cache for shell commands or for placing symlinks in the system bin directory.

### Pattern semantics

- **Download / Registry patterns.** The symlink is placed in the system bin directory (default `/usr/local/bin`) instead of `~/.local/bin`. The downloaded binary itself still lives under the user-owned tools directory (`~/.local/share/tomei/tools/<name>/<version>/`). Placement uses the portable symlink helper — `os.Symlink` first, fallback to `sudo -n ln -snf --` on a permission error (anything matching Go's `fs.ErrPermission`, which covers both `EPERM` and `EACCES`). This works on Linux and on Intel macOS (where `/usr/local/bin` is typically user-writable under Homebrew, so the sudo fallback is rarely needed). Apple Silicon macOS uses `/opt/homebrew` by default; users who want privileged tools there should override `SystemBinDir`.
- **Commands pattern.** Tomei pre-acquires the sudo timestamp and keeps it refreshed in the background for the duration of the apply invocation. The user's install/check/remove commands execute as the invoking user; they may invoke `sudo` internally to perform root-requiring steps. Earlier versions wrapped the entire command in `sudo -n sh -c …`; manifests must now prefix individual root-requiring steps with `sudo` (e.g., `cp …` → `sudo cp …`).
- **Installer-/name-delegation.** `privileged` is ignored. The installer (e.g., `brew`, `apt`) or runtime (e.g., `go install`) owns the destination directory and decides its own escalation policy.
- **Runtime delegation (`runtimeRef` set).** Rejected by Validate with a clear error — privileged + runtime delegation has no coherent semantics.

### System bin directory

This directory is exposed in code as `SystemBinDir`. It defaults to `/usr/local/bin` and is overridable via `path.WithSystemBinDir(...)` on the runtime `Paths` value (used for testing and for hosts with non-standard layouts).

The chosen bin directory is persisted as `ToolState.BinDirKind` (`"user"` or `"system"`) so subsequent applies and removals clean up the correct location even after the spec is deleted.

### sudoers requirement

`tomei apply --system` runs `sudo -n true` to probe a cached or passwordless ticket, then `sudo -v` (interactive) as fallback if a TTY is attached. CI and unattended environments require passwordless sudo — typically:

```
<user> ALL=(ALL) NOPASSWD: ALL
```

in `/etc/sudoers.d/<user>`. This is the same requirement `--system` package management imposes (see §10's "Execution model").

### 1-host-1-user constraint

`/usr/local/bin` is host-global; tomei state is per-user. Two tomei-using users on the same host who both apply privileged download/registry tools would race on the same symlink path — whichever applied last wins, and the loser's state.json continues to claim ownership of a target now owned by a different user's spec.

This is a deliberate trade-off for the tool's target use case (single-developer dev environment setup). Tomei does not coordinate between users on the same host. Multi-user environments should restrict privileged tool usage to a single dedicated user.

## 12. Roadmap

### Private repository access

Authenticated downloads from private GitHub repositories. Public repository rate limiting is already addressed via `GITHUB_TOKEN` / `GH_TOKEN`.

## 13. Design Considerations

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
