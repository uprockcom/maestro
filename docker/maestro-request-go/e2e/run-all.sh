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

# Run all automated maestro-request e2e tests against a container.
# Usage: ./run-all.sh <container>
#
# Prerequisites:
#   make all                            # Fresh binary + Docker image
#   maestro new -en "test request e2e"  # Create test container

set -uo pipefail
# NOTE: No set -e. Test suites use print_summary which returns $FAIL_COUNT;
# with set -e this would abort the runner after the first failed suite.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

echo -e "${BOLD}Running maestro-request e2e tests against: $CONTAINER${NC}"
echo ""

TOTAL_PASS=0
TOTAL_FAIL=0
SUITE_RESULTS=()

run_suite() {
  local name="$1" script="$2"
  PASS_COUNT=0
  FAIL_COUNT=0

  echo -e "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"

  source "$script" "$CONTAINER"

  TOTAL_PASS=$((TOTAL_PASS + PASS_COUNT))
  TOTAL_FAIL=$((TOTAL_FAIL + FAIL_COUNT))

  if [[ $FAIL_COUNT -eq 0 ]]; then
    SUITE_RESULTS+=("${GREEN}PASS${NC}  $name ($PASS_COUNT tests)")
  else
    SUITE_RESULTS+=("${RED}FAIL${NC}  $name ($FAIL_COUNT of $((PASS_COUNT + FAIL_COUNT)) failed)")
  fi
}

# Container-local suites first
run_suite "New Command"         "$SCRIPT_DIR/test-new.sh"
run_suite "Done Command"        "$SCRIPT_DIR/test-done.sh"
run_suite "Notify Command"      "$SCRIPT_DIR/test-notify.sh"
run_suite "Request Command"     "$SCRIPT_DIR/test-request.sh"
run_suite "Wait Script"         "$SCRIPT_DIR/test-wait-script.sh"
run_suite "Wait Message"        "$SCRIPT_DIR/test-wait-message.sh"
run_suite "Wait Request"        "$SCRIPT_DIR/test-wait-request.sh"
run_suite "Wait Composite"      "$SCRIPT_DIR/test-wait-composite.sh"

# Daemon-dependent suites
run_suite "Status Command"      "$SCRIPT_DIR/test-status.sh"
run_suite "Wait Daemon"         "$SCRIPT_DIR/test-wait-daemon.sh"
run_suite "Claude Commands"     "$SCRIPT_DIR/test-claude.sh"

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
else
  echo -e "${RED}${BOLD}$TOTAL_FAIL of $TOTAL tests failed.${NC}"
  exit 1
fi
