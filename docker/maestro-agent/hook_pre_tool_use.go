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
	"github.com/spf13/cobra"
)

var preToolUseIdle bool

func hookPreToolUseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pre-tool-use",
		Short: "Handle Claude's PreToolUse hook — re-entrancy guard and state management",
		RunE:  runHookPreToolUse,
	}
	cmd.Flags().BoolVar(&preToolUseIdle, "idle", false, "Set idle state (for user-blocking tools like AskUserQuestion)")
	return cmd
}

func runHookPreToolUse(cmd *cobra.Command, args []string) error {
	suppressStderr = true

	// Re-entrancy guard: if this is a child Claude process, exit silently
	if !isMainClaude() {
		return nil // exit 0
	}

	if preToolUseIdle {
		// User-blocking tool (AskUserQuestion, EnterPlanMode, ExitPlanMode)
		// Claude is waiting for user input
		WriteState(StateIdle)
	} else {
		// Normal tool use — Claude is actively working
		WriteState(StateActive)
	}

	return nil // exit 0
}
