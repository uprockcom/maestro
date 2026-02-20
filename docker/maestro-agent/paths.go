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

// Path variables — declared as var so tests can redirect to temp dirs.
// Production code never modifies these.
var (
	// Base directories
	maestroDir    = "/home/node/.maestro"
	stateDir      = maestroDir + "/state"
	logsDir       = maestroDir + "/logs"
	binDir        = maestroDir + "/bin"
	pendingMsgDir = maestroDir + "/pending-messages"
	requestsDir   = maestroDir + "/requests"

	// State files
	agentStateFile   = stateDir + "/agent-state"
	claudePIDFile    = stateDir + "/claude-pid"
	sessionReadyFile = stateDir + "/session-ready"
	agentPIDFile     = stateDir + "/maestro-agent.pid"

	// Backward-compat idle flag (daemon reads this)
	claudeIdleFile = maestroDir + "/claude-idle"

	// Question/response files (ask hook <-> daemon)
	currentQuestionFile  = maestroDir + "/current-question.json"
	questionResponseFile = maestroDir + "/question-response.txt"

	// Configuration
	manifestFile = maestroDir + "/agent.yml"

	// Logs
	agentLogFile = logsDir + "/maestro-agent.log"
)
