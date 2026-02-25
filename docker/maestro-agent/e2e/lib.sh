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

# Shared helpers for maestro-agent e2e tests.
# Source this from other scripts: source "$(dirname "$0")/lib.sh"

set -uo pipefail
# NOTE: We intentionally do NOT use set -e. The ask hook uses os.Exit(2) for
# response delivery, which propagates through docker exec. Individual test
# assertions handle errors explicitly via assert_eq/assert_contains.

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# --- Container resolution ---
# Accepts a container name or a search term. If the argument doesn't start with
# "maestro-", searches running containers for a match.
resolve_container() {
  local input="${1:?Usage: resolve_container <name|search>}"
  if docker inspect "$input" &>/dev/null; then
    echo "$input"
    return
  fi
  # Try with maestro- prefix
  if docker inspect "maestro-$input" &>/dev/null; then
    echo "maestro-$input"
    return
  fi
  # Fuzzy match on running containers
  local match
  match=$(docker ps --format '{{.Names}}' | grep -i "$input" | head -1)
  if [[ -n "$match" ]]; then
    echo "$match"
    return
  fi
  echo >&2 "Error: no container matching '$input'"
  return 1
}

# --- Container exec helpers ---
cexec() {
  local container="$1"; shift
  docker exec "$container" "$@"
}

cexec_bash() {
  local container="$1"; shift
  docker exec "$container" bash -c "$*"
}

# --- State paths (must match paths.go) ---
STATE_FILE="/home/node/.maestro/state/agent-state"
IDLE_FLAG="/home/node/.maestro/claude-idle"
QUESTION_FILE="/home/node/.maestro/current-question.json"
RESPONSE_FILE="/home/node/.maestro/question-response.txt"
LOG_FILE="/home/node/.maestro/logs/maestro-agent.log"
SETTINGS_FILE="/home/node/.claude/settings.json"

# --- Read helpers ---
read_state() {
  cexec_bash "$1" "cat $STATE_FILE 2>/dev/null || echo '(none)'"
}

has_idle_flag() {
  cexec_bash "$1" "test -f $IDLE_FLAG && echo Y || echo N"
}

has_question_file() {
  cexec_bash "$1" "test -f $QUESTION_FILE && echo Y || echo N"
}

has_response_file() {
  cexec_bash "$1" "test -f $RESPONSE_FILE && echo Y || echo N"
}

read_question_file() {
  cexec_bash "$1" "cat $QUESTION_FILE 2>/dev/null || echo '(none)'"
}

# --- Set state (writes state file + manages idle flag correctly) ---
set_agent_state() {
  local container="$1" state="$2"
  cexec_bash "$container" "echo '$state' > $STATE_FILE"
  case "$state" in
    idle|question)
      cexec_bash "$container" "touch $IDLE_FLAG"
      ;;
    *)
      cexec_bash "$container" "rm -f $IDLE_FLAG"
      ;;
  esac
}

# --- Assertion helpers ---
PASS_COUNT=0
FAIL_COUNT=0

pass() {
  PASS_COUNT=$((PASS_COUNT + 1))
  echo -e "  ${GREEN}PASS${NC} $1"
}

fail() {
  FAIL_COUNT=$((FAIL_COUNT + 1))
  echo -e "  ${RED}FAIL${NC} $1"
  if [[ -n "${2:-}" ]]; then
    echo -e "       expected: $2"
    echo -e "       got:      $3"
  fi
}

assert_eq() {
  local label="$1" expected="$2" actual="$3"
  if [[ "$actual" == "$expected" ]]; then
    pass "$label"
  else
    fail "$label" "$expected" "$actual"
  fi
}

assert_contains() {
  local label="$1" expected="$2" actual="$3"
  if [[ "$actual" == *"$expected"* ]]; then
    pass "$label"
  else
    fail "$label" "contains '$expected'" "$actual"
  fi
}

assert_not_contains() {
  local label="$1" unexpected="$2" actual="$3"
  if [[ "$actual" != *"$unexpected"* ]]; then
    pass "$label"
  else
    fail "$label" "should not contain '$unexpected'" "$actual"
  fi
}

# --- Summary ---
print_summary() {
  echo ""
  local total=$((PASS_COUNT + FAIL_COUNT))
  if [[ $FAIL_COUNT -eq 0 ]]; then
    echo -e "${GREEN}${BOLD}All $total tests passed.${NC}"
  else
    echo -e "${RED}${BOLD}$FAIL_COUNT of $total tests failed.${NC}"
  fi
  return $FAIL_COUNT
}

# --- Section header ---
section() {
  echo ""
  echo -e "${CYAN}${BOLD}=== $1 ===${NC}"
}

# --- Cleanup question/response state ---
clean_question_state() {
  local container="$1"
  cexec_bash "$container" "rm -f $QUESTION_FILE $RESPONSE_FILE"
}
