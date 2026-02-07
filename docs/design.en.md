# Toto Design Document v2

**Version:** 2.0  
**Date:** 2025-01-28

---

## 1. Overview

Toto is a declarative development environment setup tool. It adopts Kubernetes' Spec/State reconciliation pattern to manage local tools, runtimes, and system packages.

### Design Philosophy

- **Declarative Management**: Define desired state, toto realizes it
- **No Sandboxing**: Directly set up the real environment without virtualization or containers
- **Type Safety with CUE**: Schema validation and flexible configuration
- **Simplicity**: Leverage existing tools (apt, go install) without nix-level complexity

---

## 2. Installer Patterns

Toto supports two installer patterns.

### 2.1 Delegation Pattern

Delegates processing to external commands.

```
Examples:
├── apt install <package>
├── brew install <package>
├── go install <package>
├── cargo install <package>
└── npm install -g <package>
```

Toto instructs "what to install", while external tools perform the actual processing.

### 2.2 Download Pattern

Toto directly downloads and places files.

```
Examples:
├── Fetch binaries from GitHub Releases
├── Fetch Go tarball from go.dev
└── Aqua registry format tools
```

Toto handles checksum verification, extraction, and symlink creation.

---

## 3. Resource Definitions

### 3.1 Classification by Privilege

```
User Privilege (toto apply):
├── Installer  - User-level installer definition (aqua, go, cargo, npm, brew)
├── Runtime    - Language runtimes (Go, Rust, Node)
├── Tool       - Individual tools
└── ToolSet    - Set of multiple tools

System Privilege (sudo toto apply --system):
├── SystemInstaller         - Package manager definition (apt)
├── SystemPackageRepository - Third-party repositories
└── SystemPackageSet        - Package sets
```

### 3.2 Structure of Each Resource

#### SystemInstaller

Package manager definition. apt is provided as a builtin CUE manifest by toto.

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "SystemInstaller"
metadata: name: "apt"
spec: {
    type: "delegation"
    privileged: true
    commands: {
        install: {command: "apt-get", verb: "install -y"}
        remove: {command: "apt-get", verb: "remove -y"}
        check: {command: "dpkg", verb: "-l"}
        update: "apt-get update"
    }
}
```

#### SystemPackageRepository

Third-party repository definition.

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "SystemPackageRepository"
metadata: name: "docker"
spec: {
    installerRef: "apt"
    source: {
        url: "https://download.docker.com/linux/ubuntu"
        keyUrl: "https://download.docker.com/linux/ubuntu/gpg"
        keyHash: "sha256:..."  // optional
        options: {
            distribution: "noble"
            components: "stable"
            arch: "amd64"
        }
    }
}
```

#### SystemPackageSet

Set of packages.

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "SystemPackageSet"
metadata: name: "docker"
spec: {
    installerRef: "apt"
    repositoryRef: "docker"  // optional
    packages: ["docker-ce", "docker-ce-cli", "containerd.io"]
}
```

#### Installer

User-level installer definition. toto provides builtin installers: aqua, go, cargo, npm, brew.

```cue
// Download Pattern (aqua) - Downloads directly from GitHub Releases, etc.
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "aqua"
spec: {
    type: "download"
}

// Delegation Pattern (binstall) - depends on Tool
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "binstall"
spec: {
    type: "delegation"
    toolRef: "cargo-binstall"  // depends on cargo-binstall Tool (installed via cargo install)
    commands: {
        install: "cargo binstall -y {{.Package}}{{if .Version}}@{{.Version}}{{end}}"
        check: "cargo binstall --info {{.Package}}"
        remove: "cargo uninstall {{.Package}}"
    }
}

