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
	"io"
	"os"

	"github.com/spf13/cobra"
)

func hookPostToolUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "post-tool-use",
		Short: "Handle Claude's PostToolUse/PostToolUseFailure hook — state transition and question cleanup",
		RunE:  runHookPostToolUse,
	}
}

// postToolUseInput represents the JSON structure piped to the hook via stdin
type postToolUseInput struct {
	ToolName string `json:"tool_name"`
}

func runHookPostToolUse(cmd *cobra.Command, args []string) error {
	suppressStderr = true

	// Re-entrancy guard
	if !isMainClaude() {
		return nil // exit 0
	}

	// Parse stdin to get tool name
	stdinData, err := io.ReadAll(os.Stdin)
	if err != nil {
		// Can't read stdin — still transition state
		WriteState(StateActive)
		return nil
	}

	var input postToolUseInput
	json.Unmarshal(stdinData, &input)

	// Claude got its answer — it's working again
	WriteState(StateActive)

	// Clean up question file if this was an AskUserQuestion completion
	if input.ToolName == "AskUserQuestion" {
		os.Remove(currentQuestionFile)
	}

	return nil // exit 0
}
