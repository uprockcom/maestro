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

# Tests for: maestro-request claude (read/send/answer)
#
# All tests run with daemon DISABLED. With a live daemon, these commands
# would attempt to interact with nonexistent child containers and hang
# in polling loops. The request file is created BEFORE the daemon check,
# so we can verify file creation with daemon disabled.
source "$(dirname "$0")/lib.sh"

CONTAINER="${1:?Usage: $0 <container>}"

section "maestro-request claude commands"

# Disable daemon for all claude tests
disable_daemon "$CONTAINER"

# Clean slate
clean_requests "$CONTAINER"

# --- Test: claude answer with no flags → exit 1 ---
OUTPUT=$(cexec "$CONTAINER" maestro-request claude answer fake-id 2>&1)
EXIT_CODE=$?
assert_exit_code "answer no flags exits 1" 1 "$EXIT_CODE"
assert_contains "error mentions --select or --text" "select" "$OUTPUT"

# --- Test: claude answer --select creates request file ---
clean_requests "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request claude answer fake-req-id --select "Option A" 2>&1)
ID=$(latest_request_id "$CONTAINER")
if [[ "$ID" != "(none)" ]]; then
  ACTION=$(request_field "$CONTAINER" "$ID" "action")
  assert_eq "answer --select: action is 'answer_question'" "answer_question" "$ACTION"
else
  fail "answer --select: no request file created"
fi

# --- Test: claude answer --text creates request file ---
clean_requests "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request claude answer fake-req-id --text "custom answer" 2>&1)
ID=$(latest_request_id "$CONTAINER")
if [[ "$ID" != "(none)" ]]; then
  MSG=$(request_field "$CONTAINER" "$ID" "message")
  assert_eq "answer --text: message is 'custom answer'" "custom answer" "$MSG"
else
  fail "answer --text: no request file created"
fi

# --- Test: claude read creates request file ---
clean_requests "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request claude read fake-req-id 2>&1)
ID=$(latest_request_id "$CONTAINER")
if [[ "$ID" != "(none)" ]]; then
  ACTION=$(request_field "$CONTAINER" "$ID" "action")
  assert_eq "read: action is 'read_messages'" "read_messages" "$ACTION"
else
  fail "read: no request file created"
fi

# --- Test: claude send joins args ---
clean_requests "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request claude send fake-req-id hello world 2>&1)
ID=$(latest_request_id "$CONTAINER")
if [[ "$ID" != "(none)" ]]; then
  MSG=$(request_field "$CONTAINER" "$ID" "message")
  assert_eq "send: message is 'hello world'" "hello world" "$MSG"
else
  fail "send: no request file created"
fi

# --- Test: daemon unavailable → exit 1 for all claude cmds ---
clean_requests "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request claude read fake-id 2>&1)
EXIT_CODE=$?
assert_exit_code "read: daemon unavailable exits 1" 1 "$EXIT_CODE"

OUTPUT=$(cexec "$CONTAINER" maestro-request claude send fake-id hello 2>&1)
EXIT_CODE=$?
assert_exit_code "send: daemon unavailable exits 1" 1 "$EXIT_CODE"

OUTPUT=$(cexec "$CONTAINER" maestro-request claude answer fake-id --select "A" 2>&1)
EXIT_CODE=$?
assert_exit_code "answer: daemon unavailable exits 1" 1 "$EXIT_CODE"

# Restore daemon for subsequent suites
restore_daemon "$CONTAINER"

print_summary
