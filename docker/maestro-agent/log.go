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
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// LogEntry represents a structured log line
type LogEntry struct {
	Time    string `json:"time"`
	Level   string `json:"level"`
	Msg     string `json:"msg"`
	Hook    string `json:"hook,omitempty"`
	State   string `json:"state,omitempty"`
	Trigger string `json:"trigger,omitempty"`
	Error   string `json:"error,omitempty"`
}

var logFile *os.File

// suppressStderr disables stderr logging. Set true in hook commands
// where stderr is captured by Claude Code (especially Stop hook).
var suppressStderr bool

// InitLog opens the log file for appending
func InitLog() {
	var err error
	logFile, err = os.OpenFile(agentLogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		// Best-effort: if we can't log, continue without
		logFile = nil
	}
}

// Log writes a structured log entry
func Log(level, msg string, fields ...string) {
	entry := LogEntry{
		Time:  time.Now().Format(time.RFC3339),
		Level: level,
		Msg:   msg,
	}

	// Parse key-value pairs from fields
	for i := 0; i+1 < len(fields); i += 2 {
		switch fields[i] {
		case "hook":
			entry.Hook = fields[i+1]
		case "state":
			entry.State = fields[i+1]
		case "trigger":
			entry.Trigger = fields[i+1]
		case "error":
			entry.Error = fields[i+1]
		}
	}

	if logFile != nil {
		data, err := json.Marshal(entry)
		if err == nil {
			logFile.Write(data)
			logFile.Write([]byte("\n"))
		}
	}

	// Write to stderr only when not running as a hook
	if !suppressStderr {
		fmt.Fprintf(os.Stderr, "[%s] %s %s", entry.Time, level, msg)
		for i := 0; i+1 < len(fields); i += 2 {
			fmt.Fprintf(os.Stderr, " %s=%s", fields[i], fields[i+1])
		}
		fmt.Fprintln(os.Stderr)
	}
}

func LogInfo(msg string, fields ...string) {
	Log("INFO", msg, fields...)
}

func LogError(msg string, fields ...string) {
	Log("ERROR", msg, fields...)
}

func LogDebug(msg string, fields ...string) {
	Log("DEBUG", msg, fields...)
}
