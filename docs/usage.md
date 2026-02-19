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
| `--quiet` | Suppress progress output |
| `--no-color` | Disable colored output |

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

### Version Resolvers

Runtime presets can declare a `resolveVersion` field that automatically resolves the actual version at install time. Two built-in resolver syntaxes are available, plus a shell command fallback.

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
| `--system` | Apply system-level resources (requires root). Used with `apply` and `plan`. |

## Environment Variables

| Variable | Description |
|----------|-------------|
| `GITHUB_TOKEN` | GitHub personal access token for API rate limit mitigation |
| `GH_TOKEN` | Alternative to `GITHUB_TOKEN` (used by gh CLI) |

tomei checks `GITHUB_TOKEN` first, then falls back to `GH_TOKEN`. The token is used for GitHub API requests when downloading tools and resolving aqua registry packages.
