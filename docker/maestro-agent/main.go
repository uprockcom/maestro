// Copyright 2025 Christopher O'Connell
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

func main() {
	root := &cobra.Command{
		Use:   "maestro-agent",
		Short: "Container-side agent process manager for Maestro",
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	hookCmd := &cobra.Command{
		Use:   "hook",
		Short: "Hook handlers called by Claude Code",
	}
	hookCmd.AddCommand(hookStopCmd())
	hookCmd.AddCommand(hookSessionStartCmd())
	hookCmd.AddCommand(hookPromptCmd())
	hookCmd.AddCommand(hookPreToolUseCmd())
	hookCmd.AddCommand(hookAskCmd())
	hookCmd.AddCommand(hookPostToolUseCmd())

	root.AddCommand(hookCmd)
	root.AddCommand(serviceCmd())
	root.AddCommand(statusCmd())
	root.AddCommand(bootstrapCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
