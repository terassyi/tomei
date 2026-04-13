---
name: gha
description: Inspect GitHub Actions CI failure logs and auto-fix the issues.
disable-model-invocation: true
allowed-tools:
  - Bash(gh run view *)
  - Bash(gh run watch *)
  - Bash(gh api *)
  - Bash(gh pr view *)
  - Bash(make build)
  - Bash(make test)
  - Bash(make test-integration)
  - Bash(make test-e2e)
  - Bash(make lint)
  - Bash(make fmt)
  - Bash(make vendor-cue)
  - Bash(cue fmt *)
  - Bash(git add *)
  - Bash(git commit *)
  - Bash(git diff *)
  - Bash(git status*)
  - Bash(git log *)
  - Bash(git rev-parse *)
  - Read
  - Grep
  - Glob
  - Edit
argument-hint: "<run-url-or-id> — GitHub Actions run URL or run ID"
---

# GitHub Actions CI Fix

Inspect a failed GitHub Actions run, diagnose the failure, and fix it.

## Dynamic context

```!
git branch --show-current
```
```!
git status --short | head -20
```

## Step 1: Fetch run details

Parse the argument. Accept either:
- Full URL: `https://github.com/terassyi/tomei/actions/runs/12345`
- Run ID: `12345`

Extract the run ID, then fetch run metadata:

```
gh run view <run_id> --json status,conclusion,jobs,name,headBranch
```

## Step 2: Identify failed jobs

List all jobs and find the failed ones:

```
gh run view <run_id> --json jobs --jq '.jobs[] | select(.conclusion == "failure") | {name, conclusion, steps: [.steps[] | select(.conclusion == "failure") | .name]}'
```

## Step 3: Fetch failure logs

For each failed job, get the logs:

```
gh run view <run_id> --log-failed
```

Read the logs carefully. Identify:
- Which step failed (Build, Unit Test, Lint, CUE Validate, Integration, E2E, etc.)
- The exact error message
- The file and line number if available

## Step 4: Diagnose

Categorize the failure:

| CI Job | Common causes |
|--------|--------------|
| Build | Compilation error, missing import, type mismatch |
| Unit Test | Test assertion failure, race condition |
| Lint | golangci-lint violation, cue fmt check failure |
| CUE Validate | Schema violation, invalid CUE syntax |
| Integration Test | Network issue, API change, platform-specific bug |
| E2E Test | Container issue, timeout, behavior regression |

Read the relevant source files to understand the context around the error.

## Step 5: Fix

Apply the fix. Common fixes:

- **Lint failure**: `make fmt`, then fix remaining lint issues manually
- **CUE format**: `cue fmt ./cuemodule/...`
- **Build error**: fix the compilation error in the Go source
- **Test failure**: fix the code or update the test expectation
- **CUE validation**: fix the schema or manifest, then `make vendor-cue`

## Step 6: Verify locally

Reproduce and verify the fix locally by running the same step that failed:

- Build failed → `make build`
- Unit test failed → `make test` (or `go test -v -race -run <TestName> <package>`)
- Lint failed → `make lint`
- CUE validate failed → `cd cuemodule && cue vet ./...`
- Integration failed → `make test-integration`
- E2E failed → `make test-e2e`

## Step 7: Report

Print a summary:

```
## CI Fix: run <run_id>

### Failed job
<job name> / <step name>

### Root cause
<explanation>

### Fix applied
<what was changed and why>

### Local verification
<which command was run and result>
```

## Safety

- NEVER run `tomei apply`, `tomei init`, `chezmoi apply`, or `chezmoi init`
- NEVER run `git push`
- NEVER commit automatically — report the fix and let the user decide (or use `/pr`)
