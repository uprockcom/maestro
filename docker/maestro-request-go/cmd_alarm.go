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
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	maxAlarmNameLen    = 64
	maxAlarmMessageLen = 512
	alarmsDir          = "/home/node/.maestro/alarms"
)

func alarmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "alarm",
		Short: "Manage timed alarms for message delivery",
		Long: `Set, list, or cancel timed alarms. When an alarm fires, a message is
injected into your pending-messages queue, waking you up if idle.

Use alarms to schedule reminders — for example, to check back on a
long-running process, revisit a task after a delay, or wake up at a
specific time.`,
	}

	cmd.AddCommand(alarmSetCmd())
	cmd.AddCommand(alarmListCmd())
	cmd.AddCommand(alarmCancelCmd())

	return cmd
}

func alarmSetCmd() *cobra.Command {
	var message string

	cmd := &cobra.Command{
		Use:   "set <time> <name>",
		Short: "Set a new alarm",
		Long: `Set an alarm that fires at the specified time.

Time formats supported:
  - RFC3339:    2026-02-25T15:30:00Z
  - Date+time:  2026-02-25 15:30       (interpreted as UTC)
  - Relative:   +30m, +2h, +1h30m, +1d (minutes, hours, days from now)

When the alarm fires, a trigger message is injected into the pending-messages
queue. If you are idle, this will wake you up automatically.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			timeStr := args[0]
			name := args[1]

			if name == "" {
				fmt.Fprintln(os.Stderr, "Error: alarm name cannot be empty")
				os.Exit(1)
			}
			if len(name) > maxAlarmNameLen {
				fmt.Fprintf(os.Stderr, "Error: alarm name too long (%d chars, max %d)\n", len(name), maxAlarmNameLen)
				os.Exit(1)
			}
			if len(message) > maxAlarmMessageLen {
				fmt.Fprintf(os.Stderr, "Error: message too long (%d chars, max %d)\n", len(message), maxAlarmMessageLen)
				os.Exit(1)
			}

			// Parse time
			fireAt, err := parseAlarmTime(timeStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			if fireAt.Before(time.Now()) {
				fmt.Fprintf(os.Stderr, "Error: alarm time %s is in the past\n", fireAt.Format(time.RFC3339))
				os.Exit(1)
			}

			id, err := generateUUID()
			if err != nil {
				return err
			}

			parent := hostname()
			fireAtStr := fireAt.UTC().Format(time.RFC3339)

			// Build daemon request JSON
			req := map[string]string{
				"id":            id,
				"action":        "alarm_set",
				"parent":        parent,
				"alarm_name":    name,
				"alarm_time":    fireAtStr,
				"alarm_message": message,
			}
			reqJSON, _ := json.Marshal(req)

			// Persist request file
			rf := &RequestFile{
				ID:           id,
				Action:       "alarm_set",
				Parent:       parent,
				Status:       "pending",
				RequestedAt:  nowUTC(),
				AlarmTime:    fireAtStr,
				AlarmName:    name,
				AlarmMessage: message,
			}
			if err := writeRequestFile(rf); err != nil {
				return err
			}

			// Also persist alarm file locally for daemon restart recovery
			alarm := map[string]interface{}{
				"id":         id,
				"name":       name,
				"message":    message,
				"fire_at":    fireAtStr,
				"created_at": time.Now().UTC().Format(time.RFC3339),
			}
			if err := persistAlarmLocally(id, alarm); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to persist alarm locally: %v\n", err)
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
							fulfilledAt := nowUTC()
							rf.Status = "fulfilled"
							rf.FulfilledAt = &fulfilledAt
							_ = writeRequestFile(rf)
							fmt.Fprintf(cmd.OutOrStdout(), "Alarm set: %s\n", name)
							fmt.Fprintf(cmd.OutOrStdout(), "  ID: %s\n", id)
							fmt.Fprintf(cmd.OutOrStdout(), "  Fires at: %s\n", fireAtStr)
							until := time.Until(fireAt).Round(time.Second)
							fmt.Fprintf(cmd.OutOrStdout(), "  In: %s\n", formatDuration(until))
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
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Alarm queued (ID: %s)\n", id)
			fmt.Fprintf(cmd.OutOrStdout(), "  Fires at: %s\n", fireAtStr)
			fmt.Fprintln(cmd.OutOrStdout(), "Daemon unreachable — alarm will be registered when daemon reconnects.")
			return nil
		},
	}

	cmd.Flags().StringVarP(&message, "message", "m", "", "Message to deliver when alarm fires")

	return cmd
}

func alarmListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List pending alarms",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := generateUUID()
			if err != nil {
				return err
			}

			parent := hostname()

			req := map[string]string{
				"id":     id,
				"action": "alarm_list",
				"parent": parent,
			}
			reqJSON, _ := json.Marshal(req)

			// Persist request file
			rf := &RequestFile{
				ID:          id,
				Action:      "alarm_list",
				Parent:      parent,
				Status:      "pending",
				RequestedAt: nowUTC(),
			}
			if err := writeRequestFile(rf); err != nil {
				return err
			}

			if !daemonAvailable() {
				fmt.Fprintln(os.Stderr, "Error: daemon unreachable — cannot list alarms")
				os.Exit(1)
			}

			resp, err := daemonCall("POST", "/request", string(reqJSON))
			if err != nil {
				return fmt.Errorf("daemon call failed: %w", err)
			}

			var result struct {
				Status string `json:"status"`
				Error  string `json:"error,omitempty"`
				Alarms []struct {
					ID      string `json:"id"`
					Name    string `json:"name"`
					Message string `json:"message,omitempty"`
					FireAt  string `json:"fire_at"`
				} `json:"alarms"`
			}
			if err := json.Unmarshal([]byte(resp), &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if result.Status == "error" {
				fmt.Fprintf(os.Stderr, "Error: %s\n", result.Error)
				os.Exit(1)
			}

			// Update request file
			fulfilledAt := nowUTC()
			rf.Status = "fulfilled"
			rf.FulfilledAt = &fulfilledAt
			_ = writeRequestFile(rf)

			if len(result.Alarms) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No pending alarms.")
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Pending alarms (%d):\n", len(result.Alarms))
			for _, a := range result.Alarms {
				fireAt, _ := time.Parse(time.RFC3339, a.FireAt)
				until := time.Until(fireAt).Round(time.Second)
				fmt.Fprintf(cmd.OutOrStdout(), "  - %s (ID: %s)\n", a.Name, a.ID)
				fmt.Fprintf(cmd.OutOrStdout(), "    Fires at: %s (in %s)\n", a.FireAt, formatDuration(until))
				if a.Message != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "    Message: %s\n", a.Message)
				}
			}

			// Also print as JSON for machine consumption
			alarmsJSON, _ := json.MarshalIndent(result.Alarms, "", "  ")
			fmt.Fprintf(cmd.OutOrStdout(), "\n%s\n", string(alarmsJSON))
			return nil
		},
	}
}

func alarmCancelCmd() *cobra.Command {
	var byName string

	cmd := &cobra.Command{
		Use:   "cancel [alarm-id]",
		Short: "Cancel a pending alarm",
		Long: `Cancel a pending alarm by ID or name.

Use --name to cancel by alarm name instead of ID.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var alarmID, alarmName string
			if len(args) == 1 {
				alarmID = args[0]
			}
			if byName != "" {
				alarmName = byName
			}
			if alarmID == "" && alarmName == "" {
				fmt.Fprintln(os.Stderr, "Error: provide an alarm ID or --name")
				os.Exit(1)
			}

			id, err := generateUUID()
			if err != nil {
				return err
			}

			parent := hostname()

			req := map[string]string{
				"id":     id,
				"action": "alarm_cancel",
				"parent": parent,
			}
			if alarmID != "" {
				req["alarm_id"] = alarmID
			}
			if alarmName != "" {
				req["alarm_name"] = alarmName
			}
			reqJSON, _ := json.Marshal(req)

			// Persist request file
			rf := &RequestFile{
				ID:          id,
				Action:      "alarm_cancel",
				Parent:      parent,
				Status:      "pending",
				RequestedAt: nowUTC(),
			}
			if err := writeRequestFile(rf); err != nil {
				return err
			}

			if !daemonAvailable() {
				fmt.Fprintln(os.Stderr, "Error: daemon unreachable — cannot cancel alarm")
				os.Exit(1)
			}

			resp, err := daemonCall("POST", "/request", string(reqJSON))
			if err != nil {
				return fmt.Errorf("daemon call failed: %w", err)
			}

			var result map[string]interface{}
			if err := json.Unmarshal([]byte(resp), &result); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			status, _ := result["status"].(string)
			switch status {
			case "ok":
				fulfilledAt := nowUTC()
				rf.Status = "fulfilled"
				rf.FulfilledAt = &fulfilledAt
				_ = writeRequestFile(rf)

				// Remove local alarm file. For cancel-by-name the daemon
				// returns the resolved alarm_id so we can clean up.
				cancelledID, _ := result["alarm_id"].(string)
				if cancelledID != "" {
					removeAlarmLocally(cancelledID)
				} else if alarmID != "" {
					removeAlarmLocally(alarmID)
				}

				fmt.Fprintln(cmd.OutOrStdout(), "Alarm cancelled.")
				return nil
			case "error":
				errMsg, _ := result["error"].(string)
				if errMsg == "" {
					errMsg = "unknown error"
				}
				fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
				os.Exit(1)
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&byName, "name", "", "Cancel alarm by name instead of ID")

	return cmd
}

// parseAlarmTime parses various time formats into a time.Time
func parseAlarmTime(s string) (time.Time, error) {
	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}

	// Try date+time format "2006-01-02 15:04"
	if t, err := time.Parse("2006-01-02 15:04", s); err == nil {
		return t.UTC(), nil
	}

	// Try date+time+seconds "2006-01-02 15:04:05"
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC(), nil
	}

	// Try relative time: +30m, +2h, +1h30m, +1d
	if strings.HasPrefix(s, "+") {
		return parseRelativeTime(s[1:])
	}

	return time.Time{}, fmt.Errorf("unrecognized time format %q (use RFC3339, \"2006-01-02 15:04\" or \"2006-01-02 15:04:05\" interpreted as UTC, or relative like +30m, +2h, +1d)", s)
}

