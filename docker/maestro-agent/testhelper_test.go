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
	"path/filepath"
	"testing"
)

// setupTestDirs redirects all package-level paths to a temp directory
// and returns a cleanup function that restores the originals.
func setupTestDirs(t *testing.T) string {
	t.Helper()

	tmp := t.TempDir()

	// Save originals
	origMaestroDir := maestroDir
	origStateDir := stateDir
	origLogsDir := logsDir
	origPendingMsgDir := pendingMsgDir
	origAgentStateFile := agentStateFile
	origClaudePIDFile := claudePIDFile
	origSessionReadyFile := sessionReadyFile
	origAgentPIDFile := agentPIDFile
	origClaudeIdleFile := claudeIdleFile
	origCurrentQuestionFile := currentQuestionFile
	origQuestionResponseFile := questionResponseFile
	origManifestFile := manifestFile
	origAgentLogFile := agentLogFile

	// Override
	maestroDir = tmp
	stateDir = filepath.Join(tmp, "state")
	logsDir = filepath.Join(tmp, "logs")
	pendingMsgDir = filepath.Join(tmp, "pending-messages")
	agentStateFile = filepath.Join(stateDir, "agent-state")
	claudePIDFile = filepath.Join(stateDir, "claude-pid")
	sessionReadyFile = filepath.Join(stateDir, "session-ready")
	agentPIDFile = filepath.Join(stateDir, "maestro-agent.pid")
	claudeIdleFile = filepath.Join(tmp, "claude-idle")
	currentQuestionFile = filepath.Join(tmp, "current-question.json")
	questionResponseFile = filepath.Join(tmp, "question-response.txt")
	manifestFile = filepath.Join(tmp, "agent.yml")
	agentLogFile = filepath.Join(logsDir, "maestro-agent.log")

	// Create dirs
	os.MkdirAll(stateDir, 0755)
	os.MkdirAll(logsDir, 0755)
	os.MkdirAll(pendingMsgDir, 0755)

	t.Cleanup(func() {
		maestroDir = origMaestroDir
		stateDir = origStateDir
		logsDir = origLogsDir
		pendingMsgDir = origPendingMsgDir
		agentStateFile = origAgentStateFile
		claudePIDFile = origClaudePIDFile
		sessionReadyFile = origSessionReadyFile
		agentPIDFile = origAgentPIDFile
		claudeIdleFile = origClaudeIdleFile
		currentQuestionFile = origCurrentQuestionFile
		questionResponseFile = origQuestionResponseFile
		manifestFile = origManifestFile
		agentLogFile = origAgentLogFile
	})

	return tmp
}
