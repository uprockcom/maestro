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
	"time"

	"github.com/spf13/cobra"
)

func requestCmd() *cobra.Command {
	var timeout int

	cmd := &cobra.Command{
		Use:   "request <type> <value>",
		Short: "Request resources or firewall access from the host",
		Long: `Request additional resources or firewall access. Sends a notification
to the host user for approval. Blocks until approved or denied.

Types:
  domain <domain>  - Request firewall access to a domain
  memory <amount>  - Request memory increase (e.g., 14g)
  cpus <count>     - Request CPU increase (e.g., 12)
  ip <address>     - Request firewall access to an IP address`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			requestType := args[0]
			requestValue := args[1]

			validTypes := map[string]bool{"domain": true, "memory": true, "cpus": true, "ip": true}
			if !validTypes[requestType] {
				return fmt.Errorf("invalid request type %q (valid: domain, memory, cpus, ip)", requestType)
			}

			id, err := generateUUID()
			if err != nil {
				return err
			}

			parent := hostname()

			// Build daemon request JSON
			req := map[string]string{
				"id":            id,
				"action":        "request",
				"parent":        parent,
				"request_type":  requestType,
				"request_value": requestValue,
			}
			reqJSON, _ := json.Marshal(req)

			// Persist request file locally
			rf := &RequestFile{
				ID:           id,
				Action:       "request",
				Parent:       parent,
				Status:       "pending",
				RequestedAt:  nowUTC(),
				RequestType:  requestType,
				RequestValue: requestValue,
			}
			if err := writeRequestFile(rf); err != nil {
				return err
			}

			// Send to daemon
			if !daemonAvailable() {
				fmt.Fprintf(cmd.OutOrStdout(), "Request queued (ID: %s)\n", id)
				fmt.Fprintln(cmd.OutOrStdout(), "Daemon unreachable — request will be processed when daemon reconnects.")
				return nil
			}

			resp, err := daemonCall("POST", "/request", string(reqJSON))
			if err != nil {
				fmt.Fprintf(cmd.OutOrStdout(), "Request queued (ID: %s)\n", id)
				fmt.Fprintln(cmd.OutOrStdout(), "Daemon unreachable — request will be processed when daemon reconnects.")
				return nil
			}

			var result map[string]interface{}
			if err := json.Unmarshal([]byte(resp), &result); err == nil {
				if status, _ := result["status"].(string); status == "error" {
					errMsg, _ := result["error"].(string)
					return fmt.Errorf("daemon error: %s", errMsg)
				}
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Request submitted for %s=%s (ID: %s)\n", requestType, requestValue, id)
			fmt.Fprintln(cmd.OutOrStdout(), "Waiting for host approval...")

			// Poll for result
			deadline := time.Now().Add(time.Duration(timeout) * time.Second)
			for time.Now().Before(deadline) {
				time.Sleep(2 * time.Second)
				r, err := readRequestFile(id)
				if err != nil {
					continue
				}

				switch r.Status {
				case "fulfilled":
					fmt.Fprintln(cmd.OutOrStdout(), "Request approved!")
					return nil
				case "failed":
					errMsg := "request denied"
					if r.Error != nil {
						errMsg = *r.Error
					}
					fmt.Fprintf(os.Stderr, "Request denied: %s\n", errMsg)
					os.Exit(1)
				}
			}

			fmt.Fprintf(os.Stderr, "Timeout: no response within %ds\n", timeout)
			os.Exit(1)
			return nil
		},
	}

	cmd.Flags().IntVar(&timeout, "timeout", 300, "Timeout in seconds to wait for approval")
	return cmd
}
