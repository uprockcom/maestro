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

# Test: host-side state display (maestro list indicators).
# Usage: ./test-host-display.sh <container>
#
# Requires maestro binary on PATH.

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")
# Strip maestro- prefix for grep matching
SHORT_NAME=$(echo "$CONTAINER" | sed 's/^maestro-//')

section "Host-side state indicators"

for state in active idle question waiting; do
  set_agent_state "$CONTAINER" "$state"
  sleep 0.2  # let docker exec settle
  LINE=$(maestro list 2>/dev/null | grep "$SHORT_NAME" | head -1)
  case "$state" in
    active)   assert_contains "$state shows working indicator" "●" "$LINE" ;;
    idle)     assert_contains "$state shows bell indicator" "🔔" "$LINE" ;;
    question) assert_contains "$state shows question indicator" "❓" "$LINE" ;;
    waiting)  assert_contains "$state shows bell indicator" "🔔" "$LINE" ;;
  esac
done

section "Host-side ReadAgentState"

set_agent_state "$CONTAINER" "question"
REMOTE_STATE=$(docker exec "$CONTAINER" cat "$STATE_FILE")
assert_eq "remote state reads correctly" "question" "$REMOTE_STATE"

set_agent_state "$CONTAINER" "active"
REMOTE_STATE=$(docker exec "$CONTAINER" cat "$STATE_FILE")
assert_eq "remote state reads correctly" "active" "$REMOTE_STATE"

# Cleanup
set_agent_state "$CONTAINER" "active"

print_summary
