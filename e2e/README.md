# E2E Tests

End-to-end tests for tomei using Ginkgo BDD framework.
Tests run in Docker containers (default) or natively on supported platforms.

## Requirements

- Docker (container mode)
- Go 1.25+
- Ginkgo v2 (`go install github.com/onsi/ginkgo/v2/ginkgo@latest`)

## Running Tests

```bash
# From project root (recommended)
make test-e2e
```

## Test Structure (BDD)

All tests run within a single `Ordered` Describe block to guarantee execution order:

```
tomei E2E
├── Basic                 # init, validate, plan, apply, runtime/tool install, upgrade, doctor
├── ToolSet               # ToolSet expansion and installation
├── Aqua Registry         # Registry-based tool install, version upgrade/downgrade
├── Dependency Resolution # Circular detection, parallel install, runtime chain, toolRef
├── Delegation            # Runtime delegation serialization
├── Installer Repository  # InstallerRepository management
├── Logs                  # Log inspection commands
├── Get                   # Resource listing (table/wide/JSON)
├── State Backup and Diff # State backup comparison
├── CUE Ecosystem         # CUE module loading, eval, export
├── Schema Management     # CUE schema validation
├── Three-Segment Version # Three-segment version handling
├── Taint on Upgrade      # Runtime upgrade triggers tool reinstall
├── Tar.xz Archives       # tar.xz archive extraction
├── Update Flags          # --update-tools, --update-runtimes, --update-all
└── Commands Pattern      # Self-managed tools (mise, update-tool mock)
```

## Version Management

CUE manifests are the **single source of truth** for all version strings.
Go test code loads versions from CUE at startup via `versions_test.go`.

### How it works

1. `versions_test.go` defines `e2eVersions` struct and `loadVersions()` function
2. `loadVersions()` uses `internal/config.Loader.LoadFile()` to parse each CUE manifest
3. `BeforeSuite` calls `loadVersions()` and stores the result in a global `versions` variable
4. All test assertions reference `versions.GoVersion`, `versions.GhVersion`, etc.

### Updating versions

**To update a version, just change the `_xxxVersion` variable in the CUE file. No Go test code changes required.**

Example: updating the Go runtime version

```diff
// e2e/config/manifests/runtime.cue
-_goVersion: "1.25.6"
+_goVersion: "1.26.0"
```

| Manifest | Variables | Notes |
|----------|-----------|-------|
| `config/manifests/runtime.cue` | `_goVersion` | |
| `config/manifests/runtime.cue.upgrade` | `_goVersionUpgrade` | |
| `config/manifests/tools.cue` | `_ghVersion` | |
| `config/manifests/delegation.cue` | `_goplsVersion` | |
| `config/dependency-test/parallel.cue` | `_rgVersion`, `_fdVersion`, `_batVersion` | Checksum update required |
| `config/dependency-test/runtime-chain.cue` | `_goVersion` | |
| `config/dependency-test/toolref.cue` | `_jqVersion` | Checksum update required |
| `config/registry/tools.cue` | `_rgVersion`, `_fdVersion`, `_jqVersion` | No checksum (registry resolves) |
| `config/registry/tools.cue.old` | `_rgVersionOld`, `_fdVersionOld`, `_jqVersionOld` | No checksum (registry resolves) |
| `config/commands-test/mise.cue` | — | Commands pattern, no version management |
| `config/commands-test/update.cue` | — | Commands pattern, mock update-tool |
| `config/update-flags-test/latest-tool.cue` | — | Latest-version tool for update flag tests |
| `config/update-flags-test/runtime.cue` | — | Runtime for update flag tests |
| `config/update-flags-test/tool.cue` | — | Pinned tool for update flag tests |

### Maintenance notes

There are three categories of manifests, each with different update requirements:

**1. URL-template manifests (version change only)**

