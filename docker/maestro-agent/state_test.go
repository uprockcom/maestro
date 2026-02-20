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

func TestReadState_NoFile(t *testing.T) {
	setupTestDirs(t)
	if got := ReadState(); got != StateStarting {
		t.Errorf("ReadState() with no file = %q, want %q", got, StateStarting)
	}
}

func TestReadState_ValidStates(t *testing.T) {
	setupTestDirs(t)

	for _, state := range []AgentState{StateStarting, StateActive, StateWaiting, StateIdle, StateQuestion, StateClearing, StateConnected} {
		os.WriteFile(agentStateFile, []byte(string(state)+"\n"), 0644)
		if got := ReadState(); got != state {
			t.Errorf("ReadState() = %q, want %q", got, state)
		}
	}
}

func TestReadState_InvalidState(t *testing.T) {
	setupTestDirs(t)
	os.WriteFile(agentStateFile, []byte("bogus\n"), 0644)
	if got := ReadState(); got != StateStarting {
		t.Errorf("ReadState() with invalid state = %q, want %q", got, StateStarting)
	}
}

func TestWriteState_Basic(t *testing.T) {
	setupTestDirs(t)

	if err := WriteState(StateActive); err != nil {
		t.Fatalf("WriteState(active) error: %v", err)
	}
	if got := ReadState(); got != StateActive {
		t.Errorf("ReadState() after write = %q, want %q", got, StateActive)
	}
}

func TestWriteState_IdleFlagCreated(t *testing.T) {
	setupTestDirs(t)

	WriteState(StateIdle)

	if _, err := os.Stat(claudeIdleFile); os.IsNotExist(err) {
		t.Error("claude-idle file should exist when state is idle")
	}
}

func TestWriteState_IdleFlagRemovedOnActive(t *testing.T) {
	setupTestDirs(t)

	WriteState(StateIdle)
	// Verify it was created
	if _, err := os.Stat(claudeIdleFile); os.IsNotExist(err) {
		t.Fatal("claude-idle file should exist after setting idle")
	}

	WriteState(StateActive)
	if _, err := os.Stat(claudeIdleFile); !os.IsNotExist(err) {
		t.Error("claude-idle file should be removed when state is active")
	}
}

func TestWriteState_QuestionCreatesIdleFlag(t *testing.T) {
	setupTestDirs(t)

	WriteState(StateQuestion)

	if _, err := os.Stat(claudeIdleFile); os.IsNotExist(err) {
		t.Error("claude-idle file should exist when state is question")
	}

	// Verify state roundtrip
	if got := ReadState(); got != StateQuestion {
		t.Errorf("ReadState() = %q, want %q", got, StateQuestion)
	}
}

func TestWriteState_AllNonIdleStatesRemoveFlag(t *testing.T) {
	setupTestDirs(t)

	for _, state := range []AgentState{StateStarting, StateActive, StateWaiting, StateClearing, StateConnected} {
		// Set idle first to create the flag
		WriteState(StateIdle)
		// Then set non-idle state
		WriteState(state)

		if _, err := os.Stat(claudeIdleFile); !os.IsNotExist(err) {
			t.Errorf("claude-idle file should not exist for state %q", state)
		}
	}
}

func TestEnsureStateDirs(t *testing.T) {
	tmp := t.TempDir()
	stateDir = tmp + "/state"
	logsDir = tmp + "/logs"
	defer func() {
		stateDir = "/home/node/.maestro/state"
		logsDir = "/home/node/.maestro/logs"
	}()

	if err := EnsureStateDirs(); err != nil {
		t.Fatalf("EnsureStateDirs() error: %v", err)
	}

	for _, dir := range []string{stateDir, logsDir} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("directory %s should exist: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("%s should be a directory", dir)
		}
	}
}
