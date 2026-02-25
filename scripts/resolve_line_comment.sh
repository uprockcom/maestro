#!/bin/bash

# A script to resolve a line comment thread on a GitHub Pull Request.
# This uses the GraphQL API since REST API does not support thread resolution.

# --- Usage ---
if [ "$#" -ne 2 ]; then
    cat <<'EOF'
A helper script to resolve (mark as done) a line comment thread on a GitHub Pull Request.

--- PURPOSE ---
This script marks a review comment thread as "resolved" in GitHub's UI.
This is the programmatic equivalent of clicking the "Resolve conversation" button.

--- USAGE ---
./scripts/resolve_line_comment.sh <PR_NUMBER> <COMMENT_ID>

--- ARGUMENTS ---
  PR_NUMBER:   The number of the target pull request (e.g., 49)
  COMMENT_ID:  The numeric ID of any comment in the thread (from view_line_comments.sh)

--- HOW IT WORKS ---
1. Queries GitHub's GraphQL API to find the thread containing the comment
2. Gets the thread ID (PRRT_... format) from the GraphQL response
3. Calls the resolveReviewThread mutation to mark the thread as resolved

--- NOTES ---
- You can provide the ID of ANY comment in the thread (parent or reply)
- The entire thread will be resolved, not just the individual comment
- Resolved threads can be unresolved again in the GitHub UI

--- GETTING COMMENT IDs ---
Use ./scripts/view_line_comments.sh <PR_NUMBER> to see all comments with their IDs.

--- EXAMPLE ---
./scripts/resolve_line_comment.sh 49 2679783228

EOF
    exit 1
fi

# --- Variables ---
PR_NUMBER="$1"
COMMENT_ID="$2"
OWNER=$(gh repo view --json owner -q '.owner.login')
REPO=$(gh repo view --json name -q '.name')

# --- Functions ---
find_thread_id() {
    local pr_number="$1"
    local comment_id="$2"

    # Query all review threads and their comments to find the one containing our comment
    gh api graphql -f query='
    query($owner: String!, $repo: String!, $pr: Int!) {
        repository(owner: $owner, name: $repo) {
            pullRequest(number: $pr) {
                reviewThreads(first: 100) {
                    nodes {
                        id
                        isResolved
                        comments(first: 100) {
                            nodes {
                                databaseId
                            }
                        }
                    }
                }
            }
        }
    }' -f owner="$OWNER" -f repo="$REPO" -F pr="$pr_number" 2>/dev/null | \
    jq -r --argjson cid "$comment_id" '
        .data.repository.pullRequest.reviewThreads.nodes[] |
        select(.comments.nodes | map(.databaseId) | index($cid)) |
        {id: .id, isResolved: .isResolved}
    '
}

resolve_thread() {
    local thread_id="$1"

    gh api graphql -f query='
    mutation($threadId: ID!) {
        resolveReviewThread(input: {threadId: $threadId}) {
            thread {
                id
                isResolved
            }
        }
    }' -f threadId="$thread_id" 2>&1
}

# --- Main Logic ---
echo "Looking up thread for comment #$COMMENT_ID on PR #$PR_NUMBER..."

THREAD_INFO=$(find_thread_id "$PR_NUMBER" "$COMMENT_ID")

if [ -z "$THREAD_INFO" ]; then
    echo "Error: Could not find a thread containing comment #$COMMENT_ID"
    echo ""
    echo "Possible causes:"
    echo "  - The comment ID is invalid"
    echo "  - The comment is not a review comment (line comment)"
    echo "  - The PR number is incorrect"
    echo ""
    echo "Use ./scripts/view_line_comments.sh $PR_NUMBER to see valid comment IDs."
    exit 1
fi

THREAD_ID=$(echo "$THREAD_INFO" | jq -r '.id')
IS_RESOLVED=$(echo "$THREAD_INFO" | jq -r '.isResolved')

if [ "$IS_RESOLVED" = "true" ]; then
    echo "Thread is already resolved."
    exit 0
fi

echo "Found thread: $THREAD_ID"
echo "Resolving thread..."

RESULT=$(resolve_thread "$THREAD_ID")

if echo "$RESULT" | jq -e '.data.resolveReviewThread.thread.isResolved' >/dev/null 2>&1; then
    IS_NOW_RESOLVED=$(echo "$RESULT" | jq -r '.data.resolveReviewThread.thread.isResolved')
    if [ "$IS_NOW_RESOLVED" = "true" ]; then
        echo "Successfully resolved thread."
    else
        echo "Warning: Thread resolution returned but isResolved is false."
        echo "Response: $RESULT"
    fi
else
    echo "Failed to resolve thread."
    echo "Response: $RESULT"

    # Check for common errors
    if echo "$RESULT" | grep -qi "permission\|forbidden\|unauthorized"; then
        echo ""
        echo "Hint: You may not have permission to resolve threads on this PR."
        echo "      Ensure you have write access to the repository."
    fi
    exit 1
fi
