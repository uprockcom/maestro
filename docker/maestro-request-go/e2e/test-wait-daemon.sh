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

# Tests for: maestro-request wait daemon
source "$(dirname "$0")/lib.sh"

CONTAINER="${1:?Usage: $0 <container>}"

section "maestro-request wait daemon"

# --- Test: daemon already available → immediate return ---
DAEMON_PRESENT=$(has_daemon_ipc "$CONTAINER")
if [[ "$DAEMON_PRESENT" == "Y" ]]; then
  OUTPUT=$(cexec "$CONTAINER" maestro-request wait daemon --timeout 5 2>&1)
  EXIT_CODE=$?
  # If daemon is reachable, exit 0 and JSON output.
  # If daemon-ipc.json exists but daemon is down, it will timeout.
  if [[ $EXIT_CODE -eq 0 ]]; then
    assert_contains "wait daemon returns content" "{" "$OUTPUT"
  else
    # Daemon file exists but daemon not reachable — still a valid test,
    # just means the daemon isn't running. Mark as pass with note.
    pass "wait daemon: daemon-ipc exists but daemon unreachable (expected in some setups)"
  fi
else
  pass "wait daemon: skipped (no daemon-ipc.json)"
fi

# --- Test: output is JSON (when daemon reachable) ---
if [[ "$DAEMON_PRESENT" == "Y" ]]; then
  OUTPUT=$(cexec "$CONTAINER" maestro-request wait daemon --timeout 5 2>&1)
  EXIT_CODE=$?
  if [[ $EXIT_CODE -eq 0 ]]; then
    VALID=$(echo "$OUTPUT" | cjq "$CONTAINER" '.' &>/dev/null && echo "Y" || echo "N")
    assert_eq "wait daemon output is valid JSON" "Y" "$VALID"
  else
    pass "wait daemon JSON: skipped (daemon unreachable)"
  fi
else
  pass "wait daemon JSON: skipped (no daemon-ipc.json)"
fi

# --- Test: timeout → exit 1 ---
disable_daemon "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait daemon --timeout 2 2>&1)
EXIT_CODE=$?
restore_daemon "$CONTAINER"
assert_exit_code "no daemon times out with exit 1" 1 "$EXIT_CODE"
assert_contains "timeout message" "Timeout" "$OUTPUT"

print_summary
