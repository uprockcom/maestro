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

# Add Apache 2.0 license headers to all Go source files

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info() { echo -e "${BLUE}ℹ${NC} $1"; }
success() { echo -e "${GREEN}✓${NC} $1"; }
warning() { echo -e "${YELLOW}⚠${NC} $1"; }
error() { echo -e "${RED}✗${NC} $1"; }

# Apache 2.0 license header
YEAR=$(date +%Y)
LICENSE_HEADER="// Copyright $YEAR Christopher O'Connell
//
// Licensed under the Apache License, Version 2.0 (the \"License\");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an \"AS IS\" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License."

# Find all Go files (excluding vendor and generated files)
GO_FILES=$(find . -name "*.go" -not -path "./vendor/*" -not -path "./.git/*" -not -path "./dist/*" -not -name "*_gen.go")

FIXED=0
ALREADY_OK=0
UPDATED=0

info "Checking license headers in Go files..."
echo ""

for file in $GO_FILES; do
    # Check if file has Apache license header
    if head -n 15 "$file" | grep -q "Licensed under the Apache License"; then
        success "$file - Already has Apache 2.0 header"
        ((ALREADY_OK++))
    # Check if it has old "All rights reserved" header
    elif head -n 3 "$file" | grep -q "All rights reserved"; then
        warning "$file - Updating old copyright header"
        
        # Get the package line and everything after
        PACKAGE_LINE=$(grep -n "^package " "$file" | head -1 | cut -d: -f1)
        
        # Create temp file with new header + rest of file
        {
            echo "$LICENSE_HEADER"
            echo ""
            tail -n +$((PACKAGE_LINE)) "$file"
        } > "${file}.tmp"
        
        mv "${file}.tmp" "$file"
        ((UPDATED++))
    else
        warning "$file - Adding Apache 2.0 header"
        
        # Prepend license header
        {
            echo "$LICENSE_HEADER"
            echo ""
            cat "$file"
        } > "${file}.tmp"
        
        mv "${file}.tmp" "$file"
        ((FIXED++))
    fi
done

echo ""
echo "═══════════════════════════════════════"
success "License Header Check Complete!"
echo "═══════════════════════════════════════"
echo "  Already compliant: $ALREADY_OK"
echo "  Updated old header: $UPDATED"
echo "  Added new header:   $FIXED"
echo "  Total files:        $((ALREADY_OK + UPDATED + FIXED))"
echo ""

if [ $FIXED -gt 0 ] || [ $UPDATED -gt 0 ]; then
    info "Changes made. Please review and commit:"
    echo "  git diff"
    echo "  git add -u"
    echo "  git commit -m \"Add Apache 2.0 license headers to all source files\""
fi
