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

# Tests for: maestro-request notify
#
# All tests run with daemon DISABLED. Running notify with a live daemon sends
# an actual desktop notification (harmless but noisy).
source "$(dirname "$0")/lib.sh"

CONTAINER="${1:?Usage: $0 <container>}"

section "maestro-request notify"

# Disable daemon for all notify tests
disable_daemon "$CONTAINER"

# Clean slate
clean_requests "$CONTAINER"

# --- Test: creates request file with action=notify ---
OUTPUT=$(cexec "$CONTAINER" maestro-request notify "Test Title" "Test message body" 2>&1)
ID=$(latest_request_id "$CONTAINER")
ACTION=$(request_field "$CONTAINER" "$ID" "action")
assert_eq "action is 'notify'" "notify" "$ACTION"

# --- Test: title stored correctly ---
TITLE=$(request_field "$CONTAINER" "$ID" "title")
assert_eq "title matches input" "Test Title" "$TITLE"

# --- Test: message stored correctly ---
MSG=$(request_field "$CONTAINER" "$ID" "message")
assert_eq "message matches input" "Test message body" "$MSG"

# --- Test: title > 64 chars → exit 1 ---
LONG_TITLE=$(printf 'A%.0s' {1..65})
OUTPUT=$(cexec "$CONTAINER" maestro-request notify "$LONG_TITLE" "msg" 2>&1)
EXIT_CODE=$?
assert_exit_code "title > 64 chars exits 1" 1 "$EXIT_CODE"
assert_contains "error mentions title too long" "title too long" "$OUTPUT"

# --- Test: message > 256 chars → exit 1 ---
LONG_MSG=$(printf 'B%.0s' {1..257})
OUTPUT=$(cexec "$CONTAINER" maestro-request notify "Ok" "$LONG_MSG" 2>&1)
EXIT_CODE=$?
assert_exit_code "message > 256 chars exits 1" 1 "$EXIT_CODE"
assert_contains "error mentions message too long" "message too long" "$OUTPUT"

# --- Test: title exactly 64 chars → ok ---
clean_requests "$CONTAINER"
EXACT_TITLE=$(printf 'C%.0s' {1..64})
OUTPUT=$(cexec "$CONTAINER" maestro-request notify "$EXACT_TITLE" "ok" 2>&1)
EXIT_CODE=$?
BOUNDARY_ID=$(latest_request_id "$CONTAINER")
assert_not_contains "title=64 creates request" "(none)" "$BOUNDARY_ID"

# --- Test: message exactly 256 chars → ok ---
clean_requests "$CONTAINER"
EXACT_MSG=$(printf 'D%.0s' {1..256})
OUTPUT=$(cexec "$CONTAINER" maestro-request notify "Ok" "$EXACT_MSG" 2>&1)
BOUNDARY_ID2=$(latest_request_id "$CONTAINER")
assert_not_contains "message=256 creates request" "(none)" "$BOUNDARY_ID2"

# --- Test: daemon unavailable → exit 0 ---
clean_requests "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request notify "Offline" "Test" 2>&1)
EXIT_CODE=$?
assert_exit_code "daemon unavailable exits 0" 0 "$EXIT_CODE"

# Restore daemon for subsequent suites
restore_daemon "$CONTAINER"

print_summary
