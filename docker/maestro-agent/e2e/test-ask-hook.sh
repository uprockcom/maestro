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

# Test: ask hook — question extraction, blocking wait, response delivery.
# Usage: ./test-ask-hook.sh <container>
#
# NOTE: This test does NOT cover user-connection detection (requires human).
# See test-ask-connect.sh for that.
#
# The ask hook uses os.Exit(2) for response delivery, so we must capture
# exit codes inside the container shell to avoid propagating non-zero exits
# through docker exec (which would trigger set -e).

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

# Helper: run a blocking ask hook test inside the container.
# All output/exit handling happens inside the container shell to avoid
# SIGPIPE (head -1) and set -e issues on the host side.
ask_test() {
  local container="$1"; shift
  cexec_bash "$container" "$@" || true
}

section "Ask hook: question file extraction"

clean_question_state "$CONTAINER"
set_agent_state "$CONTAINER" "active"

# Launch ask hook; background writer captures question file content then unblocks.
QCONTENT=$(ask_test "$CONTAINER" '
  rm -f '"$QUESTION_FILE"' '"$RESPONSE_FILE"'
  (
    sleep 2
    cat '"$QUESTION_FILE"' 2>/dev/null
    echo "unblock" > '"$RESPONSE_FILE"'
  ) &
  BG=$!
  echo "{\"tool_name\":\"AskUserQuestion\",\"tool_input\":{\"question\":\"Pick?\",\"options\":[\"A\",\"B\"]}}" | maestro-agent hook ask 2>/dev/null; true
  wait $BG 2>/dev/null
' 2>&1)
# Extract first line (the cat output from background subshell)
QCONTENT=$(echo "$QCONTENT" | head -1)

assert_contains "question file has question field" '"question":"Pick?"' "$QCONTENT"
assert_contains "question file has options" '"options":["A","B"]' "$QCONTENT"
assert_not_contains "question file lacks tool_name" "tool_name" "$QCONTENT"

section "Ask hook: state during blocking wait"

clean_question_state "$CONTAINER"
set_agent_state "$CONTAINER" "active"

# Launch ask hook, check state mid-wait, then unblock
MID_OUTPUT=$(ask_test "$CONTAINER" '
  rm -f '"$QUESTION_FILE"' '"$RESPONSE_FILE"'
  (
    sleep 2
    STATE=$(cat '"$STATE_FILE"' 2>/dev/null)
    IDLE=$(test -f '"$IDLE_FLAG"' && echo Y || echo N)
    echo "state=$STATE idle=$IDLE"
    sleep 1
    echo "done" > '"$RESPONSE_FILE"'
  ) &
  BG=$!
  echo "{\"tool_name\":\"AskUserQuestion\",\"tool_input\":{\"question\":\"Wait test\"}}" | maestro-agent hook ask 2>/dev/null; true
  wait $BG 2>/dev/null
' 2>&1)
MID_STATE=$(echo "$MID_OUTPUT" | head -1)

assert_contains "state is question during wait" "state=question" "$MID_STATE"
assert_contains "idle flag exists during wait" "idle=Y" "$MID_STATE"

section "Ask hook: response delivery (exit 2 + stderr)"

clean_question_state "$CONTAINER"
set_agent_state "$CONTAINER" "active"

# Write response after delay; capture stderr and exit code inside container.
# The inner subshell captures the ask hook's stderr and exit code, then prints
# a structured result line so the host can assert on it.
RESULT=$(ask_test "$CONTAINER" '
  rm -f '"$QUESTION_FILE"' '"$RESPONSE_FILE"'
  (sleep 3 && echo "The answer is 42" > '"$RESPONSE_FILE"') &
  BG=$!
  STDERR=$(echo "{\"tool_name\":\"AskUserQuestion\",\"tool_input\":{\"question\":\"What?\"}}" | maestro-agent hook ask 2>&1 >/dev/null; true)
  echo "stderr=$STDERR"
  wait $BG 2>/dev/null
')

assert_contains "stderr has response" "The answer is 42" "$RESULT"

# Files should be cleaned up
assert_eq "question file cleaned up" "N" "$(has_question_file "$CONTAINER")"
assert_eq "response file cleaned up" "N" "$(has_response_file "$CONTAINER")"

section "Ask hook: stale response file is cleared"

# Pre-write a response file, then launch ask hook with a DIFFERENT delayed response.
# The pre-written file should be ignored (removed by hook before watchers start).
clean_question_state "$CONTAINER"
set_agent_state "$CONTAINER" "active"
cexec_bash "$CONTAINER" "echo 'stale answer' > $RESPONSE_FILE"

RESULT=$(ask_test "$CONTAINER" '
  (sleep 3 && echo "fresh answer" > '"$RESPONSE_FILE"') &
  BG=$!
  STDERR=$(echo "{\"tool_name\":\"AskUserQuestion\",\"tool_input\":{\"question\":\"Stale test\"}}" | maestro-agent hook ask 2>&1 >/dev/null; true)
  echo "stderr=$STDERR"
  wait $BG 2>/dev/null
')

assert_contains "receives fresh answer (not stale)" "fresh answer" "$RESULT"
assert_not_contains "does not receive stale answer" "stale answer" "$RESULT"

# Cleanup
clean_question_state "$CONTAINER"
set_agent_state "$CONTAINER" "active"

print_summary
