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

# Tests for: maestro-request wait script
source "$(dirname "$0")/lib.sh"

CONTAINER="${1:?Usage: $0 <container>}"

section "maestro-request wait script"

# --- Test: successful command → exit 0 ---
OUTPUT=$(cexec "$CONTAINER" maestro-request wait script echo hello 2>&1)
EXIT_CODE=$?
assert_exit_code "echo exits 0" 0 "$EXIT_CODE"

# --- Test: captures stdout ---
assert_contains "stdout captured" "hello" "$OUTPUT"

# --- Test: failing command → exit 1 ---
OUTPUT=$(cexec "$CONTAINER" maestro-request wait script false 2>&1)
EXIT_CODE=$?
assert_exit_code "false exits 1" 1 "$EXIT_CODE"

# --- Test: timeout kills process → exit 1 ---
OUTPUT=$(cexec "$CONTAINER" maestro-request wait script sleep 30 --timeout 2 2>&1)
EXIT_CODE=$?
assert_exit_code "timeout exits 1" 1 "$EXIT_CODE"
assert_contains "timeout message" "Timeout" "$OUTPUT"

# --- Test: --timeout 0 means no timeout (infinite) ---
OUTPUT=$(cexec "$CONTAINER" maestro-request wait script echo ok --timeout 0 2>&1)
EXIT_CODE=$?
assert_exit_code "timeout=0 exits 0" 0 "$EXIT_CODE"
assert_contains "timeout=0 runs normally" "ok" "$OUTPUT"

# --- Test: multi-word command ---
OUTPUT=$(cexec "$CONTAINER" maestro-request wait script bash -c "echo hello world" 2>&1)
EXIT_CODE=$?
assert_exit_code "multi-word exits 0" 0 "$EXIT_CODE"
assert_contains "multi-word output" "hello world" "$OUTPUT"

# --- Test: exit code preserved ---
OUTPUT=$(cexec "$CONTAINER" maestro-request wait script bash -c "exit 42" 2>&1)
EXIT_CODE=$?
assert_exit_code "exit 42 preserved" 42 "$EXIT_CODE"

# --- Test: no command → error ---
OUTPUT=$(cexec "$CONTAINER" maestro-request wait script 2>&1)
EXIT_CODE=$?
assert_exit_code "no command exits 1" 1 "$EXIT_CODE"

print_summary
