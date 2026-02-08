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
├── Basic             # init, validate, plan, apply, runtime/tool install, upgrade, doctor
├── ToolSet           # ToolSet expansion and installation via runtime delegation
├── Aqua Registry     # Registry-based tool install, version upgrade/downgrade
└── Dependency Resolution  # Circular detection, parallel install, runtime chain, toolRef
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

### Maintenance notes

There are three categories of manifests, each with different update requirements:

**1. URL-template manifests (version change only)**

Manifests like `runtime.cue`, `runtime.cue.upgrade`, and `tools.cue` use `\(spec.version)` in URL templates and fetch checksums from a remote URL. Changing the `_xxxVersion` variable is sufficient — URLs and checksum resolution adapt automatically.

**2. Download pattern manifests with inline checksums (version + checksum)**

`parallel.cue` and `toolref.cue` embed per-platform checksum values directly. When updating a version, you must also update all `checksum: value: "sha256:..."` entries for each OS/arch combination.

**3. Registry manifests (version change only)**

`config/registry/tools.cue` and `tools.cue.old` use the aqua registry to resolve download URLs and checksums. Only the version variable needs to be changed.

> **Tip:** When updating `runtime.cue.upgrade`, keep it one patch version ahead of `runtime.cue` so the upgrade test remains meaningful.

## Directory Structure

```
e2e/
├── README.md                # This file
├── suite_test.go            # Ginkgo suite setup, single Ordered Describe
├── versions_test.go         # CUE → Go version extraction
├── executor.go              # Test executor (container/native mode)
├── basic_test.go            # Basic workflow tests
├── toolset_test.go          # ToolSet tests
├── registry_test.go         # Aqua registry tests
├── dependency_test.go       # Dependency resolution tests
├── config/
│   ├── manifests/           # Basic test CUE manifests
│   │   ├── runtime.cue
│   │   ├── runtime.cue.upgrade
│   │   ├── tools.cue
│   │   ├── toolset.cue
│   │   ├── delegation.cue
│   │   └── registry/
│   │       ├── tools.cue
│   │       └── tools.cue.old
│   └── dependency-test/     # Dependency test CUE manifests
│       ├── parallel.cue
│       ├── runtime-chain.cue
│       ├── toolref.cue
│       ├── circular.cue
│       ├── circular3.cue
│       └── invalid-installer.cue
└── containers/
    └── ubuntu/
        └── Dockerfile       # Ubuntu test container
```
