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

# Shared helpers for maestro-request e2e tests.
# Source this from other scripts: source "$(dirname "$0")/lib.sh"

set -uo pipefail
# NOTE: We intentionally do NOT use set -e. Exit codes from maestro-request
# are tested explicitly via assert_eq/assert_contains. docker exec propagates
# non-zero exits which would abort the suite prematurely under set -e.

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# --- Container resolution ---
resolve_container() {
  local input="${1:?Usage: resolve_container <name|search>}"
  if docker inspect "$input" &>/dev/null; then
    echo "$input"
    return
  fi
  if docker inspect "maestro-$input" &>/dev/null; then
    echo "maestro-$input"
    return
  fi
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

# --- Paths (must match request.go / daemon.go / cmd_wait.go) ---
REQUEST_DIR="/home/node/.maestro/requests"
PENDING_MESSAGES_DIR="/home/node/.maestro/pending-messages"
DAEMON_IPC_FILE="/home/node/.maestro/daemon/daemon-ipc.json"

# --- Request file helpers ---
list_requests() {
  cexec_bash "$1" "ls -t $REQUEST_DIR/*.json 2>/dev/null || true"
}

count_requests() {
  cexec_bash "$1" "ls $REQUEST_DIR/*.json 2>/dev/null | wc -l | tr -d ' '"
}

read_request() {
  local container="$1" id="$2"
  cexec_bash "$container" "cat $REQUEST_DIR/${id}.json 2>/dev/null || echo '(not found)'"
}

latest_request() {
  cexec_bash "$1" "ls -t $REQUEST_DIR/*.json 2>/dev/null | head -1"
}

clean_requests() {
  cexec_bash "$1" "rm -f $REQUEST_DIR/*.json"
}

request_field() {
  local container="$1" id="$2" field="$3"
  cexec_bash "$container" "jq -r '.$field' $REQUEST_DIR/${id}.json 2>/dev/null || echo '(error)'"
}

# Extract the request ID from the most recent request file (basename without .json)
latest_request_id() {
  local path
  path=$(latest_request "$1")
  if [[ -z "$path" ]]; then
    echo "(none)"
    return
  fi
  basename "$path" .json
}

# --- Message queue helpers ---
write_message() {
  local container="$1" name="$2" content="$3"
  cexec_bash "$container" "mkdir -p $PENDING_MESSAGES_DIR && echo '$content' > $PENDING_MESSAGES_DIR/${name}.txt"
}

clean_messages() {
  cexec_bash "$1" "rm -f $PENDING_MESSAGES_DIR/*.txt"
}

count_messages() {
  cexec_bash "$1" "ls $PENDING_MESSAGES_DIR/*.txt 2>/dev/null | wc -l | tr -d ' '"
}

# --- Daemon helpers ---
has_daemon_ipc() {
  cexec_bash "$1" "test -f $DAEMON_IPC_FILE && echo Y || echo N"
}

# Disable daemon by renaming the daemon-ipc binary.
# We can't move daemon-ipc.json because it's on a read-only bind mount.
# Renaming the binary causes daemonCall() to fail, triggering the
# "daemon unreachable" fallback in all commands.
DAEMON_IPC_BIN="/usr/local/bin/daemon-ipc"

disable_daemon() {
  docker exec -u root "$1" mv "$DAEMON_IPC_BIN" "${DAEMON_IPC_BIN}.bak" 2>/dev/null || true
}

restore_daemon() {
  docker exec -u root "$1" mv "${DAEMON_IPC_BIN}.bak" "$DAEMON_IPC_BIN" 2>/dev/null || true
}

# --- Write a synthetic request file directly (for wait request tests) ---
write_request_file() {
  local container="$1" id="$2" json="$3"
  cexec_bash "$container" "mkdir -p $REQUEST_DIR && echo '$json' > $REQUEST_DIR/${id}.json"
}

# --- Assertion helpers ---
PASS_COUNT=0
FAIL_COUNT=0

pass() {
  ((PASS_COUNT++))
  echo -e "  ${GREEN}PASS${NC} $1"
}

fail() {
  ((FAIL_COUNT++))
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

assert_exit_code() {
  local label="$1" expected="$2" actual="$3"
  if [[ "$actual" -eq "$expected" ]]; then
    pass "$label"
  else
    fail "$label" "exit code $expected" "exit code $actual"
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

# --- JSON parsing (uses jq inside the container) ---
# Usage: echo "$JSON_STRING" | cjq <container> '.field'
cjq() {
  local container="$1"; shift
  docker exec -i "$container" jq "$@"
}

# --- Section header ---
section() {
  echo ""
  echo -e "${CYAN}${BOLD}=== $1 ===${NC}"
}
