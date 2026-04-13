---
name: copilot
description: Respond to GitHub Copilot review comments — fix or reply, then commit and resolve threads.
disable-model-invocation: true
allowed-tools:
  - Bash(gh pr view *)
  - Bash(gh pr list*)
  - Bash(gh api *)
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
  - Bash(git rev-parse *)
  - Read
  - Grep
  - Glob
  - Edit
---

# Copilot Review Response

Fetch GitHub Copilot review comments, address each one, commit fixes, and resolve threads.

## Dynamic context

```!
git branch --show-current
```
```!
gh pr view --json number,title,url --jq '"#\(.number) \(.title) \(.url)"' 2>/dev/null || echo "no PR found for current branch"
```

## Step 1: Fetch review comments

Get PR number for the current branch, then fetch all review comments:

```
gh api repos/{owner}/{repo}/pulls/{pr_number}/comments
```

Filter to unresolved comments only.

## Step 2: Evaluate each comment

For each unresolved comment:
- Read the relevant code at the file and line mentioned.
- Assess independently and objectively — do NOT blindly accept or reject.

## Step 3: Respond

### Fix needed:
1. Implement the fix.
2. Reply via API: `gh api repos/{owner}/{repo}/pulls/{pr_number}/comments -f body="Fixed. <explanation>" -f in_reply_to={comment_id}`

### No fix needed:
1. Reply with reasoning: `gh api ... -f body="No change needed. <reasoning>" -f in_reply_to={comment_id}`

## Step 4: Commit (same as /pr flow)

If code changes were made:
1. `make build` → `make test` → `make test-integration` → `make test-e2e`
2. `make fmt` + `make lint`
3. `git add` relevant files
4. Commit: `fix(<scope>): address copilot review comments`
5. Include `Co-Authored-By: Claude Opus 4.6 (1M context) <noreply@anthropic.com>`

## Step 5: Resolve threads

```
gh api graphql -f query='mutation { resolveReviewThread(input: {threadId: "<thread_id>"}) { thread { isResolved } } }'
```

## Safety

- NEVER run `tomei apply`, `tomei init`, `chezmoi apply`, or `chezmoi init`
- NEVER run `git push`
- NEVER dismiss reviews — only resolve individual threads
- Evaluate comments objectively
