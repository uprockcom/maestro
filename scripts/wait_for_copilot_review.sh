#!/bin/bash

# Wait for a GitHub Copilot code review to complete on a Pull Request.
#
# This script polls the GitHub API to detect when Copilot finishes reviewing.
# It uses two lightweight endpoints:
#   1. /pulls/{PR}/requested_reviewers - to check if Copilot is still pending
#   2. /pulls/{PR}/reviews - to confirm the new review arrived
#
# The script handles multiple review rounds correctly by recording the initial
# count of Copilot reviews and waiting for a new one to appear.
#
# Typical Copilot reviews take 5-15 minutes, so we poll once per minute
# to stay well within GitHub API rate limits.

# --- Usage ---
if [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    cat <<'EOF'
Wait for a GitHub Copilot code review to complete on a Pull Request.

--- PURPOSE ---
Blocks until Copilot finishes its code review. Useful for CI/CD pipelines
and automated workflows where you need to wait for Copilot's feedback
before proceeding.

--- USAGE ---
./scripts/wait_for_copilot_review.sh [PR_NUMBER] [OPTIONS]

--- ARGUMENTS ---
  PR_NUMBER:   (Optional) The number of the target pull request (e.g., 42).
               If omitted, uses the PR associated with the current branch.

--- OPTIONS ---
  --timeout N  Maximum time to wait in minutes (default: 30)
  --interval N Polling interval in seconds (default: 60)
  --quiet      Suppress progress messages
  -h, --help   Show this help message

--- EXIT CODES ---
  0  Copilot review received successfully
  1  Error (missing PR, API failure, etc.)
  2  Timeout waiting for review
  3  No pending Copilot review found

--- EXAMPLES ---
# Wait for review on current branch's PR
./scripts/wait_for_copilot_review.sh

# Wait for review on a specific PR
./scripts/wait_for_copilot_review.sh 42

# Wait up to 45 minutes, polling every 30 seconds
./scripts/wait_for_copilot_review.sh 42 --timeout 45 --interval 30

--- RELATED SCRIPTS ---
- ./scripts/request_copilot_review.sh [PR] - Request/re-request Copilot review
- ./scripts/view_line_comments.sh <PR> - View all review comments

EOF
    exit 0
fi

# --- Parse Arguments ---
TIMEOUT_MINUTES=30
POLL_INTERVAL=60
QUIET=false
PR_NUMBER=""

while [ $# -gt 0 ]; do
    case "$1" in
        --timeout)
            if [ -z "$2" ] || ! [[ "$2" =~ ^[0-9]+$ ]]; then
                echo "Error: --timeout requires a numeric value (minutes)." >&2
                exit 1
            fi
            TIMEOUT_MINUTES="$2"
            shift 2
            ;;
        --interval)
            if [ -z "$2" ] || ! [[ "$2" =~ ^[0-9]+$ ]]; then
                echo "Error: --interval requires a numeric value (seconds)." >&2
                exit 1
            fi
            if [ "$2" -lt 1 ]; then
                echo "Error: --interval must be at least 1 second." >&2
                exit 1
            fi
            POLL_INTERVAL="$2"
            shift 2
            ;;
        --quiet)
            QUIET=true
            shift
            ;;
        -*)
            echo "Unknown option: $1" >&2
            exit 1
            ;;
        *)
            if [ -z "$PR_NUMBER" ]; then
                PR_NUMBER="$1"
            fi
            shift
            ;;
    esac
done

# --- Resolve PR Number ---
if [ -z "$PR_NUMBER" ]; then
    PR_NUMBER=$(gh pr view --json number -q '.number' 2>/dev/null)
    if [ -z "$PR_NUMBER" ]; then
        echo "Error: No PR found for the current branch."
        echo "Either specify a PR number or run this from a branch with an open PR."
        exit 1
    fi
fi

REPO_NWO=$(gh repo view --json owner,name -q '"\(.owner.login)/\(.name)"')
if [ -z "$REPO_NWO" ]; then
    echo "Error: Could not determine repository."
    exit 1
fi

COPILOT_BOT="copilot-pull-request-reviewer[bot]"
TIMEOUT_SECONDS=$((TIMEOUT_MINUTES * 60))

log() {
    if [ "$QUIET" = false ]; then
        echo "$@"
    fi
}

# --- Get Initial Copilot Review Count ---
# NOTE: exit 1 is intentional here and in copilot_is_pending() — we cannot
# continue polling without a reliable review count or pending status.
# By contrast, get_latest_copilot_review() uses 'return 1' because the
# review display at the end is non-critical (the review was already detected).
get_copilot_review_count() {
    local result
    result=$(gh api \
        -H "Accept: application/vnd.github+json" \
        -H "X-GitHub-Api-Version: 2022-11-28" \
        "/repos/$REPO_NWO/pulls/$PR_NUMBER/reviews" \
        --jq "[.[] | select(.user.login == \"$COPILOT_BOT\")] | length" 2>&1)
    if [ $? -ne 0 ]; then
        echo "Error: Failed to query reviews for PR #$PR_NUMBER." >&2
        echo "$result" >&2
        exit 1
    fi
    echo "$result"
}

