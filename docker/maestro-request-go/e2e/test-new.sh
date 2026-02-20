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

# Tests for: maestro-request new
#
# IMPORTANT: All tests run with daemon DISABLED. Running "new" with a live
# daemon spawns an actual sibling container as a side effect.
source "$(dirname "$0")/lib.sh"

CONTAINER="${1:?Usage: $0 <container>}"

section "maestro-request new"

# Disable daemon for ALL new tests — running new with daemon spawns real containers
disable_daemon "$CONTAINER"

# Clean slate
clean_requests "$CONTAINER"

# --- Test: creates request file ---
OUTPUT=$(cexec "$CONTAINER" maestro-request new "test task alpha" 2>&1)
ID=$(latest_request_id "$CONTAINER")
assert_not_contains "new creates request file" "(none)" "$ID"

# --- Test: request file has action=new ---
ACTION=$(request_field "$CONTAINER" "$ID" "action")
assert_eq "action is 'new'" "new" "$ACTION"

# --- Test: request file has task field ---
TASK=$(request_field "$CONTAINER" "$ID" "task")
assert_eq "task matches input" "test task alpha" "$TASK"

# --- Test: request file has parent=hostname ---
PARENT=$(request_field "$CONTAINER" "$ID" "parent")
HOSTNAME=$(cexec "$CONTAINER" hostname)
assert_eq "parent is hostname" "$HOSTNAME" "$PARENT"

# --- Test: request file has status=pending ---
STATUS=$(request_field "$CONTAINER" "$ID" "status")
assert_eq "status is 'pending'" "pending" "$STATUS"

# --- Test: request file has requested_at timestamp ---
TS=$(request_field "$CONTAINER" "$ID" "requested_at")
assert_contains "requested_at has ISO format" "T" "$TS"

# --- Test: --branch flag sets branch field ---
clean_requests "$CONTAINER"
cexec "$CONTAINER" maestro-request new "branched task" --branch feat/test-branch 2>&1
BRANCH_ID=$(latest_request_id "$CONTAINER")
BRANCH=$(request_field "$CONTAINER" "$BRANCH_ID" "branch")
assert_eq "--branch sets branch field" "feat/test-branch" "$BRANCH"

# --- Test: daemon unavailable → exit 0 + queued msg ---
clean_requests "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request new "offline task" 2>&1)
EXIT_CODE=$?
assert_exit_code "daemon unavailable exits 0" 0 "$EXIT_CODE"
assert_contains "output mentions queued" "queued" "$OUTPUT"

# Restore daemon for subsequent suites
restore_daemon "$CONTAINER"

print_summary
