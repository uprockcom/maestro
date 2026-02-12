#!/bin/bash
#
# Copyright 2025 Christopher O'Connell
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# merge-from-public.sh - Pull changes from public repo PRs into private repo
#
# This script handles merging changes from public/main into origin/main.
# Since these branches have unrelated histories, we can't use git merge.
# Instead, we compare file contents and apply changes from public.
#
# The script detects three cases:
#   1. New files in public - safe to add
#   2. Files where only public changed - safe to update
#   3. Files where BOTH public and origin changed - needs manual 3-way merge
#
# For case 3, we save both versions (.origin and .public) for manual merging.
#
# Usage: ./scripts/merge-from-public.sh [--dry-run]
#
# Options:
#   --dry-run    Show what would change without modifying anything

set -e  # Exit on error

DRY_RUN=false
if [ "$1" = "--dry-run" ]; then
    DRY_RUN=true
    echo "🔍 DRY RUN MODE - no changes will be made"
    echo ""
fi

# Color codes for output (only if terminal supports it)
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    NC='\033[0m' # No Color
else
    RED=''
    GREEN=''
    YELLOW=''
    NC=''
fi

# Files/directories that only exist in private repo (from publish script)
PRIVATE_ONLY=(
    # Internal documentation
    "CLAUDE.md"
    "RELEASE_PLAN.md"
    "PUBLISH_NOTES.md"
    "DISTRIBUTION.md"
    "IDEAS.md"
    "PLAN_FOR_RELEASE.md"
    "TEST_PLAN.md"
    "docs/SIGNING_SETUP.md"
    "docs/TOKEN_REFRESH_NOTES.md"

    # Internal scripts
    "scripts/publish-to-public.sh"
    "scripts/merge-from-public.sh"
    "scripts/migrate-configs.sh"
    "scripts/cleanup-bad-migration.sh"
    "scripts/release-preflight.sh"
    "scripts/release.sh"

    # Credentials (should be gitignored but extra safety)
    ".env.signing"
)

# Check if a path matches any private-only pattern
is_private_only() {
    local path="$1"
    for private in "${PRIVATE_ONLY[@]}"; do
        if [ "$path" = "$private" ]; then
            return 0
        fi
    done
    return 1
}

# Check if origin has unique changes that would be lost by overwriting with public
# Returns 0 if origin has unique content, 1 if safe to overwrite
origin_has_unique_changes() {
    local file="$1"
    local origin_content public_content

    origin_content=$(git show "origin/main:$file" 2>/dev/null) || return 1
    public_content=$(git show "public/main:$file" 2>/dev/null) || return 1

    # Count unique lines in origin that aren't in public
    # This detects if origin has additions that public lacks
    local origin_unique_lines
    origin_unique_lines=$(diff <(echo "$public_content") <(echo "$origin_content") 2>/dev/null | grep "^>" | wc -l)

    # If origin has more than 5 unique lines, it likely has meaningful changes
    # (allowing small differences for formatting/whitespace)
    if [ "$origin_unique_lines" -gt 5 ]; then
        return 0
    fi
    return 1
}

echo "📥 Preparing to merge changes from public/main..."

# Ensure we're on main
if [ "$(git branch --show-current)" != "main" ]; then
    echo "❌ Error: Must be on main branch"
    echo "Run: git checkout main"
    exit 1
fi

# Check for uncommitted changes
if ! git diff --quiet || ! git diff --cached --quiet; then
    echo "❌ Error: You have uncommitted changes"
    echo "Please commit or stash them first"
    exit 1
fi

# Check for unpushed commits
echo "🔍 Checking for unpushed commits..."
git fetch origin || exit 1
UNPUSHED_COUNT=$(git rev-list origin/main..main --count)
if [ "$UNPUSHED_COUNT" -gt 0 ]; then
    echo "❌ Error: Local main has $UNPUSHED_COUNT unpushed commit(s)!"
    echo "Please push to origin first: git push origin main"
    exit 1
fi

# Fetch latest from public
echo "🔄 Fetching from public remote..."
git fetch public || exit 1

# Create temp directory for comparison
TEMP_DIR=$(mktemp -d)
trap "rm -rf $TEMP_DIR" EXIT

