---
name: release-check
description: Pre-release checklist — verify CI status, version tags, CUE module coordination, changelog preview. Read-only.
disable-model-invocation: true
allowed-tools:
  - Bash(gh pr list*)
  - Bash(gh pr view *)
  - Bash(gh pr status*)
  - Bash(git log *)
  - Bash(git diff *)
  - Bash(git status*)
  - Bash(git tag *)
  - Bash(git describe *)
  - Bash(git rev-parse *)
  - Bash(git show *)
  - Bash(git branch *)
  - Read
  - Grep
argument-hint: "<version> — target version (e.g., 0.2.0). Omit 'v' prefix."
---

# Pre-Release Checklist

Verify everything is ready for a release. This skill is READ-ONLY.

## Dynamic context

```!
git describe --tags --always 2>/dev/null
```
```!
git tag --sort=-v:refname | head -10
```
```!
git tag --list 'cuemodule/*' --sort=-v:refname | head -5
```
```!
gh pr list --state open --json number,title --jq '.[] | "#\(.number) \(.title)"' 2>/dev/null | head -10
```

## Steps

1. **Parse version**: derive binary tag `v<version>` and CUE module tag `cuemodule/v<version>`.

2. **Check branch state**:
   - On `main`?
   - Clean working tree?
   - HEAD matches `origin/main`?

3. **Unreleased changes**: `git log <last-tag>..HEAD --oneline`. Categorize by type.

4. **CUE module coordination**:
   - Did `cuemodule/` change since last CUE module tag?
   - If yes: remind that `cuemodule/v<version>` must be tagged FIRST.

5. **Changelog preview**: group commits by type (Features, Bug Fixes, etc.).

6. **Output checklist**:
   ```
   ## Release Checklist: v<version>

   - [ ] On main, clean tree
   - [ ] Up to date with origin/main
   - [ ] CI passing
   - [ ] CUE module tag coordination (if needed)

   ### Release Commands (do NOT run)
   git tag cuemodule/v<version>  # only if CUE changed
   git push origin cuemodule/v<version>
   git tag v<version>
   git push origin v<version>
   ```

## Safety

- READ-ONLY — does NOT create tags, push, or modify anything
- NEVER run `git push` or `git tag`
- NEVER run `tomei apply` or `tomei init`
