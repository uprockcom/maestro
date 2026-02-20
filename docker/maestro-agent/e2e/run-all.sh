#!/usr/bin/env bash
#
# Copyright 2026 Christopher O'Connell
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

# Run all automated maestro-agent e2e tests against a container.
# Usage: ./run-all.sh <container>
#
# Runs all non-interactive tests. For the interactive user-connection test,
# run test-ask-connect.sh separately.
#
# Prerequisites:
#   make all    # Fresh binary + Docker image
#   maestro new -en "test hook e2e"   # Create test container

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

echo -e "${BOLD}Running maestro-agent e2e tests against: $CONTAINER${NC}"
echo ""

TOTAL_PASS=0
TOTAL_FAIL=0
SUITE_RESULTS=()

run_suite() {
  local name="$1" script="$2"
  # Reset counters (they're global in lib.sh)
  PASS_COUNT=0
  FAIL_COUNT=0

  echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

  # Source the script to run in our process (shares lib.sh globals)
  source "$script" "$CONTAINER"

  TOTAL_PASS=$((TOTAL_PASS + PASS_COUNT))
  TOTAL_FAIL=$((TOTAL_FAIL + FAIL_COUNT))

  if [[ $FAIL_COUNT -eq 0 ]]; then
    SUITE_RESULTS+=("${GREEN}PASS${NC}  $name ($PASS_COUNT tests)")
  else
    SUITE_RESULTS+=("${RED}FAIL${NC}  $name ($FAIL_COUNT of $((PASS_COUNT + FAIL_COUNT)) failed)")
  fi
}

run_suite "Image Contents"    "$SCRIPT_DIR/test-image.sh"
run_suite "Claude Process"    "$SCRIPT_DIR/test-claude.sh"
run_suite "Settings.json"     "$SCRIPT_DIR/test-settings.sh"
run_suite "Pre-Tool-Use"      "$SCRIPT_DIR/test-pre-tool-use.sh"
run_suite "Post-Tool-Use"     "$SCRIPT_DIR/test-post-tool-use.sh"
run_suite "Ask Hook"          "$SCRIPT_DIR/test-ask-hook.sh"
run_suite "Host Display"      "$SCRIPT_DIR/test-host-display.sh"
run_suite "Structured Logs"   "$SCRIPT_DIR/test-logs.sh"

# Final summary
echo ""
echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${BOLD}SUITE SUMMARY${NC}"
echo ""
for result in "${SUITE_RESULTS[@]}"; do
  echo -e "  $result"
done
echo ""

TOTAL=$((TOTAL_PASS + TOTAL_FAIL))
if [[ $TOTAL_FAIL -eq 0 ]]; then
  echo -e "${GREEN}${BOLD}All $TOTAL tests passed across ${#SUITE_RESULTS[@]} suites.${NC}"
  echo ""
  echo -e "${YELLOW}Note: Interactive test (user connection) not included.${NC}"
  echo -e "${YELLOW}Run separately: $SCRIPT_DIR/test-ask-connect.sh $CONTAINER${NC}"
else
  echo -e "${RED}${BOLD}$TOTAL_FAIL of $TOTAL tests failed.${NC}"
  exit 1
fi