echo "📦 Extracting public/main for comparison..."

# Get list of all files in public/main
PUBLIC_FILES=$(git ls-tree -r --name-only public/main)

# Track changes by category
NEW_FILES=()       # Files that only exist in public (new from PR)
SAFE_UPDATES=()    # Files where public changed but origin has no unique content
NEEDS_MERGE=()     # Files where BOTH public and origin have unique changes

echo "🔍 Analyzing differences..."
echo ""

for file in $PUBLIC_FILES; do
    # Skip private-only files (shouldn't exist in public, but safety check)
    if is_private_only "$file"; then
        continue
    fi

    # Get file content from public/main
    PUBLIC_CONTENT=$(git show "public/main:$file" 2>/dev/null) || continue

    # Check if file exists in origin/main
    if git show "origin/main:$file" > /dev/null 2>&1; then
        # File exists in both - check if different
        ORIGIN_CONTENT=$(git show "origin/main:$file")

        if [ "$PUBLIC_CONTENT" != "$ORIGIN_CONTENT" ]; then
            # Files differ - check if origin has unique changes we'd lose
            if origin_has_unique_changes "$file"; then
                # Both sides have changes - needs manual merge
                NEEDS_MERGE+=("$file")
            else
                # Only public has meaningful changes - safe to update
                SAFE_UPDATES+=("$file")
            fi
        fi
    else
        # New file in public
        NEW_FILES+=("$file")
    fi
done

# Summary
echo "═══════════════════════════════════════════════════════════════"
echo "                        CHANGE SUMMARY"
echo "═══════════════════════════════════════════════════════════════"
echo ""

