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
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestAskHookInput_ParsesToolInput(t *testing.T) {
	input := `{"tool_name": "AskUserQuestion", "tool_input": {"question": "Which approach?", "options": ["A", "B"]}}`

	var parsed askHookInput
	if err := json.Unmarshal([]byte(input), &parsed); err != nil {
		t.Fatalf("failed to parse ask hook input: %v", err)
	}

	if parsed.ToolInput == nil {
		t.Fatal("tool_input should not be nil")
	}

	// Verify tool_input is valid JSON
	var toolInput map[string]interface{}
	if err := json.Unmarshal(parsed.ToolInput, &toolInput); err != nil {
		t.Fatalf("tool_input should be valid JSON: %v", err)
	}

	if q, ok := toolInput["question"].(string); !ok || q != "Which approach?" {
		t.Errorf("question = %v, want 'Which approach?'", toolInput["question"])
	}
}

func TestParseAndWriteQuestion_ValidJSON(t *testing.T) {
	setupTestDirs(t)

	stdinData := []byte(`{"tool_name": "AskUserQuestion", "tool_input": {"question": "Pick one", "options": ["X", "Y"]}}`)
	written := parseAndWriteQuestion(stdinData)

	// Should have written tool_input, not the full stdin
	var toolInput map[string]interface{}
	if err := json.Unmarshal(written, &toolInput); err != nil {
		t.Fatalf("written data should be valid JSON: %v", err)
	}
	if toolInput["question"] != "Pick one" {
		t.Errorf("question = %v, want 'Pick one'", toolInput["question"])
	}

	// Verify file on disk
	data, err := os.ReadFile(currentQuestionFile)
	if err != nil {
		t.Fatalf("failed to read question file: %v", err)
	}
	if string(data) != string(written) {
		t.Errorf("file content = %q, want %q", string(data), string(written))
	}
}

func TestParseAndWriteQuestion_InvalidJSON(t *testing.T) {
	setupTestDirs(t)

	stdinData := []byte(`not valid json`)
	written := parseAndWriteQuestion(stdinData)

	// Should write raw stdin as fallback
	if string(written) != string(stdinData) {
		t.Errorf("written = %q, want raw stdin %q", string(written), string(stdinData))
	}

	// Verify file on disk has raw content
	data, err := os.ReadFile(currentQuestionFile)
	if err != nil {
		t.Fatalf("failed to read question file: %v", err)
	}
	if string(data) != string(stdinData) {
		t.Errorf("file content = %q, want %q", string(data), string(stdinData))
	}
}

func TestParseAndWriteQuestion_EmptyToolInput(t *testing.T) {
	setupTestDirs(t)

	// tool_input is null
	stdinData := []byte(`{"tool_name": "AskUserQuestion", "tool_input": null}`)
	written := parseAndWriteQuestion(stdinData)

	// Should write "null" (the JSON representation)
	if string(written) != "null" {
		t.Errorf("written = %q, want 'null'", string(written))
	}
}

func TestWatchResponseFile_FindsResponse(t *testing.T) {
	setupTestDirs(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch := make(chan askResult, 1)

	// Write response file after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.WriteFile(questionResponseFile, []byte("selected option A"), 0644)
	}()

	go watchResponseFile(ctx, ch)

	select {
	case result := <-ch:
		if result.Source != "response" {
			t.Errorf("source = %q, want 'response'", result.Source)
		}
		if result.Content != "selected option A" {
			t.Errorf("content = %q, want 'selected option A'", result.Content)
		}
		if result.Exit0 {
			t.Error("exit0 should be false for response")
		}
	case <-ctx.Done():
		t.Fatal("watchResponseFile timed out")
	}
}

func TestWatchResponseFile_CancelledContext(t *testing.T) {
	setupTestDirs(t)

	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan askResult, 1)

	go watchResponseFile(ctx, ch)

	// Cancel immediately
	cancel()

	// Should not send any result
	select {
	case <-ch:
		t.Error("should not have received result after cancel")
	case <-time.After(500 * time.Millisecond):
		// Expected — no result
	}
}

func TestWatchResponseFile_IgnoresEmptyFile(t *testing.T) {
	setupTestDirs(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	ch := make(chan askResult, 1)

	// Write empty file first, then real content
	os.WriteFile(questionResponseFile, []byte(""), 0644)

	go func() {
		time.Sleep(1500 * time.Millisecond)
		os.WriteFile(questionResponseFile, []byte("real answer"), 0644)
	}()

	go watchResponseFile(ctx, ch)

	select {
	case result := <-ch:
		if result.Content != "real answer" {
			t.Errorf("content = %q, want 'real answer'", result.Content)
		}
	case <-ctx.Done():
		t.Fatal("watchResponseFile timed out")
	}
}

func TestQuestionFilePaths(t *testing.T) {
	setupTestDirs(t)

	// Write a question file
	questionData := []byte(`{"question": "test?"}`)
	if err := os.WriteFile(currentQuestionFile, questionData, 0644); err != nil {
		t.Fatalf("failed to write question file: %v", err)
	}

	// Verify it's readable
	data, err := os.ReadFile(currentQuestionFile)
	if err != nil {
		t.Fatalf("failed to read question file: %v", err)
	}
	if string(data) != string(questionData) {
		t.Errorf("question file content = %q, want %q", string(data), string(questionData))
	}

	// Write a response file
	responseData := []byte("selected option A")
	if err := os.WriteFile(questionResponseFile, responseData, 0644); err != nil {
		t.Fatalf("failed to write response file: %v", err)
	}

	// Verify it's readable
	data, err = os.ReadFile(questionResponseFile)
	if err != nil {
		t.Fatalf("failed to read response file: %v", err)
	}
	if string(data) != string(responseData) {
		t.Errorf("response file content = %q, want %q", string(data), string(responseData))
	}

	// Cleanup should remove both
	os.Remove(currentQuestionFile)
	os.Remove(questionResponseFile)

	if _, err := os.Stat(currentQuestionFile); !os.IsNotExist(err) {
		t.Error("question file should be removed after cleanup")
	}
	if _, err := os.Stat(questionResponseFile); !os.IsNotExist(err) {
		t.Error("response file should be removed after cleanup")
	}
}

func TestQuestionStateTransition(t *testing.T) {
	setupTestDirs(t)

	// Set question state
	WriteState(StateQuestion)

	// Should create idle flag (question = blocked = idle)
	if _, err := os.Stat(claudeIdleFile); os.IsNotExist(err) {
		t.Error("claude-idle file should exist when state is question")
	}

	// Transition to active (response received)
	WriteState(StateActive)

	// Idle flag should be removed
	if _, err := os.Stat(claudeIdleFile); !os.IsNotExist(err) {
		t.Error("claude-idle file should be removed when state is active")
	}
}