// Delegation Pattern (brew) - No Runtime dependency, self-install via bootstrap
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "brew"
spec: {
    type: "delegation"
    bootstrap: {
        install: "/bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)\""
        check: "command -v brew"
        remove: "/bin/bash -c \"$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/uninstall.sh)\""
    }
    commands: {
        install: "brew install {{.Package}}"
        check: "brew list {{.Package}}"
        remove: "brew uninstall {{.Package}}"
    }
}
```

#### Runtime

Language runtimes. Supports two installation patterns:

**Download Pattern** - Downloads and extracts tarball directly (e.g., Go):

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Runtime"
metadata: name: "go"
spec: {
    type: "download"
    version: "1.25.1"
    source: {
        url: "https://go.dev/dl/go{{.Version}}.{{.OS}}-{{.Arch}}.tar.gz"
        checksum: "sha256:..."
        archiveType: "tar.gz"
    }
    binaries: ["go", "gofmt"]       // optional: auto-detect executables in bin/ if omitted
    binDir: "{{.InstallPath}}/bin"  // optional: defaults to {{.InstallPath}}/bin
    toolBinPath: "~/go/bin"         // where tools are installed via go install
    commands: {
        install: "go install {{.Package}}@{{.Version}}"  // for Tool installation
    }
    env: {
        GOROOT: "{{.InstallPath}}"
        GOBIN: "~/go/bin"
    }
}
```

**Delegation Pattern** - Executes installer script (e.g., Rust via rustup):

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Runtime"
metadata: name: "rust"
spec: {
    type: "delegation"
    version: "stable"  // "stable", "latest", or specific version "1.83.0"
    bootstrap: {
        install: "curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y --default-toolchain {{.Version}}"
        check: "rustc --version"
        remove: "rustup self uninstall -y"
        resolveVersion: "rustup check 2>/dev/null | grep -oP 'stable-.*?: \\K[0-9.]+' || echo ''"  // optional: resolve "stable"/"latest" to actual version
    }
    commands: {
        install: "cargo install {{.Package}}{{if .Version}} --version {{.Version}}{{end}}"  // for Tool installation
    }
    // binaries: optional - external installer manages placement
    // binDir: optional - defaults to toolBinPath
    toolBinPath: "~/.cargo/bin" // where tools are installed via cargo install
    env: {
        CARGO_HOME: "~/.cargo"
        RUSTUP_HOME: "~/.rustup"
    }
}
```

**Note:** 
- `bootstrap`: Defines install/check/remove for the Runtime itself (delegation pattern only)
- `commands`: Defines install command for Tools using this Runtime (both patterns)

**Key differences:**

| Aspect | Download Pattern | Delegation Pattern |
|--------|------------------|-------------------|
| Installation | toto downloads & extracts tarball | External script installs (e.g., rustup) |
| Source | `source.url` with checksum | `commands.install` script |
| Binary location | Extracted to `dataDir/runtimes/` | Managed by external tool (e.g., `~/.cargo/bin`) |
| Symlinks | Created by toto from extract dir | Not needed (binaries already in toolBinPath) |
| Version management | toto manages versions | External tool manages (e.g., rustup) |

#### Tool (Individual)

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "ripgrep"
spec: {
    installerRef: "aqua"
    version: "14.0.0"
    source: {
        url: "https://github.com/BurntSushi/ripgrep/releases/..."
        checksum: "sha256:..."
        archiveType: "tar.gz"
    }
}
```

#### ToolSet

Set of multiple tools. Eliminates redundancy.

```cue
// Download Pattern
apiVersion: "toto.terassyi.net/v1beta1"
kind: "ToolSet"
metadata: name: "cli-tools"
spec: {
    installerRef: "aqua"
    tools: {
        ripgrep: { version: "14.0.0" }
        fd: { version: "9.0.0" }
        jq: { version: "1.7" }
    }
}

// Install via Runtime (no Installer needed)
apiVersion: "toto.terassyi.net/v1beta1"
kind: "ToolSet"
metadata: name: "go-tools"
spec: {
    runtimeRef: "go"  // Uses Runtime's commands.install
    tools: {
        gopls: { package: "golang.org/x/tools/gopls" }
        staticcheck: { package: "honnef.co/go/tools/cmd/staticcheck" }
    }
}
```

---

## 4. Command System

### 4.1 Privilege Separation

```bash
# User privilege (Runtime, Tool)
toto apply

# System privilege (SystemPackage*)
sudo toto apply --system
```

Execution order: `sudo toto apply --system` → `toto apply`

### 4.2 Command List

```bash
toto init        # Initialize (config.cue, directories, state.json)
toto validate    # CUE syntax + circular reference check
toto plan        # validate + show execution plan
toto apply       # plan + execute
toto env         # Output environment variables (for eval)
toto doctor      # detect unmanaged tools, conflicts
toto adopt       # bring unmanaged tools under management
toto version     # show version
```

### 4.3 toto init

Initialize the environment.