if [ ${#NEW_FILES[@]} -eq 0 ] && [ ${#SAFE_UPDATES[@]} -eq 0 ] && [ ${#NEEDS_MERGE[@]} -eq 0 ]; then
    echo "✅ No changes to merge - public/main matches origin/main"
    exit 0
fi

if [ ${#NEW_FILES[@]} -gt 0 ]; then
    echo -e "${GREEN}📄 New files from PR (${#NEW_FILES[@]}):${NC}"
    echo "   These are safe to add directly."
    for file in "${NEW_FILES[@]}"; do
        echo "   + $file"
    done
    echo ""
fi

if [ ${#SAFE_UPDATES[@]} -gt 0 ]; then
    echo -e "${GREEN}📝 Safe updates (${#SAFE_UPDATES[@]}):${NC}"
    echo "   Public changed these files, origin has no unique content to preserve."
    for file in "${SAFE_UPDATES[@]}"; do
        echo "   ~ $file"
    done
    echo ""
fi

if [ ${#NEEDS_MERGE[@]} -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Needs manual merge (${#NEEDS_MERGE[@]}):${NC}"
    echo "   BOTH public AND origin have unique changes to these files!"
    echo "   These require 3-way merge to preserve both sets of changes."
    for file in "${NEEDS_MERGE[@]}"; do
        echo "   ! $file"
    done
    echo ""
fi

echo "═══════════════════════════════════════════════════════════════"
echo ""

if [ "$DRY_RUN" = true ]; then
    echo "🔍 Dry run complete. Run without --dry-run to apply changes."
    if [ ${#NEEDS_MERGE[@]} -gt 0 ]; then
        echo ""
        echo -e "${YELLOW}⚠️  WARNING: ${#NEEDS_MERGE[@]} file(s) will need manual 3-way merge!${NC}"
        echo "   Run without --dry-run to see detailed diffs and get merge files."
    fi
    exit 0
fi

# Ask for confirmation
echo "Do you want to view detailed diffs before applying? (y/N)"
read -r VIEW_DIFFS

if [ "$VIEW_DIFFS" = "y" ] || [ "$VIEW_DIFFS" = "Y" ]; then
    for file in "${SAFE_UPDATES[@]}"; do
        echo ""
        echo "═══════════════════════════════════════════════════════════════"
        echo -e "${GREEN}SAFE UPDATE: $file${NC}"
        echo "═══════════════════════════════════════════════════════════════"
        # Show diff between origin and public versions
        diff -u <(git show "origin/main:$file") <(git show "public/main:$file") || true
    done

    for file in "${NEEDS_MERGE[@]}"; do
        echo ""
        echo "═══════════════════════════════════════════════════════════════"
        echo -e "${YELLOW}NEEDS MERGE: $file${NC}"
        echo "═══════════════════════════════════════════════════════════════"
        echo "--- Changes from PR (public has, origin lacks):"
        diff <(git show "origin/main:$file") <(git show "public/main:$file") 2>/dev/null | grep "^>" | head -20 || true
        echo ""
        echo "--- Changes in origin (origin has, public lacks):"
        diff <(git show "public/main:$file") <(git show "origin/main:$file") 2>/dev/null | grep "^>" | head -20 || true
    done

    for file in "${NEW_FILES[@]}"; do
        echo ""
        echo "═══════════════════════════════════════════════════════════════"
        echo -e "${GREEN}NEW FILE: $file${NC}"
        echo "═══════════════════════════════════════════════════════════════"
        git show "public/main:$file" | head -50
        LINES=$(git show "public/main:$file" | wc -l)
        if [ "$LINES" -gt 50 ]; then
            echo "... ($LINES total lines)"
        fi
    done
    echo ""
fi

if [ ${#NEEDS_MERGE[@]} -gt 0 ]; then
    echo -e "${YELLOW}⚠️  WARNING: ${#NEEDS_MERGE[@]} file(s) need manual merge!${NC}"
    echo "   Both .origin and .public versions will be saved for you to merge."
    echo ""
fi

echo "Apply changes to your working directory? (y/N)"
read -r APPLY

if [ "$APPLY" != "y" ] && [ "$APPLY" != "Y" ]; then
    echo "❌ Cancelled"
    exit 1
fi

# Apply changes
echo ""
echo "📝 Applying changes..."

# 1. New files - safe to add directly
for file in "${NEW_FILES[@]}"; do
    mkdir -p "$(dirname "$file")"
    git show "public/main:$file" > "$file"
    git add "$file"
    echo -e "   ${GREEN}+ Added: $file${NC}"
done

# 2. Safe updates - can overwrite origin version
for file in "${SAFE_UPDATES[@]}"; do
    git show "public/main:$file" > "$file"
    git add "$file"
    echo -e "   ${GREEN}~ Updated: $file${NC}"
done

# 3. Needs merge - save both versions for manual merging
if [ ${#NEEDS_MERGE[@]} -gt 0 ]; then
    echo ""
    echo -e "${YELLOW}⚠️  Files requiring manual 3-way merge:${NC}"
    for file in "${NEEDS_MERGE[@]}"; do
        # Save origin version
        git show "origin/main:$file" > "${file}.origin"
        # Save public version
        git show "public/main:$file" > "${file}.public"
        echo -e "   ${YELLOW}! $file${NC}"
        echo "     → Saved: ${file}.origin (your private changes)"
        echo "     → Saved: ${file}.public (PR changes)"
    done
    echo ""
    echo "   To merge each file:"
    echo "   1. Compare the .origin and .public versions"
    echo "   2. Manually combine changes into the main file"
    echo "   3. Run: git add <file>"
    echo "   4. Delete the .origin and .public files"
fi

echo ""
echo "═══════════════════════════════════════════════════════════════"

if [ ${#NEEDS_MERGE[@]} -gt 0 ]; then
    echo -e "${YELLOW}⚠️  PARTIAL APPLY: ${#NEW_FILES[@]} new + ${#SAFE_UPDATES[@]} updated, ${#NEEDS_MERGE[@]} need merge${NC}"
    echo ""
    echo "Next steps:"
    echo "  1. Manually merge the files marked above"
    echo "  2. Stage merged files: git add <file>"
    echo "  3. Review all changes: git diff --cached"
    echo "  4. Commit: git commit -m 'Merge PR changes from public repo'"
    echo "  5. Push to origin: git push origin main"
    echo "  6. Clean up: rm -f *.origin *.public"
else
    echo -e "${GREEN}✅ All changes applied and staged${NC}"
    echo ""
    echo "Next steps:"
    echo "  1. Review staged changes: git diff --cached"
    echo "  2. Commit: git commit -m 'Merge PR changes from public repo'"
    echo "  3. Push to origin: git push origin main"
fi
echo "═══════════════════════════════════════════════════════════════"
