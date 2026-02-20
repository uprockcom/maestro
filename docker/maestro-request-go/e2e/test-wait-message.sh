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

# Tests for: maestro-request wait message
source "$(dirname "$0")/lib.sh"

CONTAINER="${1:?Usage: $0 <container>}"

section "maestro-request wait message"

# Clean slate
clean_messages "$CONTAINER"

# --- Test: pre-existing message → immediate return ---
write_message "$CONTAINER" "msg001" "hello from host"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait message --timeout 5 2>&1)
EXIT_CODE=$?
assert_exit_code "pre-existing message exits 0" 0 "$EXIT_CODE"
assert_contains "message content returned" "hello from host" "$OUTPUT"

# --- Test: multiple messages consumed ---
clean_messages "$CONTAINER"
write_message "$CONTAINER" "msg-a" "first"
write_message "$CONTAINER" "msg-b" "second"
write_message "$CONTAINER" "msg-c" "third"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait message --timeout 5 2>&1)
EXIT_CODE=$?
assert_exit_code "multiple messages exits 0" 0 "$EXIT_CODE"
# Check count in JSON
COUNT=$(echo "$OUTPUT" | cjq "$CONTAINER" -r '.count' 2>/dev/null || echo "0")
assert_eq "count is 3" "3" "$COUNT"

# --- Test: message files deleted after consumption ---
REMAINING=$(count_messages "$CONTAINER")
assert_eq "message files deleted" "0" "$REMAINING"

# --- Test: messages sorted by filename ---
clean_messages "$CONTAINER"
write_message "$CONTAINER" "zzz" "last-alpha"
write_message "$CONTAINER" "aaa" "first-alpha"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait message --timeout 5 2>&1)
# First message in the array should be "first-alpha" (aaa.txt sorts first)
FIRST=$(echo "$OUTPUT" | cjq "$CONTAINER" -r '.messages[0]' 2>/dev/null || echo "(parse error)")
assert_eq "sorted by filename (aaa first)" "first-alpha" "$FIRST"

# --- Test: timeout → exit code 2 ---
clean_messages "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait message --timeout 2 2>&1)
EXIT_CODE=$?
assert_exit_code "timeout exits 2" 2 "$EXIT_CODE"

# --- Test: empty dir → waits then times out ---
clean_messages "$CONTAINER"
cexec_bash "$CONTAINER" "mkdir -p $PENDING_MESSAGES_DIR"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait message --timeout 2 2>&1)
EXIT_CODE=$?
assert_exit_code "empty dir times out with exit 2" 2 "$EXIT_CODE"

# --- Test: trailing newlines trimmed ---
clean_messages "$CONTAINER"
# Write a message with explicit trailing newlines
cexec_bash "$CONTAINER" "mkdir -p $PENDING_MESSAGES_DIR && printf 'trimmed\n\n\n' > $PENDING_MESSAGES_DIR/trim.txt"
OUTPUT=$(cexec "$CONTAINER" maestro-request wait message --timeout 5 2>&1)
CONTENT=$(echo "$OUTPUT" | cjq "$CONTAINER" -r '.messages[0]' 2>/dev/null || echo "(parse error)")
assert_eq "trailing newlines trimmed" "trimmed" "$CONTENT"

print_summary
