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
    pattern: "delegation"
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
// Download Pattern (aqua)
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "aqua"
spec: {
    pattern: "download"
}

// Delegation Pattern (go install)
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "go"
spec: {
    pattern: "delegation"
    runtimeRef: "go"
    commands: {
        install: "go install {{.Package}}@{{.Version}}"
        check: "go version -m {{.BinPath}}"
        remove: "rm {{.BinPath}}"
    }
}

// Delegation Pattern (brew) - No Runtime dependency, self-install via bootstrap
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Installer"
metadata: name: "brew"
spec: {
    pattern: "delegation"
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

Language runtimes.

```cue
apiVersion: "toto.terassyi.net/v1beta1"
kind: "Runtime"
metadata: name: "go"
spec: {
    installerRef: "download"
    version: "1.25.1"
    source: {
        url: "https://go.dev/dl/go{{.Version}}.{{.OS}}-{{.Arch}}.tar.gz"
        checksum: "sha256:..."
        archiveType: "tar.gz"
    }
    binaries: ["go", "gofmt"]
    toolBinPath: "~/go/bin"
    env: {
        GOROOT: "{{.InstallPath}}"
    }
}
```

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

// Runtime Delegation Pattern
apiVersion: "toto.terassyi.net/v1beta1"
kind: "ToolSet"
metadata: name: "go-tools"
spec: {
    installerRef: "go"
    runtimeRef: "go"
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
      "installerRef": "download",
      "version": "1.25.1",
      "digest": "sha256:abc123...",
      "installPath": "~/.local/share/toto/runtimes/go/1.25.1",
      "binaries": ["go", "gofmt"],
      "toolBinPath": "~/go/bin",
      "env": {
        "GOROOT": "~/.local/share/toto/runtimes/go/1.25.1"
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
├── runtimeRef: Tool → Runtime
├── repositoryRef: SystemPackage → SystemPackageRepository
├── installerRef: Each resource → Installer
└── deps: Installer → package names (best effort)
```

### 6.2 Circular Reference Detection

**Algorithm: DFS + Visit State Management**

```
States:
├── unvisited
├── visiting (currently on path)
└── visited (completed)

Procedure:
1. Mark all nodes as unvisited
2. Start DFS from each node
3. Reaching a visiting node → cycle detected
4. DFS complete → mark as visited
```

Detected early with `toto validate`, error message shows the cycle path.

### 6.3 Execution Order Determination

**Algorithm: Topological Sort**

```
1. Build graph from all resources
2. Calculate in-degree (number of dependents)
3. Add nodes with in-degree 0 to queue
4. Remove from queue, add to layer
5. Decrement in-degree of dependent nodes
6. Add to queue when in-degree becomes 0
7. Repeat
```

**Result: Layer Assignment**

```
Layer 0: [go, rust, ripgrep, fd]
Layer 1: [gopls, staticcheck, rust-analyzer]
```

Tasks within the same layer can run in parallel (errgroup, max 5).

### 6.4 Execution Order

```
System Privilege (sudo toto apply --system):
  Layer 0: SystemPackageRepository
  Layer 1: SystemPackageSet

User Privilege (toto apply):
  Layer 0: Runtime, Tool (without runtimeRef)
  Layer 1: Tool (with runtimeRef)
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

### Phase 1: Foundation

```
├── internal/resource/ (types, action)
├── internal/state/ (state.json read/write, flock)
├── internal/config/ (CUE loader foundation)
└── CLI skeleton (cobra: apply, validate, version)
```

### Phase 2: Minimum User Privilege Set

```
├── Tool (Download Pattern, aqua format)
├── Install tools with toto apply
├── Symlink to ~/.local/bin
└── Update state.json
```

### Phase 3: Runtime

```
├── Runtime (Go only initially)
├── Tool Runtime Delegation (go install)
├── Taint Logic
└── toto doctor (unmanaged tool detection)
```

### Phase 4: System Privilege

```
├── SystemInstaller (apt builtin)
├── SystemPackageRepository
├── SystemPackageSet
└── toto apply --system
```

### Phase 5: Extensions

```
├── ToolSet, overlay
├── toto adopt
├── Other Runtimes (Rust, Node)
├── CUE presets
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

## 14. Future Design Considerations

### 14.1 InstallerRepository

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

### 14.2 Authentication & Tokens

For GitHub API rate limit mitigation, private repository access, and authenticated registry support.

**Option A: Include in Installer**

```cue
kind: "Installer"
metadata: name: "aqua"
spec: {
    pattern: "download"
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
    pattern: "download"
    credentialRef: "github"
}
```

**Considerations:**
- Simplicity vs reusability
- When multiple Installers use the same authentication
- Secret management best practices
