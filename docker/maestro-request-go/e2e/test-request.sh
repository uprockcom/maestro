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

# Tests for: maestro-request request
#
# All tests run with daemon DISABLED. Running request with a live daemon
# sends actual resource requests to the host.
source "$(dirname "$0")/lib.sh"

CONTAINER="${1:?Usage: $0 <container>}"

section "maestro-request request"

# Disable daemon for all request tests
disable_daemon "$CONTAINER"

# Clean slate
clean_requests "$CONTAINER"

# --- Test: valid type "domain" creates request file ---
OUTPUT=$(cexec "$CONTAINER" maestro-request request domain example.com 2>&1)
ID=$(latest_request_id "$CONTAINER")
ACTION=$(request_field "$CONTAINER" "$ID" "action")
assert_eq "domain: action is 'request'" "request" "$ACTION"

# --- Test: request_type field ---
RTYPE=$(request_field "$CONTAINER" "$ID" "request_type")
assert_eq "request_type is 'domain'" "domain" "$RTYPE"

# --- Test: request_value field ---
RVAL=$(request_field "$CONTAINER" "$ID" "request_value")
assert_eq "request_value is 'example.com'" "example.com" "$RVAL"

# --- Test: valid type "memory" ---
clean_requests "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request request memory 14g 2>&1)
ID2=$(latest_request_id "$CONTAINER")
RTYPE2=$(request_field "$CONTAINER" "$ID2" "request_type")
assert_eq "memory: request_type is 'memory'" "memory" "$RTYPE2"

# --- Test: invalid type → exit 1 ---
OUTPUT=$(cexec "$CONTAINER" maestro-request request badtype val 2>&1)
EXIT_CODE=$?
assert_exit_code "invalid type exits 1" 1 "$EXIT_CODE"
assert_contains "error mentions invalid" "invalid request type" "$OUTPUT"

# --- Test: no args → usage error ---
OUTPUT=$(cexec "$CONTAINER" maestro-request request 2>&1)
EXIT_CODE=$?
assert_exit_code "no args exits 1" 1 "$EXIT_CODE"

# --- Test: daemon unavailable → exit 0 + queued ---
clean_requests "$CONTAINER"
OUTPUT=$(cexec "$CONTAINER" maestro-request request domain offline.com 2>&1)
EXIT_CODE=$?
assert_exit_code "daemon unavailable exits 0" 0 "$EXIT_CODE"
assert_contains "output mentions queued" "queued" "$OUTPUT"

# Restore daemon for subsequent suites
restore_daemon "$CONTAINER"

print_summary
