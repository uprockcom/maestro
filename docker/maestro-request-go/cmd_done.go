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

func doneCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "done",
		Short: "Signal task completion and stop this container",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := generateUUID()
			if err != nil {
				return err
			}

			parent := hostname()

			// Build daemon request JSON — wire protocol still uses "exit"
			req := map[string]string{
				"id":     id,
				"action": "exit",
				"parent": parent,
			}
			reqJSON, _ := json.Marshal(req)

			// Persist request file
			rf := &RequestFile{
				ID:          id,
				Action:      "exit",
				Parent:      parent,
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
						case "ok":
							fmt.Fprintln(cmd.OutOrStdout(), "Exit signal sent. Container will stop shortly.")
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
				fmt.Fprintf(cmd.OutOrStdout(), "Exit request queued (ID: %s)\n", id)
				fmt.Fprintln(cmd.OutOrStdout(), "Daemon unreachable — request will be processed when daemon reconnects.")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Exit request queued (ID: %s)\n", id)
			fmt.Fprintln(cmd.OutOrStdout(), "Daemon unreachable — request will be processed when daemon reconnects.")
			return nil
		},
	}
}