Manifests like `runtime.cue`, `runtime.cue.upgrade`, and `tools.cue` use `\(spec.version)` in URL templates and fetch checksums from a remote URL. Changing the `_xxxVersion` variable is sufficient — URLs and checksum resolution adapt automatically.

**2. Download pattern manifests with inline checksums (version + checksum)**

`parallel.cue` and `toolref.cue` embed per-platform checksum values directly. When updating a version, you must also update all `checksum: value: "sha256:..."` entries for each OS/arch combination.

**3. Registry manifests (version change only)**

`config/registry/tools.cue` and `tools.cue.old` use the aqua registry to resolve download URLs and checksums. Only the version variable needs to be changed.

**4. Commands pattern manifests (no version/checksum)**

`commands-test/mise.cue` and `commands-test/update.cue` use shell commands for installation. No version or checksum management — the tool manages itself.

> **Tip:** When updating `runtime.cue.upgrade`, keep it one patch version ahead of `runtime.cue` so the upgrade test remains meaningful.

## Directory Structure

```
e2e/
├── README.md                    # This file
├── suite_test.go                # Ginkgo suite setup, single Ordered Describe
├── versions_test.go             # CUE → Go version extraction
├── executor.go                  # Test executor (container/native mode)
├── basic_test.go                # Basic workflow tests
├── toolset_test.go              # ToolSet tests
├── registry_test.go             # Aqua registry tests
├── dependency_test.go           # Dependency resolution tests
├── delegation_test.go           # Runtime delegation tests
├── installer_repository_test.go # InstallerRepository tests
├── logs_test.go                 # Log inspection tests
├── get_test.go                  # Resource listing tests
├── state_backup_diff_test.go    # State backup and diff tests
├── cue_ecosystem_test.go        # CUE module loading tests
├── schema_management_test.go    # CUE schema validation tests
├── tag_test.go                  # @tag() injection tests
├── taint_on_upgrade_test.go     # Taint on upgrade tests
├── completion_test.go           # Shell completion tests
├── update_flags_test.go         # Update flag tests
├── commands_test.go             # Commands pattern tests
├── config/
│   ├── manifests/               # Basic test CUE manifests
│   │   ├── runtime.cue
│   │   ├── runtime.cue.upgrade
│   │   ├── tools.cue
│   │   ├── toolset.cue
│   │   └── delegation.cue
│   ├── registry/                # Aqua registry test manifests
│   │   ├── tools.cue
│   │   └── tools.cue.old
│   ├── dependency-test/         # Dependency test CUE manifests
│   │   ├── parallel.cue
│   │   ├── parallel-failure.cue
│   │   ├── runtime-chain.cue
│   │   ├── toolref.cue
│   │   ├── circular.cue
│   │   ├── circular3.cue
│   │   └── invalid-installer.cue
│   ├── delegation-test/         # Runtime delegation manifests
│   │   ├── rust-runtime.cue
│   │   └── rust-delegation.cue
│   ├── installer-repo-test/     # InstallerRepository manifests
│   │   ├── helm-repo.cue
│   │   ├── helm-only.cue
│   │   └── repo-with-tool.cue
│   ├── logs-test/               # Log inspection manifests
│   │   └── failing-tool.cue
│   ├── taint-on-upgrade-test/   # Taint test manifests
│   │   ├── runtime.cue
│   │   ├── runtime.cue.upgrade
│   │   ├── runtime-taint-enabled.cue.upgrade
│   │   └── delegation.cue
│   ├── three-segment-test/      # Three-segment version manifests
│   │   └── logcli.cue
│   ├── tar-xz-test/             # tar.xz archive manifests
│   │   └── tool.cue
│   ├── update-flags-test/       # Update flag manifests
│   │   ├── runtime.cue
│   │   ├── tool.cue
│   │   └── latest-tool.cue
│   └── commands-test/           # Commands pattern manifests
│       ├── mise.cue
│       └── update.cue
└── containers/
    └── ubuntu/
        └── Dockerfile           # Ubuntu test container
```
