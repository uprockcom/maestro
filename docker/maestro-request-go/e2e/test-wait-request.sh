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

# Tests for: maestro-request wait request
# Uses synthetic request files — no daemon needed.
source "$(dirname "$0")/lib.sh"

CONTAINER="${1:?Usage: $0 <container>}"

section "maestro-request wait request"

# Clean slate
clean_requests "$CONTAINER"

# --- Test: already fulfilled → immediate return ---
write_request_file "$CONTAINER" "test-ful-001" '{"id":"test-ful-001","action":"new","parent":"test","status":"fulfilled","requested_at":"2025-01-01T00:00:00Z","child_container":null,"fulfilled_at":"2025-01-01T00:01:00Z","error":null}'
OUTPUT=$(cexec "$CONTAINER" maestro-request wait request test-ful-001 fulfilled --timeout 5 2>&1)
EXIT_CODE=$?
assert_exit_code "already fulfilled exits 0" 0 "$EXIT_CODE"
assert_contains "output is JSON with id" "test-ful-001" "$OUTPUT"

# --- Test: polls until fulfilled (background writer updates after 3s) ---
clean_requests "$CONTAINER"
write_request_file "$CONTAINER" "test-poll-001" '{"id":"test-poll-001","action":"new","parent":"test","status":"pending","requested_at":"2025-01-01T00:00:00Z","child_container":null,"fulfilled_at":null,"error":null}'
# Background: update to fulfilled after 3s
cexec_bash "$CONTAINER" "sleep 3 && echo '{\"id\":\"test-poll-001\",\"action\":\"new\",\"parent\":\"test\",\"status\":\"fulfilled\",\"requested_at\":\"2025-01-01T00:00:00Z\",\"child_container\":null,\"fulfilled_at\":\"2025-01-01T00:03:00Z\",\"error\":null}' > $REQUEST_DIR/test-poll-001.json" &
OUTPUT=$(cexec "$CONTAINER" maestro-request wait request test-poll-001 fulfilled --timeout 10 2>&1)
EXIT_CODE=$?
wait
assert_exit_code "polls until fulfilled exits 0" 0 "$EXIT_CODE"
assert_contains "output has fulfilled id" "test-poll-001" "$OUTPUT"

# --- Test: failed status → exit 1 ---
clean_requests "$CONTAINER"
write_request_file "$CONTAINER" "test-fail-001" '{"id":"test-fail-001","action":"new","parent":"test","status":"failed","requested_at":"2025-01-01T00:00:00Z","child_container":null,"fulfilled_at":null,"error":"something broke"}'
OUTPUT=$(cexec "$CONTAINER" maestro-request wait request test-fail-001 fulfilled --timeout 5 2>&1)
EXIT_CODE=$?
assert_exit_code "failed status exits 1" 1 "$EXIT_CODE"

# --- Test: timeout → exit 1 ---
clean_requests "$CONTAINER"
write_request_file "$CONTAINER" "test-to-001" '{"id":"test-to-001","action":"new","parent":"test","status":"pending","requested_at":"2025-01-01T00:00:00Z","child_container":null,"fulfilled_at":null,"error":null}'
OUTPUT=$(cexec "$CONTAINER" maestro-request wait request test-to-001 fulfilled --timeout 2 2>&1)
EXIT_CODE=$?
assert_exit_code "timeout exits 1" 1 "$EXIT_CODE"
assert_contains "timeout message" "Timeout" "$OUTPUT"

# --- Test: single-check mode (--timeout 0) fulfilled ---
clean_requests "$CONTAINER"
write_request_file "$CONTAINER" "test-sc-001" '{"id":"test-sc-001","action":"new","parent":"test","status":"fulfilled","requested_at":"2025-01-01T00:00:00Z","child_container":null,"fulfilled_at":"2025-01-01T00:01:00Z","error":null}'
OUTPUT=$(cexec "$CONTAINER" maestro-request wait request test-sc-001 fulfilled --timeout 0 2>&1)
EXIT_CODE=$?
assert_exit_code "single-check fulfilled exits 0" 0 "$EXIT_CODE"

# --- Test: single-check mode (--timeout 0) pending → exit 1 ---
clean_requests "$CONTAINER"
write_request_file "$CONTAINER" "test-sc-002" '{"id":"test-sc-002","action":"new","parent":"test","status":"pending","requested_at":"2025-01-01T00:00:00Z","child_container":null,"fulfilled_at":null,"error":null}'
OUTPUT=$(cexec "$CONTAINER" maestro-request wait request test-sc-002 fulfilled --timeout 0 2>&1)
EXIT_CODE=$?
assert_exit_code "single-check pending exits 1" 1 "$EXIT_CODE"

# --- Test: invalid target status → exit 1 ---
OUTPUT=$(cexec "$CONTAINER" maestro-request wait request fake-id failed 2>&1)
EXIT_CODE=$?
assert_exit_code "invalid target 'failed' exits 1" 1 "$EXIT_CODE"
assert_contains "error mentions invalid" "invalid target status" "$OUTPUT"

# --- Test: output is request JSON on success ---
clean_requests "$CONTAINER"
write_request_file "$CONTAINER" "test-json-001" '{"id":"test-json-001","action":"new","parent":"test","status":"fulfilled","requested_at":"2025-01-01T00:00:00Z","child_container":null,"fulfilled_at":"2025-01-01T00:01:00Z","error":null}'
OUTPUT=$(cexec "$CONTAINER" maestro-request wait request test-json-001 fulfilled --timeout 5 2>&1)
# Verify it's valid JSON
VALID=$(echo "$OUTPUT" | cjq "$CONTAINER" -r '.id' 2>/dev/null || echo "(invalid json)")
assert_eq "output is valid JSON with id" "test-json-001" "$VALID"

print_summary
