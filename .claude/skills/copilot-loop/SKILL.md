---
name: copilot-loop
description: Iterate Copilot review → fix → push → re-request, until Copilot leaves no new comments. Builds on /copilot for the per-round work.
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
  - Bash(git push*)
  - Bash(git rev-parse *)
  - Read
  - Grep
  - Glob
  - Edit
---

# Copilot Review Loop

Fully-automated iteration of GitHub Copilot reviews on the current branch's PR.
Each cycle = request review → wait → triage → fix or reply → commit + push →
resolve threads → re-request. Loop ends when Copilot's next review adds zero
new top-level inline comments.

This skill builds on `/copilot` (per-round mechanics). The loop adds:
1. Initial review request via GraphQL (REST is blocked for the Copilot bot).
2. Background poll for the next Copilot review to be submitted.
3. Push between rounds (vs. the single-round skill which never pushes).
4. Termination condition.

## Dynamic context

```!
git branch --show-current
```
```!
gh pr view --json number,title,url,headRefName --jq '"#\(.number) \(.title) \(.url) [\(.headRefName)]"' 2>/dev/null || echo "no PR found for current branch"
```

## Preconditions

- Current branch has an open PR. (If not, surface this and stop.)
- Local working tree is clean *or* contains only the changes this loop will commit.
- Copilot bot is reviewable on this repo. The botId is `BOT_kgDOCnlnWA` on
  terassyi/tomei. For other repos, derive it via:
  ```
  gh api graphql -f query='query { repository(owner:"<owner>", name:"<repo>") { id } }'
  ```
  then check the suggested reviewers / existing Copilot reviews for the bot id.

## Step 1: Request the first review

GraphQL `requestReviews` with `botIds`. REST `requested_reviewers` does not
accept the Copilot bot.

```
PR_NODE=$(gh api repos/<owner>/<repo>/pulls/<n> --jq '.node_id')
gh api graphql -f query="mutation { requestReviews(input: {pullRequestId: \"$PR_NODE\", botIds: [\"BOT_kgDOCnlnWA\"], union: true}) { pullRequest { id } } }"
```

If Copilot has *already* reviewed at HEAD, skip the request and go straight
to Step 3 — re-requesting on an unchanged HEAD will be a no-op.

## Step 2: Wait for the next review

Run a background poller — do NOT poll synchronously. Use `Bash` with
`run_in_background: true`:

```
prev=$(gh api 'repos/<o>/<r>/pulls/<n>/reviews?per_page=100' --jq '[.[] | select(.user.login=="copilot-pull-request-reviewer[bot]")] | length')
for i in $(seq 1 45); do
  cur=$(gh api 'repos/<o>/<r>/pulls/<n>/reviews?per_page=100' --jq '[.[] | select(.user.login=="copilot-pull-request-reviewer[bot]")] | length' 2>/dev/null || echo $prev)
  [ "$cur" -gt "$prev" ] && { echo "new-review-count=$cur"; break; }
  sleep 20
done
gh api 'repos/<o>/<r>/pulls/<n>/reviews?per_page=100' --jq '[.[] | select(.user.login=="copilot-pull-request-reviewer[bot]")] | .[-1] | {id, state, submitted_at}'
```

**Pagination is mandatory**: always use `?per_page=100` when counting
reviews. Once total reviews on a PR exceed 30, the default page-1 result
no longer contains the latest Copilot review, and the count-based poller
sits silent through the new round. Learned the hard way mid-loop.

You will be auto-notified when the background command exits. Do not Read the
output file in a sleep loop.

If the poller times out without a new review, surface that to the user and
ask whether to retry, wait longer, or stop.

## Step 3: Fetch the new review's inline comments

```
gh api repos/<o>/<r>/pulls/<n>/reviews/<review_id> --jq '{body, state}'
gh api 'repos/<o>/<r>/pulls/<n>/comments?per_page=100&sort=created&direction=desc' \
  --jq '[.[] | select(.user.login=="Copilot" and .pull_request_review_id==<review_id>)] | .[] | {id, line, path, body}'
```

Also read the review `body` — Copilot lists low-confidence/suppressed comments
there. Those are worth addressing even though they have no thread to resolve;
otherwise they re-surface next round.

## Step 4: Triage each comment (delegate to /copilot semantics)

For each inline comment:
- Read the file at the cited line.
- Decide independently: fix, partial fix, or reply with reasoning.
- Edit code with the Edit tool. Build + lint + run tests as appropriate.
- Reply via `gh api .../comments -f body="Fixed. <explanation>" -F in_reply_to=<id>`
  (use `-F` not `-f` for the integer reply id).

## Step 5: Commit + push

```
git add <files>
git commit -m "fix(<scope>): address copilot round-N review comments
  ...
  Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>"
git push
```

Push IS required for this skill — Copilot only re-reviews a new HEAD.

## Step 6: Resolve threads

```
gh api graphql -f query='query { repository(owner:"<o>", name:"<r>") { pullRequest(number:<n>) { reviewThreads(first:50) { nodes { id isResolved } } } } }' \
  --jq '.data.repository.pullRequest.reviewThreads.nodes[] | select(.isResolved==false) | .id' \
  | while read tid; do
      gh api graphql -f query="mutation { resolveReviewThread(input: {threadId: \"$tid\"}) { thread { isResolved } } }" \
        --jq '.data.resolveReviewThread.thread.isResolved'
    done
```

Only resolve threads for comments addressed in *this* round. Threads from
older rounds should already be resolved.

## Step 7: Loop or terminate

Termination conditions, in order of priority:
1. Latest Copilot review has **zero new top-level inline comments** (the
   review body is the summary-only "Pull request overview" with no inline
   comments). Stop and report.
2. Latest Copilot review body explicitly states no further changes needed
   (e.g. "No comments to add", "Looks good"). Stop and report.
3. Comments have visibly turned from bugs/correctness to pure docstring
   nits across 2 consecutive rounds → surface a merge option to the user
   per `feedback_copilot_review_loop` memory, but do NOT stop without
   confirmation since the explicit instruction is to loop fully-auto.

Otherwise: re-request review (Step 1's GraphQL mutation again, on the same
PR_NODE) and go to Step 2.

## Telemetry per round (echo to user)

Before each round's Step 4, echo a one-line summary so the user can audit:
- Round number
- Review id + submitted_at
- Count of new inline comments
- Brief categorization (e.g. "5 substantive, 2 doc nits")

After Step 6, echo: "Round N: pushed <sha>, resolved <k> threads, re-requested review."

## Safety

- NEVER run `tomei apply`, `tomei init`, `chezmoi apply`, or `chezmoi init`.
- NEVER dismiss reviews — only resolve individual threads.
- NEVER force-push or rewrite history. Each round is a new commit on top of
  the previous one.
- Skip resolving a thread if the comment was answered with "No change needed"
  and the reviewer's concern is not actually addressed — leave it for the user.
- If a round's reply count is large (>10), prompt the user once before
  proceeding to push, so they can sanity-check the batch.
- Evaluate every comment independently. Do not rubber-stamp Copilot.
