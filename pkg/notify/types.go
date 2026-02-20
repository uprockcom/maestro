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

package notify

import "time"

// EventType represents the type of notification event.
type EventType string

const (
	EventAttentionNeeded       EventType = "attention_needed"
	EventQuestion              EventType = "question"
	EventTasksCompleted        EventType = "tasks_completed"
	EventTokenExpiring         EventType = "token_expiring"
	EventContainerNotification EventType = "container_notification"
	EventDormant               EventType = "dormant"
	EventBlocker               EventType = "blocker"
)

// Event represents a notification event from a container.
type Event struct {
	ID            string        `json:"id"`
	ContainerName string        `json:"container_name"`
	ShortName     string        `json:"short_name"`
	Branch        string        `json:"branch,omitempty"`
	Title         string        `json:"title"`
	Message       string        `json:"message"`
	Type          EventType     `json:"type"`
	Timestamp     time.Time     `json:"timestamp"`
	Question      *QuestionData `json:"question,omitempty"`
}

// QuestionData mirrors the tool_input JSON from a container's current-question.json.
type QuestionData struct {
	Questions []QuestionItem `json:"questions"`
}

// QuestionItem represents a single question from AskUserQuestion.
type QuestionItem struct {
	Question    string           `json:"question"`
	Header      string           `json:"header"`
	Options     []QuestionOption `json:"options"`
	MultiSelect bool             `json:"multiSelect"`
}

// QuestionOption represents a selectable option for a question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Response represents a user's answer to a notification/question.
type Response struct {
	EventID       string   `json:"event_id"`
	ContainerName string   `json:"container_name"`
	Text          string   `json:"text,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	Selections    []string `json:"selections,omitempty"`
}

// PendingQuestion is exported for TUI consumption.
type PendingQuestion struct {
	Event  Event     `json:"event"`
	SentAt time.Time `json:"sent_at"`
}

// LogEntry records a notification in the unified history.
type LogEntry struct {
	Direction       string           `json:"direction"` // "outbound" or "inbound"
	Event           *Event           `json:"event,omitempty"`
	SentAt          time.Time        `json:"sent_at"`
	Providers       []string         `json:"providers,omitempty"`
	Response        *Response        `json:"response,omitempty"`
	RespondedAt     *time.Time       `json:"responded_at,omitempty"`
	IncomingMessage *IncomingMessage `json:"incoming_message,omitempty"`
}

// IncomingMessage captures unsolicited messages from external providers.
type IncomingMessage struct {
	Provider  string    `json:"provider"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}
