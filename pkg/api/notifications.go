package api

import "time"

// These types mirror pkg/notify/types.go for the wire format.
// The daemon converts between notify.* types and these api.* types.

// QuestionOption represents a selectable option for a question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description"`
}

// QuestionItem represents a single question from AskUserQuestion.
type QuestionItem struct {
	Question    string           `json:"question"`
	Header      string           `json:"header"`
	Options     []QuestionOption `json:"options"`
	MultiSelect bool             `json:"multiSelect"`
}

// QuestionData is the full question payload from a container.
type QuestionData struct {
	Questions []QuestionItem `json:"questions"`
}

// EventType represents the type of notification event.
type EventType string

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

// PendingQuestion is a question waiting for user input.
type PendingQuestion struct {
	Event  Event     `json:"event"`
	SentAt time.Time `json:"sent_at"`
}

// ListPendingNotificationsResponse is GET /api/v1/notifications/pending.
type ListPendingNotificationsResponse struct {
	Questions []PendingQuestion `json:"questions"`
}

// AnswerNotificationRequest is POST /api/v1/notifications/answer.
type AnswerNotificationRequest struct {
	EventID    string   `json:"event_id"`
	Selections []string `json:"selections,omitempty"`
	Text       string   `json:"text,omitempty"`
}

// AnswerNotificationResponse is POST /api/v1/notifications/answer.
type AnswerNotificationResponse struct {
	Success bool `json:"success"`
}

// DismissNotificationRequest is POST /api/v1/notifications/dismiss.
type DismissNotificationRequest struct {
	EventID string `json:"event_id"`
}

// DismissNotificationResponse is POST /api/v1/notifications/dismiss.
type DismissNotificationResponse struct {
	Success bool `json:"success"`
}
