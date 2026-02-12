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

package daemon

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// maxMessageContentSize is the maximum size of a single message's content (10KB)
const maxMessageContentSize = 10 * 1024

// readClaudeMessages reads the last `count` messages from a child container's Claude session.
// It finds the most recent JSONL session file, tails the last ~100KB, and parses user/assistant messages.
func (s *IPCServer) readClaudeMessages(containerName string, count int) ([]IPCMessage, error) {
	// 1. Find most recent JSONL session file
	findCmd := exec.Command("docker", "exec", containerName,
		"sh", "-c", "ls -t /home/node/.claude/projects/-workspace/*.jsonl 2>/dev/null | head -1")
	findOutput, err := findCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("no Claude session files found")
	}

	sessionFile := strings.TrimSpace(string(findOutput))
	if sessionFile == "" {
		return nil, fmt.Errorf("no Claude session files found")
	}

	// 2. Tail last ~100KB to avoid reading multi-MB files
	tailCmd := exec.Command("docker", "exec", containerName,
		"sh", "-c", fmt.Sprintf("tail -c 102400 '%s'", sessionFile))
	tailOutput, err := tailCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to read session file: %w", err)
	}

	// 3. Split lines, skip first (likely truncated by tail -c)
	lines := strings.Split(string(tailOutput), "\n")
	if len(lines) > 1 {
		lines = lines[1:] // skip first potentially truncated line
	}

	// 4. Parse each line and extract messages
	var messages []IPCMessage
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		msgType, _ := entry["type"].(string)
		if msgType != "user" && msgType != "assistant" {
			continue
		}

		// For user messages, skip tool use results
		if msgType == "user" {
			if _, hasToolResult := entry["toolUseResult"]; hasToolResult {
				continue
			}
		}

		// Extract text content
		content := extractTextContent(entry, msgType)
		if content == "" {
			continue
		}

		// Truncate at 10KB
		if len(content) > maxMessageContentSize {
			content = content[:maxMessageContentSize] + "... (truncated)"
		}

		// Extract timestamp
		timestamp := ""
		if ts, ok := entry["timestamp"].(string); ok {
			timestamp = ts
		} else {
			timestamp = time.Now().UTC().Format(time.RFC3339)
		}

		role := msgType
		if msgType == "user" {
			// Map to simpler role names
			role = "user"
		}

		messages = append(messages, IPCMessage{
			Role:      role,
			Content:   content,
			Timestamp: timestamp,
		})
	}

	// Return last `count` messages
	if len(messages) > count {
		messages = messages[len(messages)-count:]
	}

	return messages, nil
}

// extractTextContent extracts human-readable text from a JSONL entry's message field.
func extractTextContent(entry map[string]interface{}, msgType string) string {
	msg, ok := entry["message"].(map[string]interface{})
	if !ok {
		return ""
	}

	content := msg["content"]

	// String content (simple user messages)
	if str, ok := content.(string); ok {
		return str
	}

	// Array content (structured messages with text blocks)
	if arr, ok := content.([]interface{}); ok {
		var texts []string
		for _, block := range arr {
			blockMap, ok := block.(map[string]interface{})
			if !ok {
				continue
			}

			blockType, _ := blockMap["type"].(string)

			if msgType == "assistant" {
				// For assistant: only extract text blocks
				if blockType == "text" {
					if text, ok := blockMap["text"].(string); ok && text != "" {
						texts = append(texts, text)
					}
				}
			} else {
				// For user: extract text blocks
				if blockType == "text" {
					if text, ok := blockMap["text"].(string); ok && text != "" {
						texts = append(texts, text)
					}
				}
			}
		}
		return strings.Join(texts, "\n")
	}

	return ""
}