# --- Get Latest Copilot Review ---
get_latest_copilot_review() {
    local result
    result=$(gh api \
        -H "Accept: application/vnd.github+json" \
        -H "X-GitHub-Api-Version: 2022-11-28" \
        "/repos/$REPO_NWO/pulls/$PR_NUMBER/reviews" \
        --jq "[.[] | select(.user.login == \"$COPILOT_BOT\")] | last" 2>&1)
    if [ $? -ne 0 ]; then
        echo "Error: Failed to fetch latest Copilot review for PR #$PR_NUMBER." >&2
        echo "$result" >&2
        return 1
    fi
    echo "$result"
}

# --- Check if Copilot is in Requested Reviewers ---
copilot_is_pending() {
    local result
    result=$(gh api \
        -H "Accept: application/vnd.github+json" \
        -H "X-GitHub-Api-Version: 2022-11-28" \
        "/repos/$REPO_NWO/pulls/$PR_NUMBER/requested_reviewers" \
        --jq ".users[] | select(.login == \"Copilot\" or .login == \"$COPILOT_BOT\") | .login" 2>&1)
    if [ $? -ne 0 ]; then
        echo "Error: Failed to query requested reviewers for PR #$PR_NUMBER." >&2
        echo "$result" >&2
        exit 1
    fi
    [ -n "$result" ]
}

# --- Main Logic ---
log "Waiting for Copilot review on PR #$PR_NUMBER ($REPO_NWO)..."
log "Timeout: ${TIMEOUT_MINUTES}m | Poll interval: ${POLL_INTERVAL}s"
log ""

# Record the initial number of Copilot reviews
INITIAL_COUNT=$(get_copilot_review_count)
if [ -z "$INITIAL_COUNT" ]; then
    echo "Error: Failed to query reviews for PR #$PR_NUMBER."
    exit 1
fi
log "Existing Copilot reviews: $INITIAL_COUNT"

# Check if Copilot has a pending review request
if ! copilot_is_pending; then
    # Copilot is not in the requested reviewers list. Check if a new review
    # appeared since we might have just missed it (race condition).
    CURRENT_COUNT=$(get_copilot_review_count)
    if [[ "$CURRENT_COUNT" =~ ^[0-9]+$ ]] && [ "$CURRENT_COUNT" -gt "$INITIAL_COUNT" ]; then
        log "Copilot review already completed!"
        log "Waiting 20s for line comments to propagate..."
        sleep 20
        REVIEW=$(get_latest_copilot_review)
        echo ""
        echo "=== Copilot Review ==="
        echo "$REVIEW" | jq -r '"State: \(.state)\nSubmitted: \(.submitted_at)\n\n\(.body)"' 2>/dev/null
        exit 0
    fi

    echo "Warning: Copilot is not in the requested reviewers list for PR #$PR_NUMBER."
    echo "Was a review requested? Use: ./scripts/request_copilot_review.sh $PR_NUMBER"
    exit 3
fi

log "Copilot review is pending. Polling every ${POLL_INTERVAL}s..."
log ""

# --- Poll Loop ---
ELAPSED=0
while [ "$ELAPSED" -lt "$TIMEOUT_SECONDS" ]; do
    sleep "$POLL_INTERVAL"
    ELAPSED=$((ELAPSED + POLL_INTERVAL))
    ELAPSED_MIN=$((ELAPSED / 60))

    # Check if Copilot is still pending
    if copilot_is_pending; then
        log "  Still waiting... (${ELAPSED_MIN}m elapsed)"
        continue
    fi

    # Copilot is no longer pending - verify a new review arrived
    CURRENT_COUNT=$(get_copilot_review_count)
    if [[ "$CURRENT_COUNT" =~ ^[0-9]+$ ]] && [ "$CURRENT_COUNT" -gt "$INITIAL_COUNT" ]; then
        log ""
        log "Copilot review received after ~${ELAPSED_MIN}m!"
        log "Waiting 20s for line comments to propagate..."
        sleep 20
        REVIEW=$(get_latest_copilot_review)
        echo ""
        echo "=== Copilot Review ==="
        echo "$REVIEW" | jq -r '"State: \(.state)\nSubmitted: \(.submitted_at)\n\n\(.body)"' 2>/dev/null
        exit 0
    else
        # Copilot was removed from requested reviewers but no new review appeared.
        # This can happen if the review request was dismissed or failed.
        echo ""
        echo "Warning: Copilot is no longer pending but no new review was found."
        echo "The review request may have been dismissed or failed."
        echo "Reviews before: $INITIAL_COUNT, Reviews now: $CURRENT_COUNT"
        exit 1
    fi
done

# --- Timeout ---
echo ""
echo "Timeout: Copilot review not received within ${TIMEOUT_MINUTES} minutes."
echo "The review may still be in progress. You can:"
echo "  - Run this script again to continue waiting"
echo "  - Check the PR manually: gh pr view $PR_NUMBER --web"
exit 2
