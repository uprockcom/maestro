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
	"context"
	"fmt"
	"time"

	"github.com/uprockcom/maestro/pkg/notify"
)

const pollInterval = 3 * time.Second

// pollLoop runs a ticker that polls for incoming Signal messages.
// Always polls — relay uses non-destructive cursor-based reads.
func (s *SignalProvider) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pollOnce()
		}
	}
}

// pollOnce fetches messages and matches replies to pending questions.
// Matching priority:
//  1. Reply-to (quote): match the quoted message timestamp to a pending question
//  2. Commands: if the message parses as a command (@name, @all, list, new, etc.), execute it
//  3. Fallback: match to the most recent pending question
//  4. Auto-route: if exactly one visible container exists, route the message there
func (s *SignalProvider) pollOnce() {
	result, err := s.api.Receive(s.cursor)
	if err != nil {
		s.logger("signal [%s]: receive error: %v", s.name, err)
		return
	}

	// Update cursor for next poll
	if result.MaxID > s.cursor {
		s.cursor = result.MaxID
	}

	for _, msg := range result.Messages {
		// Only process messages from the configured recipient
		if msg.Envelope.Source != s.recipient {
			continue
		}
		if msg.Envelope.DataMessage == nil || msg.Envelope.DataMessage.Message == "" {
			continue
		}

		text := msg.Envelope.DataMessage.Message

		s.mu.Lock()
		var matchedID string

		// Priority 1: match by reply-to (quote) timestamp
		if msg.Envelope.DataMessage.Quote != nil && msg.Envelope.DataMessage.Quote.ID != 0 {
			quoteTS := msg.Envelope.DataMessage.Quote.ID
			for id, pq := range s.pending {
				if pq.MsgTimestamp == quoteTS {
					matchedID = id
					break
				}
			}
			if matchedID != "" {
				s.logger("signal [%s]: matched reply-to quote (ts=%d) to question %s", s.name, quoteTS, matchedID)
			}
		}

		// Priority 2: if the message looks like a command, execute it
		// (commands like @name, @all, list, new take priority over
		// the fallback to an unrelated pending question)
		if matchedID == "" && s.commands != nil {
			if cmd := ParseSignalCommand(text); cmd != nil {
				s.mu.Unlock()
				s.logger("signal [%s]: parsed command: %s", s.name, text)
				go s.executeCommand(cmd)
				continue
			}
		}

		// Priority 3: fall back to most recent pending question
		if matchedID == "" {
			var latestTime time.Time
			for id, pq := range s.pending {
				if matchedID == "" || pq.SentAt.After(latestTime) {
					matchedID = id
					latestTime = pq.SentAt
				}
			}
			if matchedID != "" {
				s.logger("signal [%s]: no quote, falling back to most recent question %s", s.name, matchedID)
			}
		}

		// Priority 4: auto-route to sole visible container
		if matchedID == "" && s.commands != nil {
			s.mu.Unlock()
			s.autoRoute(text)
			continue
		}

		if matchedID == "" {
			s.mu.Unlock()
			continue
		}

		pq := s.pending[matchedID]
		resp := ParseResponse(text, pq.Event.Question)
		resp.EventID = matchedID
		resp.ContainerName = pq.Event.ContainerName
		resp.Provider = s.name

		// Remove from pending and send response
		delete(s.pending, matchedID)
		respCh := pq.RespCh
		s.mu.Unlock()

		// Push to the response channel (engine reads from this)
		select {
		case respCh <- resp:
		default:
		}
	}
}

// autoRoute sends an unmatched message to the sole visible container, if exactly one exists.
func (s *SignalProvider) autoRoute(text string) {
	ctx := context.Background()
	containers, err := s.commands.ListContainers(ctx, "")
	if err != nil {
		return
	}

	visible := s.filterByContacts(containers)
	if len(visible) == 1 {
		go func() {
			if err := s.commands.SendToContainer(ctx, visible[0].Name, text); err != nil {
				s.logger("signal [%s]: auto-route failed: %v", s.name, err)
				return
			}
			if _, err := s.api.SendMessage(s.recipient, fmt.Sprintf("Sent to %s", visible[0].ShortName)); err != nil {
				s.logger("signal [%s]: auto-route ack failed: %v", s.name, err)
			}
		}()
	}
}

// filterByContacts filters containers to those visible to this provider's recipient.
func (s *SignalProvider) filterByContacts(containers []notify.ContainerSummary) []notify.ContainerSummary {
	var result []notify.ContainerSummary
	for _, c := range containers {
		if c.Contacts == nil || len(c.Contacts) == 0 {
			// No contact override — visible only to default provider
			if s.isDefault {
				result = append(result, c)
			}
		} else if sc, ok := c.Contacts["signal"]; ok {
			// Signal contact override — visible only to matching provider
			if sc["recipient"] == s.recipient {
				result = append(result, c)
			}
		} else {
			// Non-signal contact override — visible to default provider
			if s.isDefault {
				result = append(result, c)
			}
		}
	}
	return result
}
