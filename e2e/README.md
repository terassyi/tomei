# E2E Tests

End-to-end tests for toto using Docker containers with Ginkgo BDD framework.

## Overview

These tests verify the complete workflow of toto in a clean Ubuntu environment:

1. Build toto binary for Linux
2. Create a Docker container with the binary
3. Run toto commands to install tools from GitHub Releases
4. Verify the installed tools work correctly

## Requirements

- Docker
- Go 1.25+
- Ginkgo v2 (`go install github.com/onsi/ginkgo/v2/ginkgo@latest`)

## Running Tests

```bash
# From this directory
make build   # Build toto binary and Docker image
make up      # Start the test container
make test    # Run E2E tests
make down    # Stop and remove the test container

# Or using go test directly (requires TOTO_E2E_CONTAINER)
TOTO_E2E_CONTAINER=toto-e2e-ubuntu go test -v ./...

# From project root
make test-e2e
```

## Test Structure (BDD)

```
toto on Ubuntu
├── displays version information
├── validates CUE configuration
├── shows planned changes
├── downloads and installs gh CLI from GitHub
├── places binary in tools directory
├── creates symlink in bin directory
├── allows running gh command after install
├── updates state.json after install
├── is idempotent on subsequent applies
└── does not re-download binary on multiple applies
```

## Test Configuration

The test installs [GitHub CLI (gh)](https://github.com/cli/cli) as a sample tool.

```cue
// e2e/config/tools.cue
gh: {
    apiVersion: "toto.terassyi.net/v1beta1"
    kind:       "Tool"
    metadata: name: "gh"
    spec: {
        installerRef: "download"
        version:      "2.86.0"
        source: {
            url:         "https://github.com/cli/cli/releases/download/v2.86.0/gh_2.86.0_linux_amd64.tar.gz"
            checksum:    "sha256:f3b08bd6a28420cc2229b0a1a687fa25f2b838d3f04b297414c1041ca68103c7"
            archiveType: "tar.gz"
        }
    }
}
```

## Directory Structure

```
e2e/
├── Makefile             # Test targets (build, up, down, test)
├── README.md            # This file
├── suite_test.go        # Ginkgo suite setup
├── e2e_test.go          # BDD test specs
├── config/
│   └── tools.cue        # Test tool configuration
└── containers/
    └── ubuntu/
        └── Dockerfile   # Ubuntu 24.04 test container
```

## Adding New Container Targets

To test on other distributions, add new Dockerfiles under `containers/`:

```
containers/
├── ubuntu/
│   └── Dockerfile
├── fedora/
│   └── Dockerfile
└── alpine/
    └── Dockerfile
```

## Updating Test Tools

To update the gh version:

1. Check latest release:
   ```bash
   curl -s https://api.github.com/repos/cli/cli/releases/latest | jq -r '.tag_name'
   ```

2. Get checksum:
   ```bash
   curl -sL https://github.com/cli/cli/releases/download/vX.Y.Z/gh_X.Y.Z_checksums.txt | grep linux_amd64.tar.gz
   ```

3. Update `config/tools.cue` with new version and checksum

## Cleanup

```bash
make clean   # Remove binary and Docker image
make down    # Stop and remove running container
```
