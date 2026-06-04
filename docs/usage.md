# Usage

Command reference for `tomei`.

## tomei init

Initialize `tomei` directories, state file, and aqua registry.

```
tomei init [flags]
```

| Flag | Description |
|------|-------------|
| `--yes`, `-y` | Skip confirmation prompt and create config.cue with defaults |
| `--force` | Force reinitialization (resets state.json) |
| `--no-color` | Disable colored output |

Creates the following:

```
~/.config/tomei/           # Config directory
└── config.cue             # Path settings
~/.local/share/tomei/      # Data directory
├── state.json             # State file
├── tools/                 # Tool install directory
└── runtimes/              # Runtime install directory
~/.local/bin/              # Symlink directory
```

## tomei cue init

Initialize a CUE module directory for use with tomei manifests.

```
tomei cue init [dir] [flags]
```

| Flag | Description |
|------|-------------|
| `--module-name` | CUE module name (default: `manifests.local@v0`) |
| `--force` | Overwrite existing files |

Creates the following:

```
<dir>/
├── cue.mod/
│   └── module.cue         # CUE module declaration with tomei dependency
└── tomei_platform.cue     # Platform @tag() declarations
```

After initialization, set `CUE_REGISTRY` for CUE tooling:

```bash
eval $(tomei env)
```

See [CUE Ecosystem Integration](cue-ecosystem.md) for details.

## tomei cue update

Update tomei module dependencies in `cue.mod/module.cue` to the latest published version.

```
tomei cue update [dir] [flags]
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Show updates without writing changes |

Scans the `deps` block for first-party `tomei.terassyi.net` dependencies and updates their version to the latest available from the OCI registry.

```bash
# Update in current directory
tomei cue update

# Preview changes without writing
tomei cue update --dry-run

# Update in specified directory
tomei cue update ./manifests
```

## tomei cue scaffold

Generate a CUE manifest scaffold for a resource kind.

```
tomei cue scaffold <kind> [flags]
```

| Flag | Description |
|------|-------------|
| `--bare` | Output without schema import (for use without `cue.mod/`) |

Supported kinds: `tool`, `runtime`, `installer`, `installer-repository`, `toolset`

By default, the output includes `import "tomei.terassyi.net/schema"` and type constraints (e.g., `schema.#Tool &`). Use `--bare` for plain CUE without schema imports.

```bash
# Generate a Tool scaffold with schema import
tomei cue scaffold tool

# Generate without schema import
tomei cue scaffold runtime --bare

# Redirect to file
tomei cue scaffold tool > tools.cue
```

## tomei cue eval

Evaluate CUE manifests with tomei configuration applied.

```
tomei cue eval <files or directories...>
```

Unlike plain `cue eval`, this command automatically:
- Configures the OCI registry for tomei module resolution
- Injects `@tag()` values (`os`, `arch`, `headless`) from the current platform
- Excludes `config.cue` from evaluation

Output is CUE text format.

```bash
# Evaluate a directory
tomei cue eval ./manifests/

# Evaluate a specific file
tomei cue eval tools.cue
```

## tomei cue export

Export CUE manifests as JSON with tomei configuration applied.

```
tomei cue export <files or directories...>
```

Same as `tomei cue eval` but outputs indented JSON instead of CUE text.

```bash
# Export as JSON
tomei cue export ./manifests/

# Pipe to jq
tomei cue export tools.cue | jq '.myTool'
```

## tomei validate

Validate CUE manifests and detect circular dependencies.

```
tomei validate <files or directories...> [flags]
```

| Flag | Description |
|------|-------------|
| `--no-color` | Disable colored output |
| `--ignore-cosign` | Skip cosign signature verification for CUE module dependencies (global flag) |

Checks:
- CUE syntax errors
- Schema conformance (field types, required fields)
- Circular dependency detection in the resource graph

## tomei plan

Show the dependency graph and execution plan without applying changes.

```
tomei plan <files or directories...> [flags]
```

| Flag | Description |
|------|-------------|
| `--sync` | Sync aqua registry to latest version before planning |
| `--update-tools` | Show plan as if updating tools with non-exact versions (latest + alias) |
| `--update-runtimes` | Show plan as if updating runtimes with non-exact versions (latest + alias) |
| `--update-all` | Show plan as if updating all tools and runtimes with non-exact versions |
| `--output`, `-o` | Output format: `text` (default), `json`, `yaml` |
| `--no-color` | Disable colored output |
| `--ignore-cosign` | Skip cosign signature verification for CUE module dependencies (global flag) |

Displays:
- Dependency tree
- Execution layers (parallel groups)
- Actions per resource (install, upgrade, reinstall, remove, none)
- Summary (counts by action type)

## tomei apply

Install, upgrade, or remove resources to match the manifests.

```
tomei apply <files or directories...> [flags]
```

