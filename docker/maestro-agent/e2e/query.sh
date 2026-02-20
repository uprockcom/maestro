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

# Query the full state of a maestro-agent container.
# Usage: ./query.sh <container>

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

echo -e "${BOLD}Container:${NC} $CONTAINER"
echo ""

echo -e "${BOLD}Agent State${NC}"
echo "  state:         $(read_state "$CONTAINER")"
echo "  idle flag:     $(has_idle_flag "$CONTAINER")"
echo "  question file: $(has_question_file "$CONTAINER")"
echo "  response file: $(has_response_file "$CONTAINER")"
echo ""

echo -e "${BOLD}Question Content${NC}"
QCONTENT=$(read_question_file "$CONTAINER")
if [[ "$QCONTENT" != "(none)" ]]; then
  echo "  $QCONTENT"
else
  echo "  (no active question)"
fi
echo ""

echo -e "${BOLD}Claude Process${NC}"
PROCS=$(cexec_bash "$CONTAINER" "ps aux | grep '[c]laude' || echo '  (not running)'")
echo "$PROCS" | sed 's/^/  /'
echo ""

echo -e "${BOLD}Tmux Sessions${NC}"
TMUX_OUT=$(cexec_bash "$CONTAINER" "tmux list-sessions 2>/dev/null || echo '  (no tmux server)'")
echo "$TMUX_OUT" | sed 's/^/  /'
echo ""

echo -e "${BOLD}Tmux Clients${NC}"
CLIENTS=$(cexec_bash "$CONTAINER" "tmux list-clients -t main 2>/dev/null || echo '  (none)'")
echo "$CLIENTS" | sed 's/^/  /'
echo ""

echo -e "${BOLD}Recent Logs (last 10)${NC}"
LOGS=$(cexec_bash "$CONTAINER" "tail -10 $LOG_FILE 2>/dev/null || echo '  (no log file)'")
echo "$LOGS" | sed 's/^/  /'
