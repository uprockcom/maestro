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
	"strings"
	"sync"
)

// AgentState represents the current state of the agent
type AgentState string

const (
	StateStarting  AgentState = "starting"
	StateActive    AgentState = "active"
	StateWaiting   AgentState = "waiting"
	StateIdle      AgentState = "idle"
	StateQuestion  AgentState = "question"
	StateClearing  AgentState = "clearing"
	StateConnected AgentState = "connected"
)

var (
	stateMu sync.Mutex
)

// ReadState reads the current agent state from the state file
func ReadState() AgentState {
	data, err := os.ReadFile(agentStateFile)
	if err != nil {
		return StateStarting
	}
	s := AgentState(strings.TrimSpace(string(data)))
	switch s {
	case StateStarting, StateActive, StateWaiting, StateIdle, StateQuestion, StateClearing, StateConnected:
		return s
	default:
		return StateStarting
	}
}

// WriteState atomically writes the agent state
func WriteState(state AgentState) error {
	stateMu.Lock()
	defer stateMu.Unlock()

	// Atomic write: write to temp file, then rename
	tmp := agentStateFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(string(state)+"\n"), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, agentStateFile); err != nil {
		// Fallback to direct write if rename fails (cross-device)
		return os.WriteFile(agentStateFile, []byte(string(state)+"\n"), 0644)
	}

	// Manage backward-compat claude-idle flag
	// Both idle and question mean Claude is blocked waiting for input
	if state == StateIdle || state == StateQuestion {
		// Touch the idle file
		f, err := os.Create(claudeIdleFile)
		if err == nil {
			f.Close()
		}
	} else {
		// Remove the idle file
		os.Remove(claudeIdleFile)
	}

	return nil
}

// EnsureStateDirs creates required directories
func EnsureStateDirs() error {
	for _, dir := range []string{stateDir, logsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}
