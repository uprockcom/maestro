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

# Tests for: maestro-request wait any / wait all
source "$(dirname "$0")/lib.sh"

CONTAINER="${1:?Usage: $0 <container>}"

section "maestro-request wait any / wait all"

# Clean slate
clean_requests "$CONTAINER"
clean_messages "$CONTAINER"

# --- wait any: first spec wins (message resolves immediately) ---
write_message "$CONTAINER" "any001" "quick message"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait any "script sleep 30" "message" --timeout 10 2>&1)
EXIT_CODE=$?
assert_exit_code "wait any: first spec wins exits 0" 0 "$EXIT_CODE"

# --- wait any: output has matched_index ---
MATCHED_TYPE=$(echo "$OUTPUT" | cjq "$CONTAINER" -r '.matched_type' 2>/dev/null || echo "(parse error)")
assert_eq "wait any: matched_type is 'message'" "message" "$MATCHED_TYPE"

# --- wait any: others shown as canceled ---
OTHERS_STATUS=$(echo "$OUTPUT" | cjq "$CONTAINER" -r '.others[0].status' 2>/dev/null || echo "(parse error)")
assert_eq "wait any: other spec is canceled" "canceled" "$OTHERS_STATUS"

# --- wait any: timeout → exit 2 ---
clean_messages "$CONTAINER"
clean_requests "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait any "message" "script sleep 30" --timeout 2 2>&1)
EXIT_CODE=$?
assert_exit_code "wait any: timeout exits 2" 2 "$EXIT_CODE"

# --- wait all: both succeed ---
clean_messages "$CONTAINER"
write_message "$CONTAINER" "all001" "msg for all"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait all "script echo done" "message" --timeout 10 2>&1)
EXIT_CODE=$?
assert_exit_code "wait all: both succeed exits 0" 0 "$EXIT_CODE"

# --- wait all: output has results array ---
RESULTS_COUNT=$(echo "$OUTPUT" | cjq "$CONTAINER" '.results | length' 2>/dev/null || echo "0")
assert_eq "wait all: results has 2 entries" "2" "$RESULTS_COUNT"

# --- wait all: one fails → exit 1 ---
clean_messages "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait all "script false" "script echo ok" --timeout 10 2>&1)
EXIT_CODE=$?
assert_exit_code "wait all: one fails exits 1" 1 "$EXIT_CODE"

# --- wait all: timeout → exit 2 ---
clean_messages "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait all "script echo fast" "message" --timeout 2 2>&1)
EXIT_CODE=$?
assert_exit_code "wait all: timeout exits 2" 2 "$EXIT_CODE"

print_summary
