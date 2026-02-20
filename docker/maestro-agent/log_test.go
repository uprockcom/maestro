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
	"os"
	"strings"
	"testing"
)

func TestLog_WritesToFile(t *testing.T) {
	setupTestDirs(t)
	InitLog()
	defer func() {
		if logFile != nil {
			logFile.Close()
			logFile = nil
		}
	}()

	suppressStderr = true
	defer func() { suppressStderr = false }()

	LogInfo("test message", "hook", "stop", "state", "active")

	// Flush and read the log file
	logFile.Sync()

	data, err := os.ReadFile(agentLogFile)
	if err != nil {
		t.Fatalf("failed to read log file: %v", err)
	}

	var entry LogEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil { // trim newline
		t.Fatalf("failed to parse log entry: %v (data: %q)", err, string(data))
	}

	if entry.Level != "INFO" {
		t.Errorf("Level = %q, want %q", entry.Level, "INFO")
	}
	if entry.Msg != "test message" {
		t.Errorf("Msg = %q, want %q", entry.Msg, "test message")
	}
	if entry.Hook != "stop" {
		t.Errorf("Hook = %q, want %q", entry.Hook, "stop")
	}
	if entry.State != "active" {
		t.Errorf("State = %q, want %q", entry.State, "active")
	}
}

func TestLog_SuppressStderr(t *testing.T) {
	setupTestDirs(t)
	InitLog()
	defer func() {
		if logFile != nil {
			logFile.Close()
			logFile = nil
		}
	}()

	// Redirect stderr to capture output
	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	suppressStderr = true
	LogInfo("suppressed message")

	w.Close()
	os.Stderr = origStderr

	var buf [4096]byte
	n, _ := r.Read(buf[:])
	r.Close()

	if n > 0 {
		t.Errorf("suppressStderr=true but got stderr output: %q", string(buf[:n]))
	}

	// Reset
	suppressStderr = false
}

func TestLog_StderrWhenNotSuppressed(t *testing.T) {
	setupTestDirs(t)
	InitLog()
	defer func() {
		if logFile != nil {
			logFile.Close()
			logFile = nil
		}
	}()

	origStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	suppressStderr = false
	LogInfo("visible message")

	w.Close()
	os.Stderr = origStderr

	var buf [4096]byte
	n, _ := r.Read(buf[:])
	r.Close()

	output := string(buf[:n])
	if !strings.Contains(output, "visible message") {
		t.Errorf("suppressStderr=false but stderr missing message, got: %q", output)
	}
}

func TestLogLevels(t *testing.T) {
	setupTestDirs(t)
	InitLog()
	defer func() {
		if logFile != nil {
			logFile.Close()
			logFile = nil
		}
	}()
	suppressStderr = true
	defer func() { suppressStderr = false }()

	LogInfo("info msg")
	LogError("error msg")
	LogDebug("debug msg")

	logFile.Sync()
	data, err := os.ReadFile(agentLogFile)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d log lines, want 3", len(lines))
	}

	levels := []string{"INFO", "ERROR", "DEBUG"}
	for i, line := range lines {
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("line %d: parse error: %v", i, err)
		}
		if entry.Level != levels[i] {
			t.Errorf("line %d: Level = %q, want %q", i, entry.Level, levels[i])
		}
	}
}
