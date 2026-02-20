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
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func claudeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "claude",
		Short: "Interact with a child container's Claude session",
	}

	cmd.AddCommand(claudeReadCmd())
	cmd.AddCommand(claudeSendCmd())
	cmd.AddCommand(claudeAnswerCmd())
	return cmd
}

func claudeReadCmd() *cobra.Command {
	var last int

	cmd := &cobra.Command{
		Use:   "read <request-id>",
		Short: "Read messages from a child container's Claude session",
		Long: `Read the last N messages from a child container's Claude session.

The request-id must be from a "maestro-request new" command that created the child.
Prints messages in human-readable format, followed by JSON to stdout.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetRequestID := args[0]

			id, err := generateUUID()
			if err != nil {
				return err
			}

			parent := hostname()

			reqJSON, _ := json.Marshal(map[string]interface{}{
				"id":                id,
				"action":            "read_messages",
				"parent":            parent,
				"target_request_id": targetRequestID,
				"count":             last,
			})

			rf := &RequestFile{
				ID:              id,
				Action:          "read_messages",
				Parent:          parent,
				Status:          "pending",
				RequestedAt:     nowUTC(),
				TargetRequestID: targetRequestID,
				Count:           last,
			}
			if err := writeRequestFile(rf); err != nil {
				return err
			}

			if !daemonAvailable() {
				fmt.Fprintf(os.Stderr, "Error: daemon not available\n")
				os.Exit(1)
			}

			resp, err := daemonCall("POST", "/request", string(reqJSON))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			var result map[string]interface{}
			if json.Unmarshal([]byte(resp), &result) == nil {
				if status, _ := result["status"].(string); status == "error" {
					errMsg, _ := result["error"].(string)
					fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
					os.Exit(1)
				}
			}

			// Poll until fulfilled or failed
			deadline := time.Now().Add(60 * time.Second)
			for {
				r, err := readRequestFile(id)
				if err == nil {
					switch r.Status {
					case "fulfilled":
						// Print human-readable format
						for _, msg := range r.Messages {
							fmt.Fprintf(cmd.OutOrStdout(), "[%s] %s\n", msg.Role, msg.Timestamp)
							fmt.Fprintln(cmd.OutOrStdout(), msg.Content)
							fmt.Fprintln(cmd.OutOrStdout())
						}
						// Show pending question if present
						if len(r.PendingQuestion) > 0 {
							fmt.Fprintln(cmd.OutOrStdout(), "--- PENDING QUESTION ---")
							var pq map[string]interface{}
							if json.Unmarshal(r.PendingQuestion, &pq) == nil {
								pqJSON, _ := json.MarshalIndent(pq, "", "  ")
								fmt.Fprintln(cmd.OutOrStdout(), string(pqJSON))
							}
							fmt.Fprintln(cmd.OutOrStdout())
						}
						// Also output full JSON
						data, _ := json.MarshalIndent(r, "", "  ")
						fmt.Fprintln(cmd.OutOrStdout(), string(data))
						return nil
					case "failed":
						data, _ := json.MarshalIndent(r, "", "  ")
						fmt.Fprintln(cmd.OutOrStdout(), string(data))
						os.Exit(1)
					}
				}

				if time.Now().After(deadline) {
					fmt.Fprintf(os.Stderr, "Timeout: read_messages did not complete within 60s\n")
					os.Exit(1)
				}

				time.Sleep(defaultPollInterval)
			}
		},
	}

	cmd.Flags().IntVar(&last, "last", 10, "Number of messages to read (max 50)")
	return cmd
}

func claudeSendCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "send <request-id> <message>",
		Short: "Send a message to a child container's Claude session",
		Long: `Send a message to a child container's Claude session.

The request-id must be from a "maestro-request new" command that created the child.
The message is typed into the Claude pane and Enter is pressed.`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetRequestID := args[0]
			message := strings.Join(args[1:], " ")

			id, err := generateUUID()
			if err != nil {
				return err
			}

			parent := hostname()

			reqJSON, _ := json.Marshal(map[string]interface{}{
				"id":                id,
				"action":            "send_message",
				"parent":            parent,
				"target_request_id": targetRequestID,
				"message":           message,
			})

			rf := &RequestFile{
				ID:              id,
				Action:          "send_message",
				Parent:          parent,
				Status:          "pending",
				RequestedAt:     nowUTC(),
				Message:         message,
				TargetRequestID: targetRequestID,
			}
			if err := writeRequestFile(rf); err != nil {
				return err
			}

			if !daemonAvailable() {
				fmt.Fprintf(os.Stderr, "Error: daemon not available\n")
				os.Exit(1)
			}

			resp, err := daemonCall("POST", "/request", string(reqJSON))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			var result map[string]interface{}
			if json.Unmarshal([]byte(resp), &result) == nil {
				if status, _ := result["status"].(string); status == "error" {
					errMsg, _ := result["error"].(string)
					fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
					os.Exit(1)
				}
			}

			// Poll until fulfilled or failed
			deadline := time.Now().Add(30 * time.Second)
			for {
				r, err := readRequestFile(id)
				if err == nil {
					switch r.Status {
					case "fulfilled":
						fmt.Fprintln(cmd.OutOrStdout(), "Message sent successfully.")
						return nil
					case "failed":
						data, _ := json.MarshalIndent(r, "", "  ")
						fmt.Fprintln(cmd.OutOrStdout(), string(data))
						os.Exit(1)
					}
				}

				if time.Now().After(deadline) {
					fmt.Fprintf(os.Stderr, "Timeout: send_message did not complete within 30s\n")
					os.Exit(1)
				}

				time.Sleep(defaultPollInterval)
			}
		},
	}

	return cmd
}

func claudeAnswerCmd() *cobra.Command {
	var selections []string
	var text string

	cmd := &cobra.Command{
		Use:   "answer <request-id>",
		Short: "Answer a pending question in a child container's Claude session",
		Long: `Answer a pending AskUserQuestion prompt in a child container.

The request-id must be from a "maestro-request new" command that created the child.
Use --select to choose option(s) and/or --text for freeform input.

Examples:
  maestro-request claude answer <id> --select "Option A"
  maestro-request claude answer <id> --select "Option A" --select "Option B"
  maestro-request claude answer <id> --text "Custom answer"
  maestro-request claude answer <id> --select "Other" --text "Details here"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetRequestID := args[0]

			if len(selections) == 0 && text == "" {
				fmt.Fprintf(os.Stderr, "Error: at least one --select or --text is required\n")
				os.Exit(1)
			}

			id, err := generateUUID()
			if err != nil {
				return err
			}

			parent := hostname()

			reqJSON, _ := json.Marshal(map[string]interface{}{
				"id":                id,
				"action":            "answer_question",
				"parent":            parent,
				"target_request_id": targetRequestID,
				"selections":        selections,
				"message":           text,
			})

			rf := &RequestFile{
				ID:              id,
				Action:          "answer_question",
				Parent:          parent,
				Status:          "pending",
				RequestedAt:     nowUTC(),
				TargetRequestID: targetRequestID,
				Message:         text,
			}
			if err := writeRequestFile(rf); err != nil {
				return err
			}

			if !daemonAvailable() {
				fmt.Fprintf(os.Stderr, "Error: daemon not available\n")
				os.Exit(1)
			}

			resp, err := daemonCall("POST", "/request", string(reqJSON))
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			var result map[string]interface{}
			if json.Unmarshal([]byte(resp), &result) == nil {
				if status, _ := result["status"].(string); status == "error" {
					errMsg, _ := result["error"].(string)
					fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
					os.Exit(1)
				}
			}

			// Poll until fulfilled or failed
			deadline := time.Now().Add(30 * time.Second)
			for {
				r, err := readRequestFile(id)
				if err == nil {
					switch r.Status {
					case "fulfilled":
						fmt.Fprintln(cmd.OutOrStdout(), "Question answered successfully.")
						return nil
					case "failed":
						data, _ := json.MarshalIndent(r, "", "  ")
						fmt.Fprintln(cmd.OutOrStdout(), string(data))
						os.Exit(1)
					}
				}

				if time.Now().After(deadline) {
					fmt.Fprintf(os.Stderr, "Timeout: answer_question did not complete within 30s\n")
					os.Exit(1)
				}

				time.Sleep(defaultPollInterval)
			}
		},
	}

	cmd.Flags().StringArrayVar(&selections, "select", nil, "Selected option label (repeatable for multi-select)")
	cmd.Flags().StringVar(&text, "text", "", "Freeform text answer (for 'Other' or standalone)")
	return cmd
}
