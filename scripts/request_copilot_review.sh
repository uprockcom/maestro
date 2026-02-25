#!/bin/bash

# Request a code review from GitHub Copilot on a Pull Request.
#
# This uses an undocumented workaround: the standard review-request API accepts
# "copilot-pull-request-reviewer[bot]" as a reviewer, even though there is no
# official first-class support for this yet.
#
# The same API call works for both initial requests and re-requests after
# pushing new commits, making the script naturally idempotent.
#
# WORKAROUND NOTE:
#   This approach is confirmed working by GitHub staff (see cli/cli#10598) but
#   is not officially supported and could break without notice.
#
#   Official support is tracked here:
#     https://github.com/cli/cli/issues/10598
#
#   When `gh pr edit --add-reviewer copilot` is eventually supported, this
#   script should be updated to use that instead.
#
#   Alternatively, repository rulesets now support automatic Copilot reviews:
#     Settings > Rules > Rulesets > "Automatically request Copilot code review"
#     https://docs.github.com/en/copilot/how-tos/use-copilot-agents/request-a-code-review/configure-automatic-review

# --- Usage ---
if [ "$#" -gt 1 ]; then
    cat <<'EOF'
Request a code review from GitHub Copilot on a Pull Request.

--- PURPOSE ---
This script adds GitHub Copilot as a reviewer on a PR, triggering its automated
code review. If Copilot has already reviewed the PR, this re-requests a fresh
review (equivalent to clicking "Re-request review" in the GitHub UI).

--- USAGE ---
./scripts/request_copilot_review.sh [PR_NUMBER]

--- ARGUMENTS ---
  PR_NUMBER: (Optional) The number of the target pull request (e.g., 42).
             If omitted, uses the PR associated with the current branch.

--- EXAMPLES ---
# Request review on a specific PR
./scripts/request_copilot_review.sh 42

# Request review on the current branch's PR
./scripts/request_copilot_review.sh

--- RELATED SCRIPTS ---
- ./scripts/view_line_comments.sh <PR> - View all review comments
- ./scripts/respond_to_line_comment.sh <PR> <COMMENT_ID> "<REPLY>" - Reply to a comment
- ./scripts/resolve_line_comment.sh <PR> <COMMENT_ID> - Mark a thread as resolved

EOF
    exit 1
fi

# --- Variables ---
COPILOT_BOT="copilot-pull-request-reviewer[bot]"

if [ -n "$1" ]; then
    PR_NUMBER="$1"
else
    # Detect PR for current branch
    PR_NUMBER=$(gh pr view --json number -q '.number' 2>/dev/null)
    if [ -z "$PR_NUMBER" ]; then
        echo "Error: No PR found for the current branch."
        echo "Either specify a PR number or run this from a branch with an open PR."
        exit 1
    fi
fi

REPO_NWO=$(gh repo view --json owner,name -q '"\(.owner.login)/\(.name)"')

# --- Main Logic ---
echo "Requesting Copilot review on PR #$PR_NUMBER ($REPO_NWO)..."

RESPONSE=$(gh api --method POST \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "/repos/$REPO_NWO/pulls/$PR_NUMBER/requested_reviewers" \
    -f "reviewers[]=$COPILOT_BOT" 2>&1)

if [ $? -eq 0 ]; then
    echo "Successfully requested review from Copilot on PR #$PR_NUMBER."
    echo "Copilot will post its review shortly."
else
    echo "Failed to request Copilot review."
    echo ""

    if echo "$RESPONSE" | grep -qi "not a valid reviewer\|could not resolve\|review cannot be requested"; then
        echo "Hint: Copilot code review may not be enabled for this repository."
        echo "      Ensure GitHub Copilot is available on your plan and enabled"
        echo "      for this repository."
    elif echo "$RESPONSE" | grep -qi "not found\|404"; then
        echo "Hint: PR #$PR_NUMBER may not exist or you may lack access."
    elif echo "$RESPONSE" | grep -qi "permission\|forbidden\|unauthorized\|401\|403"; then
        echo "Hint: Your GitHub token may lack the required permissions."
        echo "      A token with 'Pull requests: Read and write' scope is needed."
    else
        echo "Response: $RESPONSE"
    fi
    exit 1
fi
