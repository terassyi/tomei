---
name: test
description: Run full test suite — unit, integration, e2e sequentially. Stops on first failure.
disable-model-invocation: true
allowed-tools:
  - Bash(make build)
  - Bash(make test)
  - Bash(make test-integration)
  - Bash(make test-e2e)
---

# Run Test Suite

Run the full test pipeline sequentially. Stop immediately on first failure.

## Steps

1. **Build**: `make build`
2. **Unit tests**: `make test`
3. **Integration tests**: `make test-integration` (requires network, Linux amd64)
4. **E2E tests**: `make test-e2e` (requires Docker, runs in containers)

## On failure

Stop at the failing step. Report the error output and suggest a fix.

## On success

Print a summary:
```
Build              PASS
Unit tests         PASS
Integration tests  PASS
E2E tests          PASS
```

## Safety

- NEVER run `tomei apply` or `tomei init`
- E2E tests run in containers — never run E2E test binaries natively
