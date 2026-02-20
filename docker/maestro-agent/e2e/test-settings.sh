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

# Test: settings.json hook configuration is correct.
# Usage: ./test-settings.sh <container>

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

section "Settings.json Configuration"

SETTINGS=$(cexec_bash "$CONTAINER" "cat $SETTINGS_FILE")

# Hook commands present
assert_contains "Stop hook present" "maestro-agent hook stop" "$SETTINGS"
assert_contains "Session-start hook present" "maestro-agent hook session-start" "$SETTINGS"
assert_contains "Prompt hook present" "maestro-agent hook prompt" "$SETTINGS"
assert_contains "pre-tool-use hook present" "maestro-agent hook pre-tool-use\"" "$SETTINGS"
assert_contains "pre-tool-use --idle present" "maestro-agent hook pre-tool-use --idle" "$SETTINGS"
assert_contains "ask hook present" "maestro-agent hook ask" "$SETTINGS"
assert_contains "post-tool-use hook present" "maestro-agent hook post-tool-use" "$SETTINGS"

# Timeouts
assert_contains "Stop hook has 86400 timeout" "86400" "$SETTINGS"

# Matchers
assert_contains "AskUserQuestion matcher" "AskUserQuestion" "$SETTINGS"
assert_contains "EnterPlanMode matcher" "EnterPlanMode" "$SETTINGS"

# No legacy bash references
assert_not_contains "No ask-hook.sh reference" "ask-hook.sh" "$SETTINGS"
assert_not_contains "No client-connected.sh reference" "client-connected.sh" "$SETTINGS"
assert_not_contains "No touch claude-idle" "touch" "$SETTINGS"
assert_not_contains "No rm -f in hooks" "rm -f" "$SETTINGS"

# Valid JSON
VALID=$(echo "$SETTINGS" | python3 -m json.tool &>/dev/null && echo "Y" || echo "N")
assert_eq "Valid JSON" "Y" "$VALID"

print_summary
