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

# Query maestro-request state inside a container.
# Usage: ./query.sh <container>

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

echo -e "${BOLD}Container:${NC} $CONTAINER"
echo ""

echo -e "${BOLD}Hostname${NC}"
echo "  $(cexec "$CONTAINER" hostname)"
echo ""

echo -e "${BOLD}Request Files${NC}"
COUNT=$(count_requests "$CONTAINER")
echo "  count: $COUNT"
if [[ "$COUNT" -gt 0 ]]; then
  echo "  latest 3:"
  list_requests "$CONTAINER" | head -3 | while read -r f; do
    local_id=$(basename "$f" .json)
    action=$(request_field "$CONTAINER" "$local_id" "action")
    status=$(request_field "$CONTAINER" "$local_id" "status")
    echo "    $local_id  action=$action  status=$status"
  done
fi
echo ""

echo -e "${BOLD}Pending Messages${NC}"
MSG_COUNT=$(count_messages "$CONTAINER")
echo "  count: $MSG_COUNT"
if [[ "$MSG_COUNT" -gt 0 ]]; then
  cexec_bash "$CONTAINER" "ls $PENDING_MESSAGES_DIR/*.txt 2>/dev/null" | while read -r f; do
    echo "    $(basename "$f")"
  done
fi
echo ""

echo -e "${BOLD}Daemon IPC${NC}"
echo "  daemon-ipc.json present: $(has_daemon_ipc "$CONTAINER")"
echo ""

echo -e "${BOLD}maestro-request binary${NC}"
cexec_bash "$CONTAINER" "ls -la /usr/local/bin/maestro-request 2>/dev/null || echo '  (not found)'" | sed 's/^/  /'
