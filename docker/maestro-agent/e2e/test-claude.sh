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

# Test: Claude process is running in the container.
# Usage: ./test-claude.sh <container>

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

section "Claude Process"

CLAUDE_RUNNING=$(cexec_bash "$CONTAINER" "ps aux | grep '[c]laude' | grep -v grep | wc -l")
if [[ "$CLAUDE_RUNNING" -gt 0 ]]; then
  pass "Claude is running ($CLAUDE_RUNNING process(es))"
else
  fail "Claude is not running" ">0 processes" "0 processes"
fi

section "Tmux Session"

TMUX_SESSIONS=$(cexec_bash "$CONTAINER" "tmux list-sessions 2>/dev/null | wc -l")
if [[ "$TMUX_SESSIONS" -gt 0 ]]; then
  pass "Tmux session exists"
else
  fail "No tmux session" ">0 sessions" "0 sessions"
fi

print_summary
