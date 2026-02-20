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
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

func hookAskCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ask",
		Short: "Handle Claude's AskUserQuestion PreToolUse hook — blocking wait for response or user connection",
		RunE:  runHookAsk,
	}
}

// askHookInput represents the JSON structure piped to the hook via stdin
type askHookInput struct {
	ToolInput json.RawMessage `json:"tool_input"`
}

// parseAndWriteQuestion extracts tool_input from hook stdin data and writes it
// to the question file. Returns the data written.
func parseAndWriteQuestion(stdinData []byte) []byte {
	var input askHookInput
	if err := json.Unmarshal(stdinData, &input); err != nil {
		// Write raw stdin as fallback
		os.WriteFile(currentQuestionFile, stdinData, 0644)
		return stdinData
	}
	os.WriteFile(currentQuestionFile, input.ToolInput, 0644)
	return input.ToolInput
}

// askResult is returned by the first watcher to fire in the ask hook
type askResult struct {
	Source  string
	Content string
	Exit0   bool // true = exit 0 (user connected), false = exit 2 (response found)
}

// watchResponseFile polls questionResponseFile for a daemon-written answer.
func watchResponseFile(ctx context.Context, ch chan<- askResult) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		data, err := os.ReadFile(questionResponseFile)
		if err == nil && len(data) > 0 {
			select {
			case ch <- askResult{
				Source:  "response",
				Content: string(data),
				Exit0:   false,
			}:
			case <-ctx.Done():
			}
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
}

// watchUserConnected polls tmux for a user connection.
func watchUserConnected(ctx context.Context, ch chan<- askResult) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if isUserConnected() {
			select {
			case ch <- askResult{
				Source: "connected",
				Exit0:  true,
			}:
			case <-ctx.Done():
			}
			return
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func runHookAsk(cmd *cobra.Command, args []string) error {
	suppressStderr = true
	EnsureStateDirs()
	InitLog()

	// Re-entrancy guard
	if !isMainClaude() {
		LogDebug("Ask hook: child process, exiting silently", "hook", "ask")
		return nil // exit 0
	}

	LogInfo("Ask hook fired", "hook", "ask")

	// Parse stdin JSON and extract tool_input for the question file
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		LogError("Ask hook: failed to read stdin", "hook", "ask", "error", err.Error())
		return nil // exit 0 — don't block Claude on read errors
	}

	parseAndWriteQuestion(stdinData)
	LogInfo("Ask hook: wrote question file", "hook", "ask")

	// If a user is already connected, let them interact directly
	if isUserConnected() {
		LogInfo("Ask hook: user connected, passing through", "hook", "ask")
		return nil // exit 0
	}

	// No one connected — enter blocking wait
	os.Remove(questionResponseFile)
	WriteState(StateQuestion)
	LogInfo("Ask hook: entering blocking wait", "hook", "ask", "state", "question")

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()

	resultCh := make(chan askResult, 1)
	var wg sync.WaitGroup

	// Watcher 1: poll for response file
	wg.Add(1)
	go func() {
		defer wg.Done()
		watchResponseFile(ctx, resultCh)
	}()

	// Watcher 2: poll for user connection
	wg.Add(1)
	go func() {
		defer wg.Done()
		watchUserConnected(ctx, resultCh)
	}()

	// Wait for first result or timeout
	var result askResult
	select {
	case result = <-resultCh:
		cancel()
	case <-ctx.Done():
		// Timeout — let the tool proceed normally
		LogInfo("Ask hook: timeout reached, allowing passthrough", "hook", "ask")
		cancel()

		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}

		return nil // exit 0
	}

	// Clean up goroutines
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		LogDebug("Ask hook: watcher cleanup timed out", "hook", "ask")
	}

	LogInfo("Ask hook: watcher fired", "hook", "ask", "trigger", result.Source)

	if result.Exit0 {
		// User connected — let them answer interactively
		LogInfo("Ask hook: user connected, passing through", "hook", "ask")
		return nil // exit 0
	}

	// Response found — deliver to Claude via stderr and exit 2
	LogInfo("Ask hook: delivering response", "hook", "ask")
	os.Remove(questionResponseFile)
	os.Remove(currentQuestionFile)
	fmt.Fprint(os.Stderr, result.Content)
	os.Exit(2)
	return nil // unreachable
}
