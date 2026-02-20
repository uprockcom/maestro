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

func TestPostToolUse_TransitionsToActive(t *testing.T) {
	setupTestDirs(t)

	WriteState(StateIdle)

	// Simulate stdin with a non-AskUserQuestion tool
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	w.Write([]byte(`{"tool_name": "Read"}`))
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	runHookPostToolUse(nil, nil)

	got := ReadState()
	if got != StateActive {
		t.Errorf("state after post-tool-use = %q, want %q", got, StateActive)
	}
}

func TestPostToolUse_CleansUpQuestionFile(t *testing.T) {
	setupTestDirs(t)

	WriteState(StateQuestion)

	// Create question file
	os.WriteFile(currentQuestionFile, []byte(`{"question": "test?"}`), 0644)

	// Simulate stdin with AskUserQuestion tool
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	w.Write([]byte(`{"tool_name": "AskUserQuestion"}`))
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	runHookPostToolUse(nil, nil)

	// State should be active
	got := ReadState()
	if got != StateActive {
		t.Errorf("state after post-tool-use = %q, want %q", got, StateActive)
	}

	// Question file should be removed
	if _, err := os.Stat(currentQuestionFile); !os.IsNotExist(err) {
		t.Error("current-question.json should be removed after AskUserQuestion post-tool-use")
	}
}

func TestPostToolUse_NonAskDoesNotRemoveQuestionFile(t *testing.T) {
	setupTestDirs(t)

	// Create question file (simulating it exists from a prior ask)
	os.WriteFile(currentQuestionFile, []byte(`{"question": "test?"}`), 0644)

	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	w.Write([]byte(`{"tool_name": "EnterPlanMode"}`))
	w.Close()
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	runHookPostToolUse(nil, nil)

	// Question file should still exist (only removed for AskUserQuestion)
	if _, err := os.Stat(currentQuestionFile); os.IsNotExist(err) {
		t.Error("current-question.json should NOT be removed for non-AskUserQuestion tools")
	}
}

func TestPostToolUse_EmptyStdinStillTransitions(t *testing.T) {
	setupTestDirs(t)

	WriteState(StateIdle)

	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	w.Close() // empty stdin
	os.Stdin = r
	defer func() { os.Stdin = oldStdin }()

	runHookPostToolUse(nil, nil)

	got := ReadState()
	if got != StateActive {
		t.Errorf("state after post-tool-use with empty stdin = %q, want %q", got, StateActive)
	}
}
