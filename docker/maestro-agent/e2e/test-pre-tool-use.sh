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

# Test: pre-tool-use hook state transitions.
# Usage: ./test-pre-tool-use.sh <container>

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

section "pre-tool-use (default) → active"

set_agent_state "$CONTAINER" "idle"
cexec "$CONTAINER" maestro-agent hook pre-tool-use
assert_eq "state becomes active" "active" "$(read_state "$CONTAINER")"
assert_eq "idle flag removed" "N" "$(has_idle_flag "$CONTAINER")"

section "pre-tool-use --idle → idle"

set_agent_state "$CONTAINER" "active"
cexec "$CONTAINER" maestro-agent hook pre-tool-use --idle
assert_eq "state becomes idle" "idle" "$(read_state "$CONTAINER")"
assert_eq "idle flag created" "Y" "$(has_idle_flag "$CONTAINER")"

section "pre-tool-use from question → active"

set_agent_state "$CONTAINER" "question"
cexec "$CONTAINER" maestro-agent hook pre-tool-use
assert_eq "state becomes active" "active" "$(read_state "$CONTAINER")"
assert_eq "idle flag removed" "N" "$(has_idle_flag "$CONTAINER")"

print_summary