| Flag | Description |
|------|-------------|
| `--yes`, `-y` | Skip confirmation prompt |
| `--sync` | Sync aqua registry to latest version before applying |
| `--update-tools` | Update tools with non-exact versions (latest + alias) to latest |
| `--update-runtimes` | Update runtimes with non-exact versions (latest + alias) to latest. Delegation runtimes with `bootstrap.update` use the lightweight update command instead of re-running the full bootstrap installer |
| `--update-all` | Update all tools and runtimes with non-exact versions. Same lightweight update behavior as `--update-runtimes` for delegation runtimes |
| `--parallel <n>` | Max parallel installations, 1–20 (default 5) |
| `--timeout` | Per-download timeout (e.g., `5m`, `10m`, `1h`; default `5m`) |
| `--quiet` | Suppress progress output |
| `--no-color` | Disable colored output |
| `--ignore-cosign` | Skip cosign signature verification for CUE module dependencies (global flag) |

Before applying, `tomei apply` shows the execution plan and asks for confirmation (`y/N`). Use `--yes` to skip the prompt. If the current state already matches the manifests, no changes are made.

`tomei apply` requires `tomei init` to have been run first.

```bash
# Apply all manifests in the current directory
tomei apply .

# Apply specific files
tomei apply tools.cue runtime.cue

# Sync aqua registry and apply
tomei apply --sync .

# Update all non-exact tools (latest + alias versions)
tomei apply --update-tools .

# Update runtimes with alias versions (e.g., Rust "stable")
tomei apply --update-runtimes .

# Update both tools and runtimes
tomei apply --update-all .

# Control parallelism
tomei apply --parallel 4 .
```

### Pinning a Tool to a git commit SHA

For tools installed via `runtimeRef: "go"`, set `spec.sha` to a 40-character
lowercase hex commit SHA to pin the install to an exact commit instead of
a tag. `go install pkg@<sha>` resolves the SHA through `GOPROXY` and
verifies the module zip against `GOSUMDB`, so the install is reproducible
and integrity-checked.

```cue
gopls: {
    apiVersion: "tomei.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "gopls"
    spec: {
        runtimeRef: "go"
        package:    "golang.org/x/tools/gopls"
        sha:        "0123456789abcdef0123456789abcdef01234567"
    }
}
```

Constraints:

- `sha` and `version` are mutually exclusive.
- `sha` requires `runtimeRef: "go"`; other installers reject `sha` at
  validate time.
- Short SHAs and tag-like strings (e.g. `v1.2.3`) are rejected — only
  40-char lowercase hex matches.
- SHA-pinned tools are NOT tainted by `--sync` / `--update-tools` (a SHA
  is the strictest form of exact pin).
- `tomei plan` displays the SHA truncated to 12 characters; state retains
  the full SHA.

### Self-Managed Tools (Commands Pattern)

Tools with `spec.commands` manage their own installation via shell commands, without needing a runtime or installer dependency.

On `tomei apply`, the engine runs `commands.install` followed by `commands.check` to verify success. `resolveVersion` captures the installed version for `tomei get`.

On `--update-tools`, the engine uses `commands.update` if defined, falling back to `commands.install`.
On removal (manifest deleted), the engine runs `commands.remove`.

Commands-pattern tools run in the first execution layer alongside download-pattern tools.

