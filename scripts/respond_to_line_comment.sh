#!/bin/bash

# A script to reply to an existing line comment on a GitHub Pull Request.
# This uses the dedicated reply endpoint for accurate threading.

# --- Usage ---
if [ "$#" -ne 3 ]; then
    cat <<'EOF'
A helper script to reply to an existing line comment on a GitHub Pull Request.

--- PURPOSE ---
This script posts a reply to an existing review comment (line comment) on a PR.
Replies appear threaded under the original comment in the GitHub UI.

--- USAGE ---
./scripts/respond_to_line_comment.sh <PR_NUMBER> <COMMENT_ID> "<REPLY_BODY>"

--- ARGUMENTS ---
  PR_NUMBER:   The number of the target pull request (e.g., 49)
  COMMENT_ID:  The numeric ID of the comment to reply to (from view_line_comments.sh)
  REPLY_BODY:  The text of the reply. Must be enclosed in double quotes.

--- HOW IT WORKS ---
1. Uses the dedicated GitHub API reply endpoint for review comments
2. The reply appears threaded under the original comment
3. Note: You can only reply to top-level comments, not to replies

--- GETTING COMMENT IDs ---
Use ./scripts/view_line_comments.sh <PR_NUMBER> to see all comments with their IDs.
The ID is shown in the output for each comment.

--- EXAMPLE ---
./scripts/respond_to_line_comment.sh 49 2679783228 "Fixed in the latest commit. Thanks for catching this!"

EOF
    exit 1
fi

# --- Variables ---
PR_NUMBER="$1"
COMMENT_ID="$2"
REPLY_BODY="$3"
REPO_NWO=$(gh repo view --json owner,name -q '"\(.owner.login)/\(.name)"')

# --- Logic ---
echo "Posting reply to comment #$COMMENT_ID on PR #$PR_NUMBER..."

RESPONSE=$(gh api \
    --method POST \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "/repos/$REPO_NWO/pulls/$PR_NUMBER/comments/$COMMENT_ID/replies" \
    -f body="$REPLY_BODY" 2>&1)

if [ $? -eq 0 ]; then
    # Extract the URL of the new reply
    REPLY_URL=$(echo "$RESPONSE" | jq -r '.html_url // empty')
    if [ -n "$REPLY_URL" ]; then
        echo "Successfully posted reply."
        echo "View at: $REPLY_URL"
    else
        echo "Successfully posted reply."
    fi
else
    echo "Failed to post reply."
    echo "Error: $RESPONSE"

    # Check for common errors
    if echo "$RESPONSE" | grep -q "Not Found"; then
        echo ""
        echo "Hint: The comment ID may be invalid or the comment may be a reply itself."
        echo "      You can only reply to top-level comments, not to other replies."
        echo "      Use ./scripts/view_line_comments.sh $PR_NUMBER to find valid comment IDs."
    fi
    exit 1
fi
