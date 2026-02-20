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

# Test: ask hook detects user connection and passes through.
# Usage: ./test-ask-connect.sh <container>
#
# INTERACTIVE — requires a human to connect and disconnect.
#
# Flow:
#   1. Script launches ask hook in blocking wait
#   2. Script prints ">>> CONNECT NOW" prompt
#   3. Human runs: maestro connect <container>
#   4. Hook detects connection, exits 0
#   5. Human disconnects (Ctrl-B, D)
#   6. Script checks logs for "connected" trigger

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

section "Ask hook: user connection detection (INTERACTIVE)"

clean_question_state "$CONTAINER"
set_agent_state "$CONTAINER" "active"

echo ""
echo -e "  ${YELLOW}Starting ask hook in blocking wait...${NC}"

# Launch in background
cexec_bash "$CONTAINER" '
  rm -f '"$QUESTION_FILE"' '"$RESPONSE_FILE"'
  echo "{\"tool_name\":\"AskUserQuestion\",\"tool_input\":{\"question\":\"Interactive test\"}}" | timeout 120 maestro-agent hook ask 2>/dev/null
  echo "hook_exit=$?"
' &>/tmp/maestro-e2e-ask-connect.out &
BG_PID=$!

# Wait for hook to enter blocking state
sleep 3
assert_eq "state is question" "question" "$(read_state "$CONTAINER")"

echo ""
echo -e "  ${BOLD}${YELLOW}>>> CONNECT NOW: maestro connect $(echo "$CONTAINER" | sed 's/^maestro-//')${NC}"
echo -e "  ${YELLOW}    Then disconnect with Ctrl-B, D${NC}"
echo ""

# Wait for the background hook to exit
wait $BG_PID 2>/dev/null || true

# Check logs
LOGS=$(cexec_bash "$CONTAINER" "grep 'ask' $LOG_FILE 2>/dev/null | tail -5")
assert_contains "log shows connected trigger" "connected" "$LOGS"
assert_contains "log shows passthrough" "passing through" "$LOGS"

# Check the hook output
HOOK_OUT=$(cat /tmp/maestro-e2e-ask-connect.out 2>/dev/null || echo "")
assert_contains "hook exited with code 0" "hook_exit=0" "$HOOK_OUT"

# Cleanup
clean_question_state "$CONTAINER"
set_agent_state "$CONTAINER" "active"
rm -f /tmp/maestro-e2e-ask-connect.out

print_summary