See [CUE Schema Reference — ToolCommandSet](cue-schema.md#toolcommandset) for field details.

### System Package Management (`--system`)

`--system` enables system-level package management and privileged tool operations. tomei itself still runs as the invoking user; manifests must use `sudo` explicitly on commands that need elevation. With `--system`, tomei pre-acquires the sudo timestamp (`sudo -v`) and keeps it refreshed so those explicit `sudo …` calls typically do not re-prompt for a password.

```bash
# Show what would change for system resources
tomei plan --system .

# Apply (user is prompted for sudo password once)
tomei apply --system .
```

System resources:

| Kind | Purpose |
|------|---------|
| `SystemInstaller` | Declares a host package manager (currently `apt` only). Validates the package manager is available and captures its version. |
| `SystemPackageRepository` | Third-party APT repository (e.g., Docker, Kubernetes). Concrete installer (APT-based) places the GPG keyring at `/usr/share/keyrings/<name>.gpg`, writes the source entry to `/etc/apt/sources.list.d/<name>.list`, and runs `apt-get update`. On non-apt hosts the resource fails with a clear "platform unsupported" error. |
| `SystemPackage` | Single-package shorthand for `SystemPackageSet`. Expands to a one-element set at load time; same execution path. |
| `SystemPackageSet` | Set of system packages to install. Concrete installer (APT-based) runs `apt-get install` / `apt-get remove` and probes installed versions via `dpkg-query`. On non-apt hosts the resource fails with a clear "platform unsupported" error. |

Example manifest (no preset ships for `apt` — declare it inline):

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

For a single package, use `SystemPackage` (shorthand that expands to a one-element `SystemPackageSet`):

```cue
apiVersion: "tomei.terassyi.net/v1beta1"
kind:       "SystemPackage"
metadata: name: "git"
spec: {
    installerRef: "apt"
    package:      "git"
}
```

Both `SystemPackage` and `SystemPackageSet` require `--system` at apply time. See [CUE Schema → SystemPackage](./cue-schema.md#systempackage) for field details.

Without `--system`, system resources and privileged tools are skipped at apply time:

```text
$ tomei apply .
2 system resource(s) skipped. Use 'tomei apply --system' to manage.
1 privileged resource(s) skipped. Use 'tomei apply --system' to install.
```

`tomei plan .` (without `--system`) shows the same resources marked `skip` in the graph and summary, without the count lines above.

Behavior notes:

- **State location.** System state is stored at `~/.local/share/tomei/system/state.json`, per user. The system state directory is auto-created on first apply, but `tomei apply` still requires `tomei init` to have been run first because the user state file (`~/.local/share/tomei/state.json`) is checked unconditionally before any apply.
- **Multi-user limitation.** Each user maintains an independent view of system package state. tomei does not coordinate between users on the same host and does not detect out-of-band changes (e.g., manual `apt install` or distro upgrades). For shared servers, use a configuration management tool instead.
- **Sudo behavior.** tomei prompts for the sudo password once per run and reuses the cached credential. With passwordless sudo (e.g., `NOPASSWD` in `/etc/sudoers`, common in CI), no prompt is shown. Do not run `sudo tomei apply --system` — it may create or write state files as root, leaving permission issues later or operating on root's home directory instead of the invoking user's.
- **Removing a `SystemInstaller`.** Drops only the state entry — the underlying OS package manager is not uninstalled.
- **Unsupported platforms.** On non-Linux hosts (e.g., macOS) or distros without a registered package manager, distro detection falls back gracefully: removals (state cleanup) still work, but `Install` actions fail with errors of the form `system: installer: package manager validation unavailable (distro detection failed or unsupported platform)` for `SystemInstaller` and `system: repository "<name>": requires a supported Linux package manager (apt) on this host` for `SystemPackageRepository`.

### Version Resolvers

Runtime presets and commands-pattern tools can declare a `resolveVersion` field that automatically resolves the actual version at install time. Two built-in resolver syntaxes are available, plus a shell command fallback.

#### `github-release:owner/repo[:tagPrefix]`

Fetches the latest release tag from a GitHub repository via the Releases API.
The optional `tagPrefix` is stripped from the tag name.

```
resolveVersion: ["github-release:oven-sh/bun:bun-v"]
```

This calls `GET /repos/oven-sh/bun/releases/latest`, gets `tag_name: "bun-v1.2.3"`, strips `"bun-v"`, and returns `"1.2.3"`.

Uses `GITHUB_TOKEN` / `GH_TOKEN` if available for rate limit mitigation.

#### `http-text:URL:regex`

Fetches a plain-text URL via HTTP GET and applies a regex to extract the version.

```
resolveVersion: ["http-text:https://go.dev/VERSION?m=text:^go(.+)"]
resolveVersion: ["http-text:https://dl.deno.land/release-latest.txt:^v(.+)"]
```

The URL and regex are separated by the **last** `:` after the `://` scheme separator.
The first capture group of the regex is returned as the version string.
If the regex has no capture group, the full match is returned.

> **Limitation:** The regex portion must not contain literal `:` characters, as the last `:` is used as the delimiter.

#### Shell command fallback

If `resolveVersion` does not match a built-in syntax, it is executed as a shell command. The command should print the resolved version to stdout.

```
resolveVersion: ["curl -sL https://example.com/version | head -1"]
```

#### Exact version skip

When `spec.version` is set to an exact version (e.g., `"1.26.0"`), the `resolveVersion` step is skipped entirely. This allows a single preset to handle both pinned and latest versions:

```cue
// Pinned — resolveVersion is skipped
goRuntime: gopreset.#GoRuntime & {
    platform: {os: _os, arch: _arch}
    spec: version: "1.26.0"
}

// Latest — resolveVersion runs automatically
goRuntime: gopreset.#GoRuntime & {
    platform: {os: _os, arch: _arch}
}
```

## tomei get

Display installed resources from the current state.

```
tomei get <type> [name] [flags]
```

| Flag | Description |
|------|-------------|
| `--output`, `-o` | Output format: `table` (default), `wide`, `json` |

Resource types and aliases:

| Type | Aliases |
|------|---------|
| `tools` | `tool` |
| `runtimes` | `runtime`, `rt` |
| `installers` | `installer`, `inst` |
| `installerrepositories` | `installerrepository`, `instrepo` |

```bash
# List all tools
tomei get tools

# Get a specific tool
tomei get tools ripgrep

# Wide output with more columns
tomei get runtimes -o wide

# JSON output
tomei get tools -o json
```

## tomei env

Output environment variables defined by installed runtimes for shell integration.

```
tomei env [flags]
```

| Flag | Description |
|------|-------------|
| `--shell` | Shell type: `posix` (default, for bash/zsh), `fish` |
| `--export` | Write to file (`~/.config/tomei/env.sh` or `env.fish`) instead of stdout |

Add to your shell profile:

```bash
# bash / zsh
eval "$(tomei env)"

# fish
tomei env --shell fish | source
```

Outputs `export` statements for runtime environment variables (e.g., `GOROOT`, `GOBIN`, `CARGO_HOME`) and prepends runtime bin directories to `PATH`.

## tomei doctor

Diagnose the environment for unmanaged tools and conflicts.

```
tomei doctor [flags]
```

| Flag | Description |
|------|-------------|
| `--no-color` | Disable colored output |

Detects:
- Unmanaged tools in runtime bin directories (`~/go/bin/`, `~/.cargo/bin/`)
- Conflicts between `tomei`-managed and unmanaged tools
- State file integrity issues

Provides suggestions for adding unmanaged tools to manifests.

## tomei logs

Inspect installation logs from the last apply.

```
tomei logs [kind/name] [flags]
```

| Flag | Description |
|------|-------------|
| `--list` | List all log sessions |
| `--no-color` | Disable colored output |

```bash
# Show failed resources from the most recent session
tomei logs

# Show log for a specific resource
tomei logs tool/ripgrep

# List all sessions
tomei logs --list
```

## tomei state diff

Compare the current state with the backup taken before the last apply.

```
tomei state diff [flags]
```

| Flag | Description |
|------|-------------|
| `--output`, `-o` | Output format: `text` (default), `json` |
| `--no-color` | Disable colored output |

Shows additions, modifications, and removals grouped by resource kind.

## tomei uninit

Remove `tomei` directories and state. Symlinks in the bin directory pointing to `tomei`-managed tools are removed; the bin directory itself is preserved.

```
tomei uninit [flags]
```

| Flag | Description |
|------|-------------|
| `--yes`, `-y` | Skip confirmation prompt |
| `--keep-config` | Preserve the config directory |
| `--dry-run` | Show what would be removed without actually removing |
| `--no-color` | Disable colored output |

## tomei completion

Generate shell completion scripts.

```
tomei completion <shell>
```

Supported shells: `bash`, `zsh`, `fish`, `powershell`.

```bash
# bash
source <(tomei completion bash)

# zsh
tomei completion zsh > "${fpath[1]}/_tomei"

# fish
tomei completion fish | source

# powershell
tomei completion powershell | Out-String | Invoke-Expression
```

## tomei upgrade

Upgrade tomei itself to the latest version (or a specific version) from GitHub Releases.

```
tomei upgrade [flags]
```

| Flag | Description |
|------|-------------|
| `--dry-run` | Check for updates without installing |
| `--force` | Allow upgrade from development builds (not needed with `--version`) |
| `--version` | Install a specific version (e.g., `0.1.3`) |

```bash
# Check for available updates
tomei upgrade --dry-run

# Upgrade to the latest release
tomei upgrade

# Install a specific version
tomei upgrade --version 0.1.3

# Upgrade from a dev build
tomei upgrade --force
```

The upgrade process downloads the new binary from GitHub Releases, verifies its SHA-256 checksum, replaces the current binary, and verifies the installation.

Supported platforms: `linux/amd64`, `linux/arm64`, `darwin/arm64`. Running on an unsupported platform will produce an error.

Uses `GITHUB_TOKEN` / `GH_TOKEN` if available for API rate limit mitigation.

## tomei version

Print version information.

```
tomei version [flags]
```

| Flag | Description |
|------|-------------|
| `--output`, `-o` | Output format: `text` (default), `json` |

## Global Flags

| Flag | Description |
|------|-------------|
| `--system` | Enable system package management and privileged tool operations. Used with `apply` and `plan`. tomei itself runs as the invoking user; manifests use `sudo` explicitly for elevation, and tomei keeps the sudo timestamp refreshed so re-prompts are rare. Do not run `sudo tomei`. System state is stored per-user under `<dataDir>/system/` (by default `~/.local/share/tomei/system/`); in multi-user environments each user maintains an independent view, and out-of-band system changes are not detected. |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | GitHub personal access token for API rate limit mitigation |
| `GH_TOKEN` | Alternative to `GITHUB_TOKEN` (used by gh CLI) |

tomei checks `GITHUB_TOKEN` first, then falls back to `GH_TOKEN`. The token is used for GitHub API requests when downloading tools and resolving aqua registry packages.
