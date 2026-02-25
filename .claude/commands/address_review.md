# Address PR Review Feedback

This command helps you systematically address feedback received on a pull request from both automated and manual reviewers.

## Required: Task Tracking

**Before starting ANY work**, you MUST create a task to track this work:

```yaml
TaskCreate:
  subject: "Address review feedback for PR #<number>"
  description: "Address reviewer feedback on PR #<number>"
  activeForm: "Addressing PR #<number> feedback"
```

Then immediately mark it `in_progress`. When you've addressed all approved items, pushed changes, and posted the summary comment, mark the task `completed`.

**Why:** External orchestration systems monitor task status to track progress and determine completion. Without a task, the system cannot know when you're done. This is NOT optional.

## Initial Setup

Before proceeding, ensure you have the context needed:

1. **Read CLAUDE.md**: Load `./CLAUDE.md` to understand project conventions and patterns

## Step 1: Gather All Feedback

First, collect all review feedback on the PR:

1. **Get PR number**: Ask the user which PR to address (or infer from current branch if on a PR branch)

2. **Fetch general PR comments**:
   ```bash
   ./scripts/view_comments.sh <PR_NUMBER>
   ```

3. **Fetch line-specific comments**:
   ```bash
   ./scripts/view_line_comments.sh <PR_NUMBER>
   ```

4. **Check PR review status**:
   ```bash
   gh pr view <PR_NUMBER> --json reviews
   ```

## Step 2: Analyze and Categorize Feedback

For each piece of feedback (both general comments and line comments), categorize it:

### Substantive Feedback (Requires Action)
- Bug reports or correctness issues
- Security concerns
- Performance problems
- Missing functionality
- Code style violations that affect readability/maintainability
- Architectural concerns

### Non-Substantive Feedback (No Action Needed)
- Pure praise ("Great work!", "Nice catch!")
- General discussion or questions already answered
- Feedback already addressed in subsequent commits
- Nitpicks the author explicitly marked as optional

## Step 3: Present Analysis to User

Present a markdown-formatted summary of all substantive feedback. Use the following format:

```markdown
## PR #<NUMBER> Review Feedback Summary

### Issues to Address

| # | File/Location | Issue | Recommend | Proposed Fix |
|---|---------------|-------|-----------|--------------|
| 1 | `path/to/file.go:42` | Missing error handling for nil case | Yes | Add nil check before accessing field |
| 2 | `server/handlers.go:128` | Potential race condition | Yes | Add mutex lock |
| 3 | `crawl/worker.go:55` | Typo in comment | No | Minor, doesn't affect functionality |

### Issues NOT Recommended to Address

| # | File/Location | Issue | Reason |
|---|---------------|-------|--------|
| 4 | `config.go:10` | Suggests different config pattern | Out of scope for this PR |

### Additional Context
- Total comments reviewed: X
- Substantive issues found: Y
- Recommended to address: Z
```

**IMPORTANT**: After presenting this summary, you **MUST** ask the user for explicit approval before proceeding. This applies on **every iteration** of the review loop, not just the first. Do NOT skip this step based on prior approvals — each review round may surface different issues that the user wants to handle differently. The user may want to:
- Override your recommendations
- Skip certain fixes
- Add additional context
- Defer some fixes to later

## Step 4: Address Approved Feedback

Once the user approves which items to address:

1. **For each approved item**:
   - Make the necessary code changes
   - Keep track of the commit hash that addresses each issue
   - Update tests if needed

2. **Run pre-commit checks** to ensure:
   - Code compiles without errors
   - All tests pass
   - Linting passes
   - Formatting is correct

3. **Commit the changes** with a clear message describing what was fixed

## Step 5: Respond to Line Comments

For each line comment that was addressed:

1. **Reply to the comment** using the respond script:
   ```bash
   ./scripts/respond_to_line_comment.sh <PR_NUMBER> <COMMENT_ID> "Addressed in commit <HASH>. <brief explanation of fix>"
   ```

