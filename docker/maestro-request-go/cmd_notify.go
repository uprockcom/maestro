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

const (
	maxTitleLen   = 64
	maxMessageLen = 256
)

func notifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   `notify <title> <message>`,
		Short: "Send a desktop notification to the host",
		Long: fmt.Sprintf(`Send a desktop notification to the host.

Title is limited to %d characters and message to %d characters.
Longer values will be rejected — keep notifications concise.`, maxTitleLen, maxMessageLen),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			title := args[0]
			message := args[1]

			if len(title) > maxTitleLen {
				fmt.Fprintf(os.Stderr, "Error: title too long (%d chars, max %d)\n", len(title), maxTitleLen)
				os.Exit(1)
			}
			if len(message) > maxMessageLen {
				fmt.Fprintf(os.Stderr, "Error: message too long (%d chars, max %d)\n", len(message), maxMessageLen)
				os.Exit(1)
			}

			id, err := generateUUID()
			if err != nil {
				return err
			}

			parent := hostname()

			// Build daemon request JSON
			req := map[string]string{
				"id":      id,
				"action":  "notify",
				"title":   title,
				"message": message,
				"parent":  parent,
			}
			reqJSON, _ := json.Marshal(req)

			// Persist request file
			rf := &RequestFile{
				ID:          id,
				Action:      "notify",
				Title:       title,
				Message:     message,
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
							// Update request file to fulfilled
							fulfilledAt := nowUTC()
							rf.Status = "fulfilled"
							rf.FulfilledAt = &fulfilledAt
							_ = writeRequestFile(rf)
							fmt.Fprintln(cmd.OutOrStdout(), "Notification sent.")
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
				fmt.Fprintf(cmd.OutOrStdout(), "Notification queued (ID: %s)\n", id)
				fmt.Fprintln(cmd.OutOrStdout(), "Daemon unreachable — notification will be delivered when daemon reconnects.")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Notification queued (ID: %s)\n", id)
			fmt.Fprintln(cmd.OutOrStdout(), "Daemon unreachable — notification will be delivered when daemon reconnects.")
			return nil
		},
	}
}
