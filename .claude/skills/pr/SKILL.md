---
name: pr
description: Run tests, lint, format, create branch if needed, and commit with conventional message.
disable-model-invocation: true
allowed-tools:
  - Bash(make build)
  - Bash(make test)
  - Bash(make test-integration)
  - Bash(make test-e2e)
  - Bash(make lint)
  - Bash(make fmt)
  - Bash(git add *)
  - Bash(git commit *)
  - Bash(git diff *)
  - Bash(git status*)
  - Bash(git log *)
  - Bash(git branch *)
  - Bash(git checkout *)
  - Bash(git rev-parse *)
---

# PR Preparation

Run the full verification pipeline, then commit changes.

## Dynamic context

```!
git branch --show-current
```
```!
git status --short | head -20
```
```!
git log --oneline -5
```

## Steps

### 1. Run tests (same as /test)

**Skip this step** if the immediately preceding turn already ran the same
verification pipeline against the current working tree (e.g., `/copilot`,
`/gha`, or a previous `/pr` invocation that completed cleanly). Reusing
the prior run avoids redundant work when no code changed since.

Otherwise, run sequentially, stop on first failure:
1. `make build`
2. `make test`
3. `make test-integration`
4. `make test-e2e`

### 2. Lint and format

Same skip rule as step 1 — if the preceding turn already ran `make fmt`
and `make lint` against the current tree, skip. Otherwise:

```
make fmt
make lint
```

If `make fmt` produces changes, include them in the commit.

### 3. Branch management

- If on `main`: create a new branch derived from the changes (e.g., `feat/add-foo-support`). Use kebab-case with a conventional prefix.
- If on any other branch: stay on it and add a new commit.

### 4. Commit

1. Stage changes: `git add` the relevant files (not `git add -A`).
2. Write a conventional commit message: `<type>(<scope>): <description>`
   - Types: feat, fix, docs, test, ci, chore, build, refactor
   - Scopes: cue, cuemod, preset, preset/<name>, apply, plan, config, resource, installer, e2e, release, upgrade, etc.
3. Commit with `Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>`.

## Safety

- NEVER run `tomei apply`, `tomei init`, `chezmoi apply`, or `chezmoi init`
- NEVER run `git push` — only commit locally
- NEVER amend existing commits — always create new ones
