// Copyright 2026 Christopher O'Connell
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"os"
	"testing"
)

func TestPreToolUse_DefaultSetsActive(t *testing.T) {
	setupTestDirs(t)

	// Start with idle
	WriteState(StateIdle)

	// Simulate pre-tool-use without --idle flag
	preToolUseIdle = false
	runHookPreToolUse(nil, nil)

	got := ReadState()
	if got != StateActive {
		t.Errorf("state after pre-tool-use = %q, want %q", got, StateActive)
	}

	// Idle flag should be removed
	if _, err := os.Stat(claudeIdleFile); !os.IsNotExist(err) {
		t.Error("claude-idle file should not exist after pre-tool-use without --idle")
	}
}

func TestPreToolUse_IdleFlagSetsIdle(t *testing.T) {
	setupTestDirs(t)

	WriteState(StateActive)

	// Simulate pre-tool-use with --idle flag
	preToolUseIdle = true
	runHookPreToolUse(nil, nil)

	got := ReadState()
	if got != StateIdle {
		t.Errorf("state after pre-tool-use --idle = %q, want %q", got, StateIdle)
	}

	// Idle flag should be created
	if _, err := os.Stat(claudeIdleFile); os.IsNotExist(err) {
		t.Error("claude-idle file should exist after pre-tool-use --idle")
	}
}
