#!/bin/bash

# A script to view all line comments on a GitHub Pull Request in a formatted manner.
# This helps reviewers see what has already been commented on to avoid duplicates.

# --- Usage ---
if [ "$#" -ne 1 ]; then
    cat <<'EOF'
A helper script to view all line comments on a GitHub Pull Request.

--- PURPOSE ---
This script fetches and displays all line comments (and their replies) on a PR
in a nicely formatted manner. This is useful for:
- Seeing what issues have already been identified
- Avoiding duplicate comments on the same lines
- Understanding the discussion context around specific code changes
- Seeing which threads are resolved vs still open

--- USAGE ---
./scripts/view_line_comments.sh <PR_NUMBER>

--- ARGUMENTS ---
  PR_NUMBER: The number of the target pull request (e.g., 384)

--- OUTPUT FORMAT ---
The script outputs comments grouped by file and line number, showing:
- File path and line number
- Resolution status (✅ RESOLVED or ⚠️ OPEN)
- Comment ID (use this with respond_to_line_comment.sh and resolve_line_comment.sh)
- Comment author and timestamp
- Comment body
- Any replies to the comment

--- RELATED SCRIPTS ---
- ./scripts/respond_to_line_comment.sh <PR> <COMMENT_ID> "<REPLY>" - Reply to a comment
- ./scripts/resolve_line_comment.sh <PR> <COMMENT_ID> - Mark a thread as resolved

--- EXAMPLE ---
./scripts/view_line_comments.sh 384

EOF
    exit 1
fi

# --- Variables ---
PR_NUMBER="$1"
OWNER=$(gh repo view --json owner -q '.owner.login')
REPO=$(gh repo view --json name -q '.name')
REPO_NWO="$OWNER/$REPO"

# --- Functions ---
format_timestamp() {
    # Convert ISO timestamp to readable format
    local timestamp="$1"
    if command -v gdate >/dev/null 2>&1; then
        # macOS with GNU date installed
        gdate -d "$timestamp" "+%Y-%m-%d %H:%M"
    elif date --version >/dev/null 2>&1; then
        # Linux with GNU date
        date -d "$timestamp" "+%Y-%m-%d %H:%M"
    else
        # macOS default date or other BSD date
        date -j -f "%Y-%m-%dT%H:%M:%SZ" "$timestamp" "+%Y-%m-%d %H:%M" 2>/dev/null || echo "$timestamp"
    fi
}

