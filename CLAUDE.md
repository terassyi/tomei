# tomei

Declarative, idempotent dev environment setup tool powered by CUE.

## Rules

- **Do not commit until the user explicitly asks**
- **Do not run** `tomei apply`, `tomei init`, `chezmoi apply`, `chezmoi init` locally
- **TDD**: write tests first. Do not merge changes that CI cannot verify
- Run E2E tests via `make test-e2e` (containerized), never natively on the host
- `bin/tomei validate` and `bin/tomei plan` are safe to run

## Development commands

```sh
make build              # Build binary -> bin/tomei
make test               # Unit tests
make test-integration   # Integration tests (network, Linux amd64 only)
make test-e2e           # E2E tests (Docker required)
make lint               # golangci-lint + cue fmt --check
make fmt                # Format code
```

## Test placement

- Unit tests: `*_test.go` next to source in `internal/` and `cmd/`
- Integration tests: `tests/` directory. Requires `//go:build integration` build tag
- E2E tests: `e2e/` directory. Requires `//go:build e2e` build tag. Uses Ginkgo + Gomega

## CUE module changes

- After editing files under `cuemodule/`, run `make vendor-cue` to sync into examples
- `examples/*/cue.mod/pkg/` is generated — do not edit manually
- Run `make unvendor-cue` then `make vendor-cue` for a clean re-vendor

## Code style

- Go: follow `.golangci.yml`
- CUE: `cue fmt`
- CUE / JSON / YAML: 2-space indent
- Unit tests: standard `testing`. E2E / integration: Ginkgo + Gomega

## CI

- `ci.yaml`: build -> test -> lint -> shellcheck -> CUE validate -> integration -> E2E (multi-platform)
- `release.yaml`: GoReleaser
- `publish-module.yaml`: CUE module to OCI registry
