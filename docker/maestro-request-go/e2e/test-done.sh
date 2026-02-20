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

# Tests for: maestro-request done
#
# IMPORTANT: All tests run with daemon DISABLED. Running "done" with a live
# daemon sends an actual exit signal that stops the container.
source "$(dirname "$0")/lib.sh"

CONTAINER="${1:?Usage: $0 <container>}"

section "maestro-request done"

# Disable daemon for ALL done tests — running done with daemon kills the container
disable_daemon "$CONTAINER"

# Clean slate
clean_requests "$CONTAINER"

# --- Test: creates request file ---
OUTPUT=$(cexec "$CONTAINER" maestro-request done 2>&1)
ID=$(latest_request_id "$CONTAINER")
assert_not_contains "done creates request file" "(none)" "$ID"

# --- Test: action is 'exit' (wire protocol) ---
ACTION=$(request_field "$CONTAINER" "$ID" "action")
assert_eq "action is 'exit' (wire protocol)" "exit" "$ACTION"

# --- Test: parent is hostname ---
PARENT=$(request_field "$CONTAINER" "$ID" "parent")
HOSTNAME=$(cexec "$CONTAINER" hostname)
assert_eq "parent is hostname" "$HOSTNAME" "$PARENT"

# --- Test: status is pending ---
STATUS=$(request_field "$CONTAINER" "$ID" "status")
assert_eq "status is 'pending'" "pending" "$STATUS"

# --- Test: daemon unavailable → exit 0 + queued msg ---
clean_requests "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request done 2>&1)
EXIT_CODE=$?
assert_exit_code "daemon unavailable exits 0" 0 "$EXIT_CODE"
assert_contains "output mentions queued" "queued" "$OUTPUT"

# Restore daemon for subsequent suites
restore_daemon "$CONTAINER"

print_summary