```bash
# Interactive initialization (prompts to create config.cue if missing)
toto init

# Skip prompts and initialize
toto init --yes

# Force reinitialization (resets state.json)
toto init --force
```

Execution steps:
1. Create `~/.config/toto/` directory
2. If `config.cue` doesn't exist, create with default values (interactive or `--yes`)
3. Load path settings from `config.cue`
4. Create data directory (`dataDir`)
5. Create `dataDir/tools/`, `dataDir/runtimes/`
6. Create bin directory (`binDir`)
7. Initialize `dataDir/state.json`

### 4.4 toto env

Outputs environment variables defined in Runtime's `env` field.

```bash
$ toto env
export GOROOT="$HOME/.local/share/toto/runtimes/go/1.25.1"
export GOBIN="$HOME/go/bin"
export CARGO_HOME="$HOME/.cargo"
export RUSTUP_HOME="$HOME/.rustup"
export PATH="$HOME/.local/bin:$HOME/go/bin:$HOME/.cargo/bin:$PATH"
```

**Usage:**

Add the following to your shell profile (`~/.bashrc`, `~/.zshrc`, etc.):

```bash
eval "$(toto env)"
```

**Note:** During `toto apply`, delegation commands are automatically executed with the `env` field's environment variables set by toto. Therefore, `toto env` is for the user's shell environment.

---

## 5. State Management

### 5.1 File Structure

```
User State:
~/.local/share/toto/
├── state.lock  (write PID, for flock)
└── state.json  (state data)

System State:
/var/lib/toto/
├── state.lock
└── state.json
```

### 5.2 Locking Mechanism

- Uses **flock (advisory lock)**
- Acquires flock on state.lock
- Writes own PID on successful acquisition
- Prevents concurrent execution between toto processes
- Cannot prevent manual editing (vim, etc.) - nature of advisory locks

### 5.3 Write Flow

```
1. flock state.lock (TryLock)
2. Failure → Read PID, error "PID 12345 is running"
3. Success → Write own PID
4. Read state.json
5. Write to state.json.tmp
6. rename(state.json.tmp, state.json) ← atomic
7. Unlock state.lock
```

### 5.4 state.json Structure

#### User State

```json
{
  "version": "1",
  "runtimes": {
    "go": {
      "type": "download",
      "version": "1.25.1",
      "digest": "sha256:abc123...",
      "installPath": "~/.local/share/toto/runtimes/go/1.25.1",
      "binaries": ["go", "gofmt"],
      "binDir": "~/.local/share/toto/runtimes/go/1.25.1/bin",
      "toolBinPath": "~/go/bin",
      "env": {
        "GOROOT": "~/.local/share/toto/runtimes/go/1.25.1",
        "GOBIN": "~/go/bin"
      },
      "updatedAt": "2025-01-28T12:00:00Z"
    },
    "rust": {
      "type": "delegation",
      "version": "1.83.0",
      "specVersion": "stable",
      "toolBinPath": "~/.cargo/bin",
      "env": {
        "CARGO_HOME": "~/.cargo",
        "RUSTUP_HOME": "~/.rustup"
      },
      "updatedAt": "2025-01-28T12:00:00Z"
    }
  },
  "tools": {
    "ripgrep": {
      "installerRef": "aqua",
      "version": "14.0.0",
      "digest": "sha256:def456...",
      "installPath": "~/.local/share/toto/tools/ripgrep/14.0.0",
      "binPath": "~/.local/bin/rg",
      "source": {
        "url": "https://github.com/BurntSushi/ripgrep/releases/...",
        "archiveType": "tar.gz"
      },
      "updatedAt": "2025-01-28T12:00:00Z"
    },
    "gopls": {
      "installerRef": "go",
      "runtimeRef": "go",
      "version": "0.16.0",
      "digest": "sha256:ghi789...",
      "installPath": "~/go/bin/gopls",
      "binPath": "~/.local/bin/gopls",
      "package": "golang.org/x/tools/gopls",
      "taintReason": "",
      "updatedAt": "2025-01-28T12:00:00Z"
    }
  }
}
```

#### System State

