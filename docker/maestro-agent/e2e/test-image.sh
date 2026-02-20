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

# Test: Docker image contents — legacy scripts absent, new commands present.
# Usage: ./test-image.sh <container>

source "$(dirname "$0")/lib.sh"

CONTAINER=$(resolve_container "${1:?Usage: $0 <container>}")

section "Image Contents"

# Legacy scripts should be absent
for script in ask-hook.sh client-connected.sh stop-hook.sh prompt-hook.sh; do
  result=$(cexec_bash "$CONTAINER" "test -f /usr/local/bin/$script && echo EXISTS || echo ABSENT")
  assert_eq "Legacy $script absent" "ABSENT" "$result"
done

# maestro-agent binary should exist
result=$(cexec_bash "$CONTAINER" "test -x /usr/local/bin/maestro-agent && echo EXISTS || echo ABSENT")
assert_eq "maestro-agent binary exists" "EXISTS" "$result"

# New subcommands should be registered
for cmd in ask post-tool-use pre-tool-use stop prompt session-start; do
  result=$(cexec "$CONTAINER" maestro-agent hook "$cmd" --help 2>&1 || true)
  assert_contains "hook $cmd registered" "Usage" "$result"
done

# pre-tool-use should have --idle flag
result=$(cexec "$CONTAINER" maestro-agent hook pre-tool-use --help 2>&1 || true)
assert_contains "pre-tool-use has --idle flag" "--idle" "$result"

print_summary
