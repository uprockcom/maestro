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

package tui

import (
	"context"
	"fmt"

	"github.com/uprockcom/maestro/pkg/api"
	"github.com/uprockcom/maestro/pkg/notify"
)

// DaemonClient wraps api.Client for TUI-specific daemon operations.
type DaemonClient struct {
	client    *api.Client
	configDir string
}

// NewDaemonClient reads daemon-ipc.json and returns a client.
// Returns nil, nil if daemon is not running.
func NewDaemonClient(configDir string) (*DaemonClient, error) {
	client, err := api.NewClientFromConfig(configDir)
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, nil
	}
	return &DaemonClient{client: client, configDir: configDir}, nil
}

// Reconnect re-reads daemon-ipc.json (handles daemon restart).
func (c *DaemonClient) Reconnect() error {
	client, err := api.NewClientFromConfig(c.configDir)
	if err != nil {
		return err
	}
	if client == nil {
		return fmt.Errorf("daemon not running")
	}
	c.client = client
	return nil
}

// GetPendingNotifications fetches pending questions from the daemon.
func (c *DaemonClient) GetPendingNotifications() ([]notify.PendingQuestion, error) {
	ctx := context.Background()
	resp, err := api.Call(ctx, c.client, api.GetPendingNotifications, nil)
	if err != nil {
		return nil, err
	}
	return toNotifyQuestions(resp.Questions), nil
}

// AnswerNotification submits an answer to a pending question.
func (c *DaemonClient) AnswerNotification(eventID string, selections []string, text string) error {
	ctx := context.Background()
	_, err := api.Call(ctx, c.client, api.AnswerNotification, &api.AnswerNotificationRequest{
		EventID:    eventID,
		Selections: selections,
		Text:       text,
	})
	return err
}

// DismissNotification removes a pending notification.
func (c *DaemonClient) DismissNotification(eventID string) error {
	ctx := context.Background()
	_, err := api.Call(ctx, c.client, api.DismissNotification, &api.DismissNotificationRequest{
		EventID: eventID,
	})
	return err
}

// toNotifyQuestions converts api.PendingQuestion to notify.PendingQuestion.
func toNotifyQuestions(apiQs []api.PendingQuestion) []notify.PendingQuestion {
	result := make([]notify.PendingQuestion, len(apiQs))
	for i, aq := range apiQs {
		var question *notify.QuestionData
		if aq.Event.Question != nil {
			items := make([]notify.QuestionItem, len(aq.Event.Question.Questions))
			for j, q := range aq.Event.Question.Questions {
				opts := make([]notify.QuestionOption, len(q.Options))
				for k, o := range q.Options {
					opts[k] = notify.QuestionOption{
						Label:       o.Label,
						Description: o.Description,
					}
				}
				items[j] = notify.QuestionItem{
					Question:    q.Question,
					Header:      q.Header,
					Options:     opts,
					MultiSelect: q.MultiSelect,
				}
			}
			question = &notify.QuestionData{Questions: items}
		}

		result[i] = notify.PendingQuestion{
			Event: notify.Event{
				ID:            aq.Event.ID,
				ContainerName: aq.Event.ContainerName,
				ShortName:     aq.Event.ShortName,
				Branch:        aq.Event.Branch,
				Title:         aq.Event.Title,
				Message:       aq.Event.Message,
				Type:          notify.EventType(aq.Event.Type),
				Timestamp:     aq.Event.Timestamp,
				Question:      question,
			},
			SentAt: aq.SentAt,
		}
	}
	return result
}