```json
{
  "version": "1",
  "systemInstallers": {
    "apt": {
      "version": "1",
      "updatedAt": "2025-01-28T12:00:00Z"
    }
  },
  "systemPackageRepositories": {
    "docker": {
      "installerRef": "apt",
      "source": {
        "url": "https://download.docker.com/linux/ubuntu",
        "keyUrl": "https://download.docker.com/linux/ubuntu/gpg",
        "keyDigest": "sha256:..."
      },
      "installedFiles": [
        "/etc/apt/keyrings/toto-docker.asc",
        "/etc/apt/sources.list.d/toto-docker.list"
      ],
      "updatedAt": "2025-01-28T12:00:00Z"
    }
  },
  "systemPackages": {
    "docker": {
      "installerRef": "apt",
      "repositoryRef": "docker",
      "packages": ["docker-ce", "docker-ce-cli", "containerd.io"],
      "installedVersions": {
        "docker-ce": "24.0.0",
        "docker-ce-cli": "24.0.0",
        "containerd.io": "1.6.0"
      },
      "updatedAt": "2025-01-28T12:00:00Z"
    }
  }
}
```

---

## 6. Dependency Graph

### 6.1 Types of Dependencies

```
Explicitly specified:
├── runtimeRef: Tool → Runtime (uses Runtime's commands.install)
├── installerRef: Tool → Installer (uses Installer's commands.install)
├── toolRef: Installer → Tool (Installer depends on a Tool)
├── repositoryRef: SystemPackage → SystemPackageRepository
└── deps: Installer → package names (best effort)
```

**Note:** Tool specifies either `runtimeRef` or `installerRef` (mutually exclusive).

### 6.2 Tool Chain Dependencies

Tools can directly reference a Runtime. Installers are only needed for tools without a Runtime (aqua, brew) or installers that depend on a Tool (binstall).

**Example: Tool as Installer**

```
Runtime(rust) → Tool(cargo-binstall) → Installer(binstall) → Tool(ripgrep)
```

```cue
// 1. Rust Runtime (commands.install defines cargo install)
kind: "Runtime"
metadata: name: "rust"
spec: {
    version: "stable"
    bootstrap: { install: "curl ... | sh", check: "rustc --version", remove: "..." }
    commands: { install: "cargo install {{.Package}}{{if .Version}} --version {{.Version}}{{end}}" }
    ...
}

// 2. cargo-binstall Tool (directly references Runtime)
kind: "Tool"
metadata: name: "cargo-binstall"
spec: {
    runtimeRef: "rust"  // uses Runtime's commands.install
    package: "cargo-binstall"
    version: "1.6.4"
}

// 3. binstall Installer (depends on cargo-binstall Tool)
kind: "Installer"
metadata: name: "binstall"
spec: {
    type: "delegation"
    toolRef: "cargo-binstall"  // ← depends on Tool
    commands: { install: "cargo binstall -y {{.Package}}{{if .Version}}@{{.Version}}{{end}}" }
}

// 4. ripgrep Tool (installed via binstall installer)
kind: "Tool"
metadata: name: "ripgrep"
spec: {
    installerRef: "binstall"
    package: "ripgrep"
    version: "14.1.0"
}
```

### 6.3 DAG Data Structure

The dependency graph is represented as a Directed Acyclic Graph (DAG).

```
┌─────────────────────────────────────────────────────────────────────┐
│                            dag (internal)                            │
├─────────────────────────────────────────────────────────────────────┤
│ nodes: map[string]*Node                                              │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │ "Runtime/rust"       → {Kind: Runtime, Name: "rust"}        │   │
│   │ "Tool/cargo-binstall"→ {Kind: Tool, Name: "cargo-binstall"} │   │
│   │ "Installer/binstall" → {Kind: Installer, Name: "binstall"}  │   │
│   │ "Tool/ripgrep"       → {Kind: Tool, Name: "ripgrep"}        │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│ edges: map[string][]string  (node → dependencies)                   │
│   ┌─────────────────────────────────────────────────────────────┐   │
│   │ "Tool/cargo-binstall" → ["Runtime/rust"]                    │   │
│   │ "Installer/binstall"  → ["Tool/cargo-binstall"]             │   │
│   │ "Tool/ripgrep"        → ["Installer/binstall"]              │   │
│   └─────────────────────────────────────────────────────────────┘   │
│                                                                      │
│ inDegree: map[string]int  (number of dependencies)                  │
│   Runtime/rust: 0, Tool/cargo-binstall: 1, Installer/binstall: 1, ..│
└─────────────────────────────────────────────────────────────────────┘
```

