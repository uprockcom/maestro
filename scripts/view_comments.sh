#!/bin/bash

# A script to view all general (non-line) comments on a GitHub Pull Request.
# This is a stable wrapper around the GitHub REST API, insulating callers
# from gh CLI breaking changes (e.g. the Projects Classic deprecation).

# --- Usage ---
if [ "$#" -ne 1 ]; then
    cat <<'EOF'
A helper script to view all general comments on a GitHub Pull Request.

--- PURPOSE ---
This script fetches and displays all general (non-line) comments on a PR
in a nicely formatted manner. General comments are top-level conversation
comments — not inline code review comments (use view_line_comments.sh for those).

--- USAGE ---
./scripts/view_comments.sh <PR_NUMBER>

--- ARGUMENTS ---
  PR_NUMBER: The number of the target pull request (e.g., 122)

--- OUTPUT FORMAT ---
The script outputs comments in chronological order, showing:
- Comment author and timestamp
- Comment body
- Association badge (MEMBER, CONTRIBUTOR, etc.)

--- RELATED SCRIPTS ---
- ./scripts/view_line_comments.sh <PR> - View inline code review comments
- ./scripts/respond_to_line_comment.sh <PR> <COMMENT_ID> "<REPLY>" - Reply to a line comment
- ./scripts/resolve_line_comment.sh <PR> <COMMENT_ID> - Mark a thread as resolved

--- EXAMPLE ---
./scripts/view_comments.sh 122

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
    local timestamp="$1"
    if command -v gdate >/dev/null 2>&1; then
        gdate -d "$timestamp" "+%Y-%m-%d %H:%M"
    elif date --version >/dev/null 2>&1; then
        date -d "$timestamp" "+%Y-%m-%d %H:%M"
    else
        date -j -f "%Y-%m-%dT%H:%M:%SZ" "$timestamp" "+%Y-%m-%d %H:%M" 2>/dev/null || echo "$timestamp"
    fi
}

print_separator() {
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

# --- Main Logic ---
echo "Fetching general comments for PR #$PR_NUMBER..."
echo ""

# Fetch the PR body and metadata
PR_JSON=$(gh api \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "/repos/$REPO_NWO/pulls/$PR_NUMBER")

if [ $? -ne 0 ]; then
    echo "Error: Failed to fetch PR #$PR_NUMBER"
    exit 1
fi

# Display PR description
PR_TITLE=$(echo "$PR_JSON" | jq -r '.title')
PR_AUTHOR=$(echo "$PR_JSON" | jq -r '.user.login')
PR_CREATED=$(echo "$PR_JSON" | jq -r '.created_at')
PR_BODY=$(echo "$PR_JSON" | jq -r '.body // "No description provided."')
PR_STATE=$(echo "$PR_JSON" | jq -r '.state')

print_separator
echo "PR #$PR_NUMBER: $PR_TITLE"
echo "State: $PR_STATE | Author: $PR_AUTHOR | Created: $(format_timestamp "$PR_CREATED")"
print_separator
echo ""
echo "$PR_BODY"
echo ""

# Fetch all issue comments (general PR comments use the issues API)
COMMENTS_JSON=$(gh api \
    -H "Accept: application/vnd.github+json" \
    -H "X-GitHub-Api-Version: 2022-11-28" \
    "/repos/$REPO_NWO/issues/$PR_NUMBER/comments" \
    --paginate)

if [ -z "$COMMENTS_JSON" ] || [ "$COMMENTS_JSON" = "[]" ]; then
    echo "No general comments found on PR #$PR_NUMBER"
    exit 0
fi

TOTAL_COMMENTS=$(echo "$COMMENTS_JSON" | jq 'length')
print_separator
echo "COMMENTS ($TOTAL_COMMENTS)"
print_separator

echo "$COMMENTS_JSON" | jq -r '.[] | @json' | while IFS= read -r comment; do
    AUTHOR=$(echo "$comment" | jq -r '.user.login')
    CREATED=$(echo "$comment" | jq -r '.created_at')
    BODY=$(echo "$comment" | jq -r '.body')
    ASSOCIATION=$(echo "$comment" | jq -r '.author_association')
    URL=$(echo "$comment" | jq -r '.html_url')

    FORMATTED_TIME=$(format_timestamp "$CREATED")

    # Format association badge
    BADGE=""
    case "$ASSOCIATION" in
        OWNER)       BADGE=" [OWNER]" ;;
        MEMBER)      BADGE=" [MEMBER]" ;;
        CONTRIBUTOR) BADGE=" [CONTRIBUTOR]" ;;
        COLLABORATOR) BADGE=" [COLLABORATOR]" ;;
    esac

    echo ""
    echo "💬 $AUTHOR$BADGE • $FORMATTED_TIME"
    echo ""
    echo "$BODY" | sed 's/^/   /'
    echo ""
    echo "   🔗 $URL"
    echo ""
    print_separator
done

# --- Summary ---
echo ""
echo "Total: $TOTAL_COMMENTS general comment(s) on PR #$PR_NUMBER"

# Count unique commenters
UNIQUE_AUTHORS=$(echo "$COMMENTS_JSON" | jq -r '[.[].user.login] | unique | length')
echo "Unique commenters: $UNIQUE_AUTHORS"
echo ""
echo "✅ Comment review complete for PR #$PR_NUMBER"
