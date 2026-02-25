#!/bin/bash

# A script to add a line comment to a GitHub Pull Request using the line-based API.
# This version uses the 'line' parameter which is more reliable than 'position'.

# --- Usage ---
if [ "$#" -ne 4 ]; then
    cat <<'EOF'
A helper script to programmatically add a line comment to a GitHub Pull Request.
This version uses the GitHub API's 'line' parameter for more accurate placement.

--- WHEN TO USE ---
This is useful during automated or semi-automated code reviews when you need 
to comment on a specific line of code without using the GitHub web UI. 

--- USAGE ---
./add_line_comment.sh <PR_NUMBER> <FILE_PATH> <LINE_NUMBER> "<COMMENT_BODY>"

--- ARGUMENTS ---
  PR_NUMBER:      The number of the target pull request (e.g., 384).
  FILE_PATH:      The relative path to the file from the repository root 
                  (e.g., 'modules/app/src/main/kotlin/com/uprock/mining/AppModule.kt').
  LINE_NUMBER:    The line number in the NEW version of the file where the comment should be placed.
  COMMENT_BODY:   The text of the comment. Must be enclosed in double quotes.

--- HOW IT WORKS ---
1. It automatically determines the repository owner and name.
2. It finds the SHA of the current HEAD commit to anchor the comment.
3. It uses the 'line' parameter with 'side=RIGHT' to place the comment on the new file version.
4. It uses the `gh api` command to post the comment.

--- IMPROVEMENTS ---
* Uses 'line' instead of 'position' for more accurate placement
* Works with both new and modified files
* Comments are placed exactly on the specified line number

--- EXAMPLE ---
./add_line_comment.sh 384 "modules/engines/crawl/src/main/kotlin/com/uprock/crawl/engine/HiddenWebView.kt" 59 "This is a comment on line 59!"

EOF
    exit 1
fi

# --- Variables ---
PR_NUMBER="$1"
FILE_PATH="$2"
LINE_NUMBER="$3"
COMMENT_BODY="$4"
REPO_NWO=$(gh repo view --json owner,name -q '"\(.owner.login)/\(.name)"')

# --- Logic ---
echo "Fetching current commit SHA..."
COMMIT_SHA=$(git rev-parse HEAD)
if [ -z "$COMMIT_SHA" ]; then
    echo "Error: Could not get commit SHA. Are you in a git repository?"
    exit 1
fi
echo "Commit SHA: $COMMIT_SHA"

echo "Posting comment to PR #$PR_NUMBER on file $FILE_PATH at line $LINE_NUMBER..."

# Use the line-based API endpoint which is more accurate
gh api \
  --method POST \
  -H "Accept: application/vnd.github+json" \
  -H "X-GitHub-Api-Version: 2022-11-28" \
  "/repos/$REPO_NWO/pulls/$PR_NUMBER/comments" \
  -f body="$COMMENT_BODY" \
  -f commit_id="$COMMIT_SHA" \
  -f path="$FILE_PATH" \
  -F line="$LINE_NUMBER" \
  -f side="RIGHT"

if [ $? -eq 0 ]; then
    echo "Successfully posted comment."
else
    echo "Failed to post comment."
    exit 1
fi