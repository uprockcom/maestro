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

package container

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/uprockcom/maestro/pkg/notify"
)

// InjectTextToContainer sends text into a container's Claude tmux pane.
// It writes the message to a temp file, loads it into a tmux buffer, pastes
// it into window 0 of the "main" session, presses Enter, and cleans up.
func InjectTextToContainer(containerName, text string) error {
	// Write message to temp file in container
	writeCmd := exec.Command("docker", "exec", "-i", containerName, "tee", "/tmp/maestro-msg")
	writeCmd.Stdin = strings.NewReader(text)
	writeCmd.Stdout = nil // suppress tee echo
	if err := writeCmd.Run(); err != nil {
		return fmt.Errorf("failed to write message to container: %w", err)
	}

	// Load into tmux buffer
	loadCmd := exec.Command("docker", "exec", containerName, "tmux", "load-buffer", "/tmp/maestro-msg")
	if err := loadCmd.Run(); err != nil {
		return fmt.Errorf("failed to load tmux buffer: %w", err)
	}

	// Paste into Claude pane
	pasteCmd := exec.Command("docker", "exec", containerName, "tmux", "paste-buffer", "-t", "main:0", "-d")
	if err := pasteCmd.Run(); err != nil {
		return fmt.Errorf("failed to paste message: %w", err)
	}

	// Press enter
	enterCmd := exec.Command("docker", "exec", containerName, "tmux", "send-keys", "-t", "main:0", "C-m")
	if err := enterCmd.Run(); err != nil {
		return fmt.Errorf("failed to send enter key: %w", err)
	}

	// Clean up temp file (best-effort)
	cleanCmd := exec.Command("docker", "exec", containerName, "rm", "-f", "/tmp/maestro-msg")
	_ = cleanCmd.Run()

	// Remove idle flag proactively (prevents race with hooks)
	rmIdleCmd := exec.Command("docker", "exec", containerName, "rm", "-f", "/home/node/.maestro/claude-idle")
	_ = rmIdleCmd.Run()

	return nil
}

// QueueMessage writes a message to the container's pending-messages queue.
// The maestro-agent hook handlers pick up queued messages and feed them to
// Claude via stderr (exit code 2) on the next Stop or UserPromptSubmit event.
// If Claude is in blocking Stop hook wait, the queue watcher detects the
// message within ~1 second. If Claude is idle, the maestro-agent service
// handles wake-up automatically.
func QueueMessage(containerName, message string) error {
	// Generate filename with timestamp for ordering
	ts := fmt.Sprintf("%d", time.Now().UnixNano())
	filename := fmt.Sprintf("/home/node/.maestro/pending-messages/%s.txt", ts)

	// Write message to queue
	writeCmd := exec.Command("docker", "exec", "-i", containerName, "tee", filename)
	writeCmd.Stdin = strings.NewReader(message)
	writeCmd.Stdout = nil
	if err := writeCmd.Run(); err != nil {
		return fmt.Errorf("failed to queue message: %w", err)
	}

	return nil
}

// WriteQuestionResponse writes an answer to a container's question-response.txt file.
// The maestro-agent ask hook polls for this file and feeds its contents to
// Claude via stderr (exit code 2), allowing the Maestro notification system to
// answer AskUserQuestion prompts without keystroke injection.
//
// selections contains one answer per question (accumulated by the TUI). For
// single-select questions, each entry is the selected option label. For the last
// multi-select question, all remaining entries are the selected labels.
func WriteQuestionResponse(containerName string, selections []string, text string) error {
	// Read question data to format the answer with context
	qd, _ := notify.ReadContainerQuestion(containerName)
	answer := formatQuestionAnswer(qd, selections, text)

	writeCmd := exec.Command("docker", "exec", "-i", containerName,
		"tee", "/home/node/.maestro/question-response.txt")
	writeCmd.Stdin = strings.NewReader(answer)
	writeCmd.Stdout = nil // suppress tee echo
	if err := writeCmd.Run(); err != nil {
		return fmt.Errorf("failed to write question response: %w", err)
	}
	return nil
}

// formatQuestionAnswer builds a human-readable answer string that Claude can
// understand when it receives it via the hook's stderr output.
func formatQuestionAnswer(qd *notify.QuestionData, selections []string, text string) string {
	if qd == nil || len(qd.Questions) == 0 {
		// No question data — return a simple answer
		if text != "" {
			return "The user's answer: " + text
		}
		return "The user's answer: " + strings.Join(selections, ", ")
	}

	var sb strings.Builder
	sb.WriteString("The user answered your question(s) via the Maestro notification system:\n")

	selIdx := 0
	for qi, q := range qd.Questions {
		sb.WriteString(fmt.Sprintf("- %s: ", q.Header))

		if qi == len(qd.Questions)-1 {
			// Last question — all remaining selections belong to it
			remaining := selections[selIdx:]
			if len(remaining) > 0 {
				sb.WriteString(strings.Join(remaining, ", "))
			}
			if text != "" {
				if len(remaining) > 0 {
					sb.WriteString("; additional text: ")
				}
				sb.WriteString(text)
			}
			if len(remaining) == 0 && text == "" {
				sb.WriteString("(no selection)")
			}
		} else if selIdx < len(selections) {
			sb.WriteString(selections[selIdx])
			selIdx++
		} else {
			sb.WriteString("(no selection)")
		}

		sb.WriteString("\n")
	}

	return sb.String()
}
