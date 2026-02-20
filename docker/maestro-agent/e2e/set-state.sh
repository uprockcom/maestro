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

# Set a container into a specific test state.
# Usage: ./set-state.sh <container> <state> [--with-question]
#
# States: active, idle, question, waiting, starting, clearing
# --with-question: also write a sample question file

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container> <state> [--with-question]}")
STATE="${2:?Usage: $0 <container> <state> [--with-question]}"
WITH_QUESTION="${3:-}"

set_agent_state "$CONTAINER" "$STATE"

if [[ "$WITH_QUESTION" == "--with-question" ]]; then
  cexec_bash "$CONTAINER" "echo '{\"question\":\"Pick one?\",\"options\":[\"A\",\"B\"]}' > $QUESTION_FILE"
  echo "Set state=$STATE with question file"
else
  echo "Set state=$STATE"
fi

# Show result
echo "  state:     $(read_state "$CONTAINER")"
echo "  idle flag: $(has_idle_flag "$CONTAINER")"
echo "  q_file:    $(has_question_file "$CONTAINER")"