// parseRelativeTime handles relative duration strings like "30m", "2h", "1h30m", "1d"
func parseRelativeTime(s string) (time.Time, error) {
	// Handle days specially since time.ParseDuration doesn't support 'd'
	if strings.Contains(s, "d") {
		days := 0
		remaining := s
		dIdx := strings.Index(s, "d")
		if dIdx == 0 {
			return time.Time{}, fmt.Errorf("invalid relative time %q: missing number before 'd'", s)
		}
		if dIdx > 0 {
			n := 0
			for _, c := range s[:dIdx] {
				if c >= '0' && c <= '9' {
					n = n*10 + int(c-'0')
				} else {
					return time.Time{}, fmt.Errorf("invalid relative time %q", s)
				}
			}
			days = n
			remaining = s[dIdx+1:]
		}

		var dur time.Duration
		if remaining != "" {
			var err error
			dur, err = time.ParseDuration(remaining)
			if err != nil {
				return time.Time{}, fmt.Errorf("invalid relative time %q: %w", s, err)
			}
		}

		return time.Now().Add(time.Duration(days)*24*time.Hour + dur), nil
	}

	dur, err := time.ParseDuration(s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid relative time %q: %w", s, err)
	}
	return time.Now().Add(dur), nil
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	if d < 0 {
		return "now"
	}

	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60

	var parts []string
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	return strings.Join(parts, "")
}

// persistAlarmLocally saves an alarm to the local filesystem for daemon restart recovery
func persistAlarmLocally(id string, alarm map[string]interface{}) error {
	if err := os.MkdirAll(alarmsDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(alarm, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(fmt.Sprintf("%s/%s.json", alarmsDir, id), data, 0644)
}

// removeAlarmLocally deletes a local alarm file
func removeAlarmLocally(id string) {
	os.Remove(fmt.Sprintf("%s/%s.json", alarmsDir, id))
}
