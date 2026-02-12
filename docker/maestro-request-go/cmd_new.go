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
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newCmd() *cobra.Command {
	var branch string

	cmd := &cobra.Command{
		Use:   "new <task description>",
		Short: "Spawn a sibling container for a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			task := args[0]

			id, err := generateUUID()
			if err != nil {
				return err
			}

			parent := hostname()

			// Build daemon request JSON
			req := map[string]string{
				"id":     id,
				"action": "new",
				"task":   task,
				"parent": parent,
			}
			if branch != "" {
				req["branch"] = branch
			}
			reqJSON, _ := json.Marshal(req)

			// Persist request file
			rf := &RequestFile{
				ID:          id,
				Action:      "new",
				Task:        task,
				Parent:      parent,
				Branch:      branch,
				Status:      "pending",
				RequestedAt: nowUTC(),
			}
			if err := writeRequestFile(rf); err != nil {
				return err
			}

			// Attempt to send via daemon
			if daemonAvailable() {
				resp, err := daemonCall("POST", "/request", string(reqJSON))
				if err == nil && resp != "" {
					var result map[string]interface{}
					if json.Unmarshal([]byte(resp), &result) == nil {
						status, _ := result["status"].(string)
						switch status {
						case "accepted":
							fmt.Fprintf(cmd.OutOrStdout(), "Request accepted (ID: %s)\n", id)
							return nil
						case "error":
							errMsg, _ := result["error"].(string)
							if errMsg == "" {
								errMsg = "unknown error"
							}
							fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
							os.Exit(1)
						}
					}
				}
				// Daemon unreachable despite config existing
				fmt.Fprintf(cmd.OutOrStdout(), "Request queued (ID: %s)\n", id)
				fmt.Fprintln(cmd.OutOrStdout(), "Daemon unreachable — request will be processed when daemon reconnects.")
				return nil
			}

			// No daemon configured
			fmt.Fprintf(cmd.OutOrStdout(), "Request queued (ID: %s)\n", id)
			fmt.Fprintln(cmd.OutOrStdout(), "Daemon unreachable — request will be processed when daemon reconnects.")
			return nil
		},
	}

	cmd.Flags().StringVar(&branch, "branch", "", "Start child from a specific branch")
	return cmd
}