### 6.4 Circular Reference Detection

**Algorithm: DFS + Three-Color Marking**

A standard technique for cycle detection in graphs.

```
Colors:
├── white: unvisited
├── gray: visiting (on DFS stack, part of current path)
└── black: visited (all descendants processed)

Procedure:
1. Initialize all nodes as white
2. Start DFS from each white node
3. Mark node as gray when entering
4. Edge to gray node found → back edge → cycle detected!
5. Mark node as black when DFS completes
```

**Example: Cycle Detection**

```
Normal case:                   Cycle case:
  A → B → C                      A → B
                                 ↑   ↓
  A: white→gray→black            └── C
  B: white→gray→black
  C: white→gray→black            A: gray, B: gray, C: gray
                                 C → A where A is gray → back edge → Cycle!
```

Detected early with `toto validate`, error message shows the cycle path.

### 6.5 Topological Sort

Determines execution order by sorting nodes into layers.

```
Step 1: Find nodes with inDegree=0
┌───────────────────────────────────────────┐
│ Queue: [Runtime/rust]                     │
│ inDegree: {Tool/cargo-binstall: 1, ...}   │
└───────────────────────────────────────────┘

Step 2: Dequeue, add to layer, decrement dependents
┌───────────────────────────────────────────┐
│ Layer 0: [Runtime/rust]                   │
│ Queue: [Tool/cargo-binstall] (inDegree→0) │
└───────────────────────────────────────────┘

Step 3-4: Repeat
┌───────────────────────────────────────────┐
│ Layer 1: [Tool/cargo-binstall]            │
│ Layer 2: [Installer/binstall]             │
│ Layer 3: [Tool/ripgrep]                   │
└───────────────────────────────────────────┘
```

**Result: Execution Layers**

```
Layer 0: [Runtime/rust]         ← No dependencies
Layer 1: [Tool/cargo-binstall]  ← Depends on Runtime/rust
Layer 2: [Installer/binstall]   ← Depends on Tool/cargo-binstall
Layer 3: [Tool/ripgrep]         ← Depends on Installer/binstall
```

### 6.6 Parallel Execution

Nodes within the same layer have no dependencies between them and can run in parallel.

```
                    ┌─────────────┐
                    │Installer/aqua│  Layer 0
                    └──────┬──────┘
           ┌───────────────┼───────────────┐
           ▼               ▼               ▼
    ┌──────────┐    ┌──────────┐    ┌──────────┐
    │Tool/ripgrep│  │ Tool/fd  │    │ Tool/bat │  Layer 1
    └──────────┘    └──────────┘    └──────────┘
    
    → Same layer = no inter-dependencies
    → Can be executed in parallel
```

**Note:** Currently sequential execution due to state file write conflicts.
Future improvement: batch state updates after layer completion.

### 6.7 Execution Order Summary

```
System Privilege (sudo toto apply --system):
  Layer 0: SystemInstaller
  Layer 1: SystemPackageRepository
  Layer 2: SystemPackageSet

User Privilege (toto apply):
  Determined by DAG topological sort:
  - Runtime (no dependencies)
  - Installer (depends on Runtime or Tool)
  - Tool (depends on Installer)
```

---

## 7. Taint Logic

Mechanism to reinstall dependent Tools when Runtime is updated.

### 7.1 Flow

```
1. Runtime (go) updated from 1.25.1 → 1.26.0
2. Search for Tools with runtimeRef: "go"
3. Set taintReason: "runtime_upgraded" on matching Tools
4. Reinstall on next apply
```

### 7.2 Targets

```
go update → Tools installed via go install
rust update → Tools installed via cargo install
node update → Tools installed via npm install -g
```

---

## 8. toto doctor

Detection of unmanaged tools and conflict detection.

### 8.1 Detection Targets

```
By Runtime:
├── go:   ~/go/bin/ (GOBIN)
├── rust: ~/.cargo/bin/
└── node: ~/.npm-global/bin/

Common:
└── Unmanaged files in ~/.local/bin/
```

### 8.2 Example Output

```
$ toto doctor

[go] ~/go/bin/
  gopls        unmanaged
  staticcheck  unmanaged

[rust] ~/.cargo/bin/
  cargo-edit   unmanaged

[Conflicts]
  gopls: found in both ~/.local/bin (toto) and ~/go/bin (unmanaged)
         PATH resolves to: ~/go/bin/gopls

[Suggestions]
  toto adopt gopls staticcheck cargo-edit
```

