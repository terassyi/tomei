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
| `--schema-dir <dir>` | Directory to place schema.cue for CUE LSP support |
| `--no-color` | Disable colored output |

Creates the following:

```
~/.config/tomei/           # Config directory
├── config.cue             # Path settings
└── schema.cue             # CUE schema for LSP support
~/.local/share/tomei/      # Data directory
├── state.json             # State file
├── tools/                 # Tool install directory
└── runtimes/              # Runtime install directory
~/.local/bin/              # Symlink directory
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
| `--sync` | Sync aqua registry to latest version before applying |
| `--parallel <n>` | Max parallel installations, 1–20 (default 5) |
| `--quiet` | Suppress progress output |
| `--no-color` | Disable colored output |

Running `tomei apply` is idempotent. If the current state already matches the manifests, no changes are made.

```bash
# Apply all manifests in the current directory
tomei apply .

# Apply specific files
tomei apply tools.cue runtime.cue

# Sync aqua registry and apply
tomei apply --sync .

# Control parallelism
tomei apply --parallel 4 .
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
