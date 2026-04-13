---
name: refactor
description: Expert team review and refactor — tomei, Go, TDD, OS specialists discuss the diff and implement improvements.
disable-model-invocation: true
allowed-tools:
  - Bash(git diff *)
  - Bash(git log *)
  - Bash(git status*)
  - Bash(git branch *)
  - Bash(git rev-parse *)
  - Bash(make build)
  - Bash(make test)
  - Bash(make lint)
  - Bash(make fmt)
  - Bash(go vet *)
  - Read
  - Grep
  - Glob
  - Edit
---

# Expert Team Refactor

Assemble a team of four specialists to review the current diff and propose targeted refactoring.

## Dynamic context

```!
git branch --show-current
```
```!
git diff --stat
```
```!
git diff main..HEAD --stat 2>/dev/null || echo "on main, using staged/unstaged diff"
```

## Step 1: Gather the diff

- Non-main branch: `git diff main..HEAD`
- Main with staged changes: `git diff --cached`
- Main with unstaged changes: `git diff`
- Read all changed files in full to understand context.

## Step 2: Expert team review

### tomei Architect
- CUE schema consistency and resource model design
- tomei-specific patterns (installer/runtime/tool lifecycle)
- Alignment with existing presets and conventions in `cuemodule/`

### Go Expert
- Go idioms, naming, error handling (`%w` wrapping, sentinel errors)
- Performance and concurrency patterns
- golangci-lint compliance

### TDD Specialist
- Test coverage gaps for changed code
- Test design: table-driven tests, edge cases, error paths
- Ginkgo/Gomega patterns for integration/E2E
- Test isolation and determinism

### OS/Infra Expert
- File system operations: path handling, permissions, symlinks
- Process management: exec, signal handling
- Cross-platform compatibility (linux/darwin, amd64/arm64)

## Step 3: Discussion and consensus

Present findings as a structured discussion. Prioritize:
1. Correctness issues (bugs, race conditions)
2. Maintainability
3. Test gaps
4. Style/idiom alignment

Skip trivial or cosmetic changes.

## Step 4: Implement refactoring

Apply the agreed refactorings.

## Step 5: Verify

1. `make build`
2. `make test`

Report what was changed and why.

## Safety

- NEVER run `tomei apply` or `tomei init`
- NEVER commit — use `/pr` afterward
- Do not add features or change behavior — only improve structure and quality