---

## 9. CUE Schema Design

### 9.1 Basic Structure (K8s Style)

```cue
#Resource: {
    apiVersion: "toto.terassyi.net/v1beta1"
    kind: string
    metadata: {
        name: string
        labels?: [string]: string
    }
    spec: {...}
}
```

### 9.2 Default Values and enabled Flag

```cue
#Tool: {
    version: string
    enabled: bool | *true  // default true
    ...
}
```

### 9.3 Environment Variable Injection

```cue
// Automatically injected by toto
_env: {
    os: "linux" | "darwin"
    arch: "amd64" | "arm64"
    headless: bool
}
```

### 9.4 Conditional Branching (Method A)

```cue
tools: {
    ripgrep: { version: "14.0.0" }
    
    if _env.os == "darwin" {
        pbpaste: {}
    }
    
    if _env.headless {
        vscode: enabled: false
    }
}
```

### 9.5 Overlay (Method B)

```
base/tools.cue
overlays/darwin/tools.cue
overlays/headless/tools.cue
```

Leverages CUE's automatic merge feature for same package. toto selects and loads files based on environment.

### 9.6 Exclusion Expression

```cue
// Disable with enabled: false
tools: vscode: enabled: false
```

---

## 10. Target Environments

```
OS:
├── linux
└── darwin
(Windows is out of scope)

Arch:
├── amd64
└── arm64

Mode:
├── headless (server, CI)
└── desktop (with GUI)
```

---

## 11. Implementation Phases

### Phase 1: Foundation (Completed)

```
├── internal/resource/ (types, action)
├── internal/state/ (state.json read/write, flock)
├── internal/config/ (CUE loader foundation)
├── internal/graph/ (DAG, cycle detection, topological sort)
└── CLI skeleton (cobra: apply, validate, version, init, plan, doctor)
```

### Phase 2: Minimum User Privilege Set (Completed)

```
├── Tool (Download Pattern) with checksum verification
├── Aqua Registry integration (package resolution, --sync)
├── Install tools with toto apply
├── Symlink to ~/.local/bin
└── Update state.json
```

### Phase 3: Runtime (Completed)

```
├── Runtime (Go, Rust, Node.js)
├── Tool Runtime Delegation (go install, cargo install, npm install -g)
├── Taint Logic (runtime upgrade triggers tool reinstall)
├── toto doctor (unmanaged tool detection, conflict detection)
└── Removal dependency guard (reject runtime removal if dependent tools remain)
```

### Phase 4: Parallel Execution & UI (Completed)

```
├── DAG-based parallel execution engine (configurable 1-20)
├── Progress UI with mpb (multi-bar download progress)
├── Event-driven architecture for progress tracking
└── --parallel, --quiet, --no-color flags
```

### Phase 5: ToolSet & E2E (Completed)

```
├── ToolSet expansion with Expandable interface
├── E2E test infrastructure (container-based, Ginkgo v2)
└── Version extraction from CUE (single source of truth)
```

### Phase 6: Userland Commands (Completed)

```
├── toto adopt — bring unmanaged tools (detected by doctor) under toto management
└── toto env — export runtime environment variables for shell (eval $(toto env))
```

### Phase 7: Runtime Delegation & Version Resolution (Completed)

```
├── Delegation pattern for runtime installation (rustup bootstrap)
├── VersionKind type for version classification (exact/latest/alias)
├── Version alias resolution in reconciler (SpecVersion comparison)
├── Auto-update latest-specified tools on --sync (taint-based)
└── E2E tests for Rust delegation runtime with cargo install
```

### Phase 8: Configuration & Registry (Next)

```
├── CUE presets/overlay — environment-based conditional branching (_env.os, _env.arch)
├── InstallerRepository — tool metadata repository management
└── Authentication & tokens — GitHub API rate limiting, private repositories
```

### Phase 9: Performance

```
└── Batch state writes per execution layer (parallel execution optimization)
```

### Phase 10: System Privilege (Deferred)

```
├── SystemInstaller (apt builtin)
├── SystemPackageRepository
├── SystemPackageSet
└── toto apply --system
```

---