2. **Resolve the thread** after replying:
   ```bash
   ./scripts/resolve_line_comment.sh <PR_NUMBER> <COMMENT_ID>
   ```

For items marked as "won't fix":

1. **Reply explaining why**:
   ```bash
   ./scripts/respond_to_line_comment.sh <PR_NUMBER> <COMMENT_ID> "Won't fix: <reason>. <optional: link to future issue if deferring>"
   ```

2. **Resolve the thread** (since we've acknowledged it):
   ```bash
   ./scripts/resolve_line_comment.sh <PR_NUMBER> <COMMENT_ID>
   ```

For items deferred to later (neither fixed nor won't-fixed):
- **Do NOT resolve** - leave the thread open as a reminder

## Step 6: Push Changes

Push the commits to the PR branch:
```bash
git push
```

## Step 7: Post Summary Comment

Post a high-level summary comment on the PR thread:

```bash
gh pr comment <PR_NUMBER> --body "$(cat <<'EOF'
## Review Feedback Addressed

Thanks for the thorough review! Here's a summary of changes made:

### Fixed
- <Brief description of fix 1> (commit abc123)
- <Brief description of fix 2> (commit def456)

### Won't Fix
- <Item>: <Brief reason>

### Deferred
- <Item>: Will address in follow-up PR/issue

---
All tests passing. Ready for re-review.
EOF
)"
```

## Step 8: Re-request Reviews

After all line comments have been resolved and changes pushed, re-request reviews:

1. Request a fresh Copilot code review:
   ```bash
   ./scripts/request_copilot_review.sh <PR_NUMBER>
   ```

2. **If running in a Maestro environment** (i.e., the `maestro-request` command is available on PATH), also spawn a parallel code review:
   ```bash
   OUTPUT=$(maestro-request new "/pr_review <PR_NUMBER>")
   ID=$(echo "$OUTPUT" | grep -oP 'ID: \K[a-f0-9-]+')
   ```
   Replace `<PR_NUMBER>` with the actual PR number. If the spawn fails or `$ID` is empty, fall back to waiting for Copilot only via `maestro-request wait script ./scripts/wait_for_copilot_review.sh --timeout 1800`. Once the wait completes (exit 0), proceed to `/address_review`. If it exits non-zero, stop and wait for instructions. Skip steps 3-5.

3. Update your current task status to indicate you're waiting for reviews.

4. Use `maestro-request wait all` to block until **both** reviews complete:
   ```bash
   maestro-request wait all "script ./scripts/wait_for_copilot_review.sh" "request $ID child_exited" --timeout 1800
   ```
   Do NOT run `wait_for_copilot_review.sh` directly — you must use `maestro-request wait all` so the orchestrator can track the wait.

5. If the wait exits 0 (both reviews completed), execute the `/address_review` skill again to address any new feedback. If the wait exits non-zero, check for available feedback by running `gh pr view <PR_NUMBER> --json comments` and `./scripts/view_line_comments.sh <PR_NUMBER>`. If at least one review produced feedback, still proceed with `/address_review`. Only stop and wait for instructions if neither review produced any feedback. **Stop the loop** if the new review has no substantive comments to address (e.g., both reviewers approve, leave only praise, or repeat previously rejected suggestions).

**If NOT in a Maestro environment** (i.e., `maestro-request` is not available): stop after requesting the Copilot review — do not wait or attempt to address feedback automatically.

## Workflow Summary

1. Gather feedback (view_comments.sh + view_line_comments.sh)
2. Analyze and categorize each item
3. Present summary table to user and **wait for approval**
4. Make approved code changes
5. Run pre-commit checks
6. Commit and push
7. Reply to each line comment (fixed or won't-fix)
8. Resolve addressed threads
9. Post summary comment on PR
10. Re-request Copilot review and spawn parallel /pr_review
11. If in Maestro environment: wait via `maestro-request wait all` for both reviews, then run /address_review again (stop if no new substantive comments)

## Starting the Process

**"What PR number would you like me to address feedback for?"**
