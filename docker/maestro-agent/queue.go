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
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// QueuedMessage represents a message from the pending-messages directory
type QueuedMessage struct {
	Filename  string
	Content   string
	Timestamp time.Time
}

// DrainQueue reads and removes all pending messages, returning them in order.
// This is atomic in the sense that files are read and removed in one pass.
func DrainQueue() ([]QueuedMessage, error) {
	entries, err := os.ReadDir(pendingMsgDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var messages []QueuedMessage
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}

		path := filepath.Join(pendingMsgDir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			continue // skip unreadable files
		}

		// Parse timestamp from filename (nanosecond unix timestamp)
		ts := time.Now() // fallback
		name := strings.TrimSuffix(entry.Name(), ".txt")
		if parsed, err := parseTimestampFilename(name); err == nil {
			ts = parsed
		}

		messages = append(messages, QueuedMessage{
			Filename:  entry.Name(),
			Content:   string(content),
			Timestamp: ts,
		})

		// Remove the file after reading
		os.Remove(path)
	}

	// Sort by timestamp
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	return messages, nil
}

// HasQueuedMessages checks if there are any pending messages without consuming them
func HasQueuedMessages() bool {
	entries, err := os.ReadDir(pendingMsgDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".txt") {
			return true
		}
	}
	return false
}

// FormatMessages formats queued messages for delivery to Claude
func FormatMessages(messages []QueuedMessage) string {
	if len(messages) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== MESSAGE SOURCE: queue (%d message", len(messages)))
	if len(messages) != 1 {
		sb.WriteString("s")
	}
	sb.WriteString(") ===\n")

	for i, msg := range messages {
		sb.WriteString(fmt.Sprintf("\n--- Message %d [%s] ---\n",
			i+1, msg.Timestamp.Format(time.RFC3339)))
		sb.WriteString(strings.TrimSpace(msg.Content))
		sb.WriteString("\n")
	}

	sb.WriteString("\n=== END MESSAGES ===\n")
	return sb.String()
}

// FormatTrigger formats a trigger message for delivery to Claude
func FormatTrigger(name, content string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== TRIGGER: %s ===\n", name))
	sb.WriteString(strings.TrimSpace(content))
	sb.WriteString("\n=== END TRIGGER ===\n")
	return sb.String()
}

func parseTimestampFilename(name string) (time.Time, error) {
	// Try parsing as nanosecond unix timestamp
	var ns int64
	if _, err := fmt.Sscanf(name, "%d", &ns); err != nil {
		return time.Time{}, err
	}
	return time.Unix(0, ns), nil
}