## 12. Directory Structure

```
~/.config/toto/           # Config directory (fixed)
├── config.cue            # Path settings (required)
├── tools.cue             # Tool definitions
├── runtimes.cue          # Runtime definitions
├── overlays/             # Environment-specific overlays
│   ├── darwin/
│   ├── linux/
│   ├── headless/
│   └── desktop/
└── system/               # System-level configuration
    ├── repos.cue
    └── packages.cue

~/.local/share/toto/      # Data directory (configurable via config.cue)
├── state.lock
├── state.json
├── runtimes/
│   └── go/1.25.1/
└── tools/
    └── ripgrep/14.0.0/

~/.local/bin/             # Symlink destination (configurable via config.cue)

/var/lib/toto/            # System State
├── state.lock
└── state.json
```

### 12.1 config.cue

Path settings file. Fixed at `~/.config/toto/config.cue`.

```cue
package toto

config: {
    // Data directory (storage for tools, runtimes, state.json)
    dataDir: "~/.local/share/toto"
    
    // Symlink destination
    binDir: "~/.local/bin"
}
```

Default values:
- `dataDir`: `~/.local/share/toto`
- `binDir`: `~/.local/bin`

When `config.cue` doesn't exist, `toto init` will interactively create it with default values.

---

## 13. Security Considerations

- Always verify checksums when downloading
- HTTPS only (enforced in CUE schema)
- Do not use apt-key add, use /etc/apt/keyrings/ + signed-by
- Prevent shell injection: use exec.Command with explicit arguments
- Atomic writes: tmp → rename to prevent corruption

---

## 13.1 Logging

Use `log/slog` for structured logging with human-readable output.

### Log Levels

| Level | Purpose | Example |
|-------|---------|---------|
| Debug | Detailed debug information | HTTP response status, file size |
| Info | Normal operation start/completion | Download started, checksum verified |
| Warn | Recoverable issues, skipped operations | Checksum file not found |
| Error | Failures affecting functionality | Download failed, verification failed |

### Implementation Example

```go
import "log/slog"

// Debug: detailed debug information
slog.Debug("http response received", "status", resp.StatusCode, "contentLength", resp.ContentLength)
slog.Debug("trying checksum algorithm", "algorithm", alg, "url", checksumURL)

// Info: operation start/completion
slog.Info("downloading file", "url", url, "dest", destPath)
slog.Info("checksum verified", "algorithm", alg)

// Warn: recoverable issues
slog.Warn("checksum file not found, skipping verification", "url", checksumURL)

// Error: failures (usually also return error)
slog.Error("failed to download", "url", url, "error", err)
```

### Guidelines

- Use structured key-value pairs for context
- Keep messages concise and human-readable
- Debug: detailed information useful for development and troubleshooting
- Info: log both start and completion of operations as pairs
- Warn: important decisions or skipped operations
- Error: failures affecting functionality (usually also return error)

---

## 14. Testing Strategy

### 14.1 Test Pyramid

```
                    ┌─────────┐
                    │   E2E   │  ← Docker container, real downloads
                   ┌┴─────────┴┐
                   │Integration│  ← Component integration, mock installers
                  ┌┴───────────┴┐
                  │  Unit Tests │  ← Single component, isolated
                 └──────────────┘
```

### 14.2 Unit Tests

**Location:** `internal/*/..._test.go`

**Scope:**
- Single component in isolation
- Uses mocks/stubs for dependencies
- No network access
- No file system side effects (uses `t.TempDir()`)

**Examples:**
- `internal/checksum/checksum_test.go` - Checksum verification logic
- `internal/installer/reconciler/reconciler_test.go` - Action determination
- `internal/state/store_test.go` - State persistence

**Requirements:**
- Fast execution (< 1s per test)
- No external dependencies
- Deterministic results

### 14.3 Integration Tests

**Location:** `tests/`

**Scope:**
- Multiple component integration
- CUE config → Resource → State flow
- Mock installers (no real downloads)
- Real file system operations (in temp directories)

**Test Files:**

| File | Purpose |
|------|---------|
| `tests/resource_test.go` | CUE loading, resource store, dependency resolution |
| `tests/engine_test.go` | Engine with mock installers (Plan, Apply, Upgrade, Remove) |
| `tests/state_test.go` | State persistence, taint logic, concurrent access |

