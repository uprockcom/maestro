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

# Test: structured JSON logging works.
# Usage: ./test-logs.sh <container>

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

section "Structured Logging"

# Trigger some log entries by running ask hook (quick unblock)
clean_question_state "$CONTAINER"
set_agent_state "$CONTAINER" "active"
cexec_bash "$CONTAINER" '
  rm -f '"$QUESTION_FILE"' '"$RESPONSE_FILE"'
  (sleep 1 && echo "x" > '"$RESPONSE_FILE"') &
  BG=$!
  echo "{\"tool_name\":\"AskUserQuestion\",\"tool_input\":{\"question\":\"Log test\"}}" | maestro-agent hook ask 2>/dev/null; true
  wait $BG 2>/dev/null
' &>/dev/null || true

# Check log file exists and has valid JSON
LOG_EXISTS=$(cexec_bash "$CONTAINER" "test -f $LOG_FILE && echo Y || echo N")
assert_eq "log file exists" "Y" "$LOG_EXISTS"

# Check entries are valid JSON with expected fields
LAST_LINES=$(cexec_bash "$CONTAINER" "tail -5 $LOG_FILE 2>/dev/null")
assert_contains "log has time field" '"time"' "$LAST_LINES"
assert_contains "log has level field" '"level"' "$LAST_LINES"
assert_contains "log has msg field" '"msg"' "$LAST_LINES"

# Check ask hook log entries specifically
ASK_LOGS=$(cexec_bash "$CONTAINER" "grep 'ask' $LOG_FILE | tail -3")
assert_contains "ask hook logged" "Ask hook" "$ASK_LOGS"
assert_contains "hook field present" '"hook":"ask"' "$ASK_LOGS"

# Cleanup
clean_question_state "$CONTAINER"
set_agent_state "$CONTAINER" "active"

print_summary
