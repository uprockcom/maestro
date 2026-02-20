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

package signal

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/uprockcom/maestro/pkg/notify"
)

// FormatEvent formats a notification event for Signal delivery.
// Questions are rendered with numbered options; other events are plain text.
func FormatEvent(event notify.Event) string {
	if event.Question != nil && len(event.Question.Questions) > 0 {
		return formatQuestion(event)
	}
	if event.ShortName != "" {
		return fmt.Sprintf("[maestro] %s — %s\n%s", event.Title, event.ShortName, event.Message)
	}
	return fmt.Sprintf("[maestro] %s\n%s", event.Title, event.Message)
}

func formatQuestion(event notify.Event) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("[maestro] %s is asking:\n\n", event.ShortName))

	for _, q := range event.Question.Questions {
		b.WriteString(q.Question)
		b.WriteString("\n\n")

		for i, opt := range q.Options {
			if opt.Description != "" {
				b.WriteString(fmt.Sprintf("%d. %s — %s\n", i+1, opt.Label, opt.Description))
			} else {
				b.WriteString(fmt.Sprintf("%d. %s\n", i+1, opt.Label))
			}
		}
	}

	b.WriteString("\nReply with a number (or type your answer):")
	return b.String()
}

// ParseResponse parses a Signal reply into a Response. A single digit selects
// the corresponding option label; anything else is treated as free-text.
func ParseResponse(text string, qd *notify.QuestionData) notify.Response {
	text = strings.TrimSpace(text)

	if qd != nil && len(qd.Questions) > 0 {
		q := qd.Questions[0]
		if n, err := strconv.Atoi(text); err == nil && n >= 1 && n <= len(q.Options) {
			return notify.Response{
				Selections: []string{q.Options[n-1].Label},
			}
		}
	}

	return notify.Response{
		Text: text,
	}
}

// FormatResolved returns a follow-up message indicating the question was
// answered via another provider.
func FormatResolved(provider string) string {
	return fmt.Sprintf("[maestro] Question resolved (answered via %s)", provider)
}

// FormatContainerList formats a list of container summaries for Signal.
func FormatContainerList(containers []notify.ContainerSummary) string {
	if len(containers) == 0 {
		return "[maestro] No running containers"
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("[maestro] %d container(s):\n", len(containers)))

	for _, c := range containers {
		// Status emoji
		var icon string
		switch c.Status {
		case "working":
			icon = "⚡"
		case "idle":
			icon = "⏸"
		case "dormant":
			icon = "💤"
		case "question":
			icon = "❓"
		default:
			icon = "•"
		}

		name := c.ShortName
		if c.Nickname != "" {
			name = fmt.Sprintf("%s (%s)", c.Nickname, c.ShortName)
		}

		line := fmt.Sprintf("%s %s — %s", icon, name, c.Status)
		if c.Task != "" {
			line += fmt.Sprintf(" [%s]", c.Task)
		}
		b.WriteString(line + "\n")
	}

	return b.String()
}

// FormatSendConfirmation formats a confirmation for a sent message.
func FormatSendConfirmation(shortName string) string {
	return fmt.Sprintf("[maestro] Message queued for %s", shortName)
}

// FormatBroadcastConfirmation formats a confirmation for a broadcast message.
func FormatBroadcastConfirmation(sent []string) string {
	if len(sent) == 0 {
		return "[maestro] Broadcast: no containers received the message"
	}
	return fmt.Sprintf("[maestro] Broadcast sent to %d container(s): %s", len(sent), strings.Join(sent, ", "))
}

// FormatNewConfirmation formats a confirmation for a newly created container.
func FormatNewConfirmation(containerName string) string {
	return fmt.Sprintf("[maestro] Container created: %s", containerName)
}

// FormatCommandError formats an error message for Signal.
func FormatCommandError(err error) string {
	return fmt.Sprintf("[maestro] Error: %v", err)
}