**Requirements:**
- No network access
- No real tool installation
- Uses `t.TempDir()` for isolation
- Clean up after tests (no local environment pollution)

**Mock Installers:**
```go
type mockToolInstaller struct {
    installed map[string]*resource.ToolState
    removed   map[string]bool
}

func (m *mockToolInstaller) Install(ctx context.Context, res *resource.Tool, name string) (*resource.ToolState, error) {
    // Record call, return mock state
}
```

### 14.4 E2E Tests

**Location:** `e2e/`

**Scope:**
- Full system test in Docker container
- Real downloads and installations
- Actual binary execution verification
- Tests `toto apply` command end-to-end

**Requirements:**
- Runs in isolated Docker container
- Requires `TOTO_E2E_CONTAINER` environment variable
- linux/amd64 only
- Network access for real downloads

**Execution:**
```bash
cd e2e
make test          # Run E2E tests in container
make exec          # Shell into test container
```

### 14.5 Test Commands

```bash
# Unit tests only
make test

# All tests including integration
go test ./...

# Run specific package
go test -v ./internal/installer/engine/...

# E2E tests (requires Docker)
cd e2e && make test
```

### 14.6 Test Guidelines

1. **Isolation**: Each test must be independent and not affect other tests
2. **Cleanup**: Use `t.TempDir()` for automatic cleanup
3. **No Side Effects**: Tests must not modify the developer's local environment
4. **Determinism**: Tests must produce the same results on repeated runs
5. **Speed**: Unit tests should be fast; slow tests belong in E2E

---

## 15. Future Design Considerations

### 15.1 InstallerRepository

A repository that provides tool metadata (URL patterns, architecture-specific filenames, etc.) like aqua registry. Similar role to SystemPackageRepository.

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "InstallerRepository"
metadata: name: "aqua-registry"
spec: {
    installerRef: "aqua"
    source: {
        type: "git"  // or "local"
        url: "https://github.com/aquaproj/aqua-registry"
        // branch: "main"
        // localPath: "/path/to/local/registry"
    }
}
```

This simplifies Tool definitions:

```cue
// With InstallerRepository, source is not needed
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Tool"
metadata: name: "ripgrep"
spec: {
    installerRef: "aqua"
    repositoryRef: "aqua-registry"  // optional, can be omitted if using default
    version: "14.1.1"
    // source not needed - auto-resolved from registry
}
```

### 15.2 Authentication & Tokens

For GitHub API rate limit mitigation, private repository access, and authenticated registry support.

**Option A: Include in Installer**

```cue
kind: "Installer"
metadata: name: "aqua"
spec: {
    type: "download"
    auth: {
        tokenEnvVar: "GITHUB_TOKEN"  // get from environment variable
        // or tokenFile: "~/.config/toto/github-token"
    }
}
```

**Option B: Separate Resource (Credential)**

```cue
kind: "Credential"
metadata: name: "github"
spec: {
    type: "token"
    envVar: "GITHUB_TOKEN"
    // or file: "~/.config/toto/github-token"
    // or secretRef: "..." (integration with external secret management)
}

kind: "Installer"
metadata: name: "aqua"
spec: {
    type: "download"
    credentialRef: "github"
}
```

**Considerations:**
- Simplicity vs reusability
- When multiple Installers use the same authentication
- Secret management best practices

---

## 16. TODO

### 16.1 Auto-update latest-specified tools on `--sync`

**Overview:**
When running `toto apply --sync`, if the aqua-registry ref is updated, tools specified with `latest` should have their latest version re-fetched and automatically reinstalled if different from the installed version.

**Current Behavior:**
- `--sync` only updates `registry.aqua.ref` in state.json
- Tools specified with `latest` have their resolved version recorded in state at initial installation
- Subsequent applies compare against the state version, so changes in the latest version are not detected

**Expected Behavior:**
1. Run `toto apply --sync`
2. Fetch the latest aqua-registry ref
3. If ref changed, update state
4. For tools specified with `latest`:
   - Re-fetch the latest version with the new ref
   - Compare with the installed version
   - Reinstall if different

**Implementation Considerations:**
- Add a field like `latestSpec: bool` to ToolState to record whether it was specified as latest
- Or treat tools with empty version in ToolSpec as latest
- Only perform re-check on `--sync` (normal apply trusts the version in state)