print_separator() {
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

print_file_header() {
    local file="$1"
    echo ""
    print_separator
    echo "📁 FILE: $file"
    print_separator
}

# --- Main Logic ---
echo "Fetching line comments for PR #$PR_NUMBER..."
echo ""

# Fetch all review comments using GitHub REST API
COMMENTS_JSON=$(gh api \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "/repos/$REPO_NWO/pulls/$PR_NUMBER/comments" \
    --paginate)

if [ -z "$COMMENTS_JSON" ] || [ "$COMMENTS_JSON" = "[]" ]; then
    echo "No line comments found on PR #$PR_NUMBER"
    exit 0
fi

# Fetch thread resolution status via GraphQL
THREADS_JSON=$(gh api graphql -f query='
query($owner: String!, $repo: String!, $pr: Int!) {
    repository(owner: $owner, name: $repo) {
        pullRequest(number: $pr) {
            reviewThreads(first: 100) {
                nodes {
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
}' -f owner="$OWNER" -f repo="$REPO" -F pr="$PR_NUMBER" 2>/dev/null)

# Build a lookup table: comment_id -> isResolved
# Output format: "comment_id:true" or "comment_id:false"
RESOLUTION_MAP=$(echo "$THREADS_JSON" | jq -r '
    .data.repository.pullRequest.reviewThreads.nodes[] |
    .isResolved as $resolved |
    .comments.nodes[].databaseId |
    "\(.)::\($resolved)"
' 2>/dev/null)

# Function to check if a comment is in a resolved thread
is_resolved() {
    local comment_id="$1"
    echo "$RESOLUTION_MAP" | grep -q "^${comment_id}::true$"
}

# Count total comments
TOTAL_COMMENTS=$(echo "$COMMENTS_JSON" | jq 'length')
echo "Found $TOTAL_COMMENTS line comment(s)"
echo ""

# Track resolution stats
RESOLVED_THREADS=0
OPEN_THREADS=0

# Group comments by file and sort by line number
echo "$COMMENTS_JSON" | jq -r '
    group_by(.path) |
    .[] |
    sort_by(.line // .original_line) |
    {
        file: .[0].path,
        comments: [.[] | {
            line: (.line // .original_line),
            side: .side,
            author: .user.login,
            created: .created_at,
            updated: .updated_at,
            body: .body,
            id: .id,
            in_reply_to_id: .in_reply_to_id,
            html_url: .html_url
        }]
    } |
    @json
' | while IFS= read -r file_group; do

    # Parse file group
    FILE_PATH=$(echo "$file_group" | jq -r '.file')
    print_file_header "$FILE_PATH"

    # Process each comment in the file
    echo "$file_group" | jq -r '.comments[] | @json' | while IFS= read -r comment; do
        LINE=$(echo "$comment" | jq -r '.line')
        SIDE=$(echo "$comment" | jq -r '.side')
        AUTHOR=$(echo "$comment" | jq -r '.author')
        CREATED=$(echo "$comment" | jq -r '.created')
        BODY=$(echo "$comment" | jq -r '.body')
        COMMENT_ID=$(echo "$comment" | jq -r '.id')
        IN_REPLY_TO=$(echo "$comment" | jq -r '.in_reply_to_id')
        URL=$(echo "$comment" | jq -r '.html_url')

        # Check resolution status
        if is_resolved "$COMMENT_ID"; then
            RESOLUTION_STATUS="✅ RESOLVED"
        else
            RESOLUTION_STATUS="⚠️  OPEN"
        fi

        # Format the display
        echo ""
        if [ "$IN_REPLY_TO" != "null" ]; then
            echo "  └─ Reply (ID: $COMMENT_ID)"
        else
            echo "📍 Line $LINE [$RESOLUTION_STATUS] (Comment ID: $COMMENT_ID):"
        fi

        # Format timestamp
        FORMATTED_TIME=$(format_timestamp "$CREATED")

        # Author and time
        echo "   👤 $AUTHOR • $FORMATTED_TIME"
        echo ""

        # Comment body (indented)
        echo "$BODY" | sed 's/^/   /'
        echo ""

        # Add link to view on GitHub
        echo "   🔗 View on GitHub: $URL"
        echo ""
    done
done

# --- Summary ---
echo ""
print_separator
echo "SUMMARY"
print_separator

# Count threads by resolution status
RESOLVED_COUNT=$(echo "$THREADS_JSON" | jq '[.data.repository.pullRequest.reviewThreads.nodes[] | select(.isResolved == true)] | length' 2>/dev/null || echo "0")
OPEN_COUNT=$(echo "$THREADS_JSON" | jq '[.data.repository.pullRequest.reviewThreads.nodes[] | select(.isResolved == false)] | length' 2>/dev/null || echo "0")

echo ""
echo "Thread Status:"
echo "  ✅ Resolved: $RESOLVED_COUNT"
echo "  ⚠️  Open: $OPEN_COUNT"
echo ""

# Count comments per file
echo "Comments by file:"
echo "$COMMENTS_JSON" | jq -r '
    group_by(.path) |
    map({
        file: .[0].path,
        count: length,
        lines: [.[].line // .[].original_line] | unique | length
    }) |
    .[] |
    "  • \(.file): \(.count) comment(s) on \(.lines) line(s)"
'

echo ""
echo "Total: $TOTAL_COMMENTS comment(s) across all files"
echo ""

# Check for comments that might be from review bots
BOT_COMMENTS=$(echo "$COMMENTS_JSON" | jq -r '[.[] | select(.body | test("PR Review Bot|generated by|automated review"))] | length')
if [ "$BOT_COMMENTS" -gt 0 ]; then
    echo "📊 Note: $BOT_COMMENTS comment(s) appear to be from automated review bots"
fi

echo ""
echo "✅ Comment review complete for PR #$PR_NUMBER"
