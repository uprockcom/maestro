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

# Test: maestro-agent service — heartbeat delivery and suppression.
# Usage: ./test-service.sh <container>
#
# These tests start a short-lived service process with a test manifest
# and verify heartbeat behavior. Each test writes its own manifest,
# starts the service, runs assertions, then kills the service.
#
# The service polls every 2s. With a 5s heartbeat interval, the first
# heartbeat fires after ~5s. We allow 10s total (5s interval + 2s poll
# + 3s margin).

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

MANIFEST_FILE="/home/node/.maestro/agent.yml"
PENDING_DIR="/home/node/.maestro/pending-messages"
SERVICE_PID_FILE="/home/node/.maestro/state/maestro-agent.pid"

# Helper: write a test manifest with short heartbeat interval
write_test_manifest() {
  local interval="$1"
  local suppress="$2"
  docker exec "$CONTAINER" bash -c "cat > $MANIFEST_FILE << EOF
type: worker
heartbeat:
  interval: $interval
  suppress_while_active: $suppress
EOF"
}

# Helper: start the service in background via docker exec -d
start_service() {
  docker exec -d -u node -e HOME=/home/node "$CONTAINER" maestro-agent service
  sleep 3  # let the service start and run its first poll cycle
}

# Helper: kill the service process
kill_service() {
  cexec_bash "$CONTAINER" "pkill -f 'maestro-agent service' 2>/dev/null" || true
  sleep 1
  # Force kill any remaining
  cexec_bash "$CONTAINER" "pkill -9 -f 'maestro-agent service' 2>/dev/null" || true
}

# Helper: count files in pending-messages dir
count_pending() {
  cexec_bash "$CONTAINER" "ls -1 $PENDING_DIR/*.txt 2>/dev/null | wc -l | tr -d ' '"
}

# Helper: clean up pending messages and manifest
cleanup() {
  kill_service
  cexec_bash "$CONTAINER" "rm -f $PENDING_DIR/*.txt $MANIFEST_FILE $SERVICE_PID_FILE"
}

# ============================================================
# Setup: ensure clean state
# ============================================================
# Kill Claude/tmux so the service's wake-from-idle tmux command
# fails harmlessly. Otherwise Claude's hooks drain the message
# queue before the test can check it.
cexec_bash "$CONTAINER" "pkill -f 'claude' 2>/dev/null" || true
cexec_bash "$CONTAINER" "tmux kill-server 2>/dev/null" || true
sleep 1

cleanup

section "Heartbeat delivery during idle state"

write_test_manifest 5 false
set_agent_state "$CONTAINER" "idle"
cexec_bash "$CONTAINER" "rm -f $PENDING_DIR/*.txt"

start_service
sleep 7

COUNT=$(count_pending)
if [[ "$COUNT" -ge 1 ]]; then
  pass "Heartbeat delivered during idle (found $COUNT message(s))"
else
  fail "Heartbeat delivered during idle" ">=1 messages" "$COUNT messages"
fi

# Verify the message contains heartbeat trigger
MSG_CONTENT=$(cexec_bash "$CONTAINER" "cat $PENDING_DIR/*.txt 2>/dev/null | head -5")
assert_contains "Heartbeat message has trigger tag" "TRIGGER: heartbeat" "$MSG_CONTENT"

cleanup

section "Heartbeat suppression during active state"

write_test_manifest 5 true
set_agent_state "$CONTAINER" "active"
cexec_bash "$CONTAINER" "rm -f $PENDING_DIR/*.txt"

start_service
sleep 12

COUNT=$(count_pending)
assert_eq "No heartbeat during active (suppress=true)" "0" "$COUNT"

cleanup

section "Heartbeat NOT suppressed during active when suppress=false"

write_test_manifest 5 false
set_agent_state "$CONTAINER" "active"
cexec_bash "$CONTAINER" "rm -f $PENDING_DIR/*.txt"

start_service
sleep 7

COUNT=$(count_pending)
if [[ "$COUNT" -ge 1 ]]; then
  pass "Heartbeat delivered during active (suppress=false, found $COUNT message(s))"
else
  fail "Heartbeat delivered during active (suppress=false)" ">=1 messages" "$COUNT messages"
fi

cleanup

section "Heartbeat delivery during waiting state"

write_test_manifest 5 true
set_agent_state "$CONTAINER" "waiting"
cexec_bash "$CONTAINER" "rm -f $PENDING_DIR/*.txt"

start_service
sleep 7

COUNT=$(count_pending)
if [[ "$COUNT" -ge 1 ]]; then
  pass "Heartbeat delivered during waiting (found $COUNT message(s))"
else
  fail "Heartbeat delivered during waiting" ">=1 messages" "$COUNT messages"
fi

cleanup

section "No heartbeat when interval=0"

docker exec "$CONTAINER" bash -c "cat > $MANIFEST_FILE << EOF
type: worker
heartbeat:
  interval: 0
EOF"
set_agent_state "$CONTAINER" "idle"
cexec_bash "$CONTAINER" "rm -f $PENDING_DIR/*.txt"

start_service
sleep 8

COUNT=$(count_pending)
assert_eq "No heartbeat when interval=0" "0" "$COUNT"

cleanup

# Restore state for other tests
set_agent_state "$CONTAINER" "active"

print_summary
