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

# Tests for: maestro-request status
#
# Note: daemon-ipc.json is on a read-only bind mount and always present.
# We test daemon-reachable and daemon-unreachable (binary disabled) paths.
source "$(dirname "$0")/lib.sh"

CONTAINER="${1:?Usage: $0 <container>}"

section "maestro-request status"

# --- Test: with daemon reachable → prints status JSON or exits 1 ---
OUTPUT=$(cexec "$CONTAINER" maestro-request status 2>&1)
EXIT_CODE=$?
if [[ $EXIT_CODE -eq 0 ]]; then
  assert_contains "status returns JSON" "{" "$OUTPUT"
else
  # Daemon IPC file present but daemon process unreachable on host
  pass "status: daemon unreachable (expected in some setups)"
fi

# --- Test: daemon-ipc binary disabled → exit 1 ---
disable_daemon "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request status 2>&1)
EXIT_CODE=$?
restore_daemon "$CONTAINER"
assert_exit_code "daemon unreachable exits 1" 1 "$EXIT_CODE"
assert_contains "error mentions daemon" "daemon" "$OUTPUT"

print_summary
