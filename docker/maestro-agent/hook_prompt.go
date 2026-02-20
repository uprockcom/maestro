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
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func hookPromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prompt",
		Short: "Handle Claude's UserPromptSubmit hook — idle wake-up delivery",
		RunE:  runHookPrompt,
	}
}

func runHookPrompt(cmd *cobra.Command, args []string) error {
	suppressStderr = true
	EnsureStateDirs()
	InitLog()

	// Re-entrancy guard
	if !isMainClaude() {
		LogDebug("Prompt hook: child process, exiting silently", "hook", "prompt")
		return nil
	}

	// Remove idle flag — Claude is processing input
	os.Remove(claudeIdleFile)
	WriteState(StateActive)

	// Check for queued messages
	messages, _ := DrainQueue()
	if len(messages) > 0 {
		LogInfo("Prompt hook: delivering messages via stdout",
			"hook", "prompt",
			"trigger", fmt.Sprintf("queue(%d)", len(messages)))

		// Output to stdout — added as context Claude can see and act on
		fmt.Print(FormatMessages(messages))
	}

	return nil // exit 0 — let the original prompt through
}
