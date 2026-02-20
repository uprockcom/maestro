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

# Test: post-tool-use hook state transitions and cleanup.
# Usage: ./test-post-tool-use.sh <container>

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

section "post-tool-use AskUserQuestion → active + cleanup"

set_agent_state "$CONTAINER" "question"
cexec_bash "$CONTAINER" "echo '{\"q\":\"test?\"}' > $QUESTION_FILE"
cexec_bash "$CONTAINER" "echo '{\"tool_name\": \"AskUserQuestion\"}' | maestro-agent hook post-tool-use"
assert_eq "state becomes active" "active" "$(read_state "$CONTAINER")"
assert_eq "idle flag removed" "N" "$(has_idle_flag "$CONTAINER")"
assert_eq "question file removed" "N" "$(has_question_file "$CONTAINER")"

section "post-tool-use Bash → active (preserves question file)"

set_agent_state "$CONTAINER" "question"
cexec_bash "$CONTAINER" "echo '{\"q\":\"test?\"}' > $QUESTION_FILE"
cexec_bash "$CONTAINER" "echo '{\"tool_name\": \"Bash\"}' | maestro-agent hook post-tool-use"
assert_eq "state becomes active" "active" "$(read_state "$CONTAINER")"
assert_eq "question file preserved" "Y" "$(has_question_file "$CONTAINER")"

# Cleanup
clean_question_state "$CONTAINER"

section "post-tool-use EnterPlanMode → active"

set_agent_state "$CONTAINER" "idle"
cexec_bash "$CONTAINER" "echo '{\"tool_name\": \"EnterPlanMode\"}' | maestro-agent hook post-tool-use"
assert_eq "state becomes active" "active" "$(read_state "$CONTAINER")"
assert_eq "idle flag removed" "N" "$(has_idle_flag "$CONTAINER")"

section "post-tool-use empty stdin → active (graceful)"

set_agent_state "$CONTAINER" "idle"
cexec_bash "$CONTAINER" "echo '' | maestro-agent hook post-tool-use"
assert_eq "state becomes active" "active" "$(read_state "$CONTAINER")"

print_summary
