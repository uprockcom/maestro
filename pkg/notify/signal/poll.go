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
	"time"
)

const pollInterval = 3 * time.Second

// pollLoop runs a ticker that polls for incoming Signal messages.
// In local mode (destructive reads), only polls when there are pending questions.
// In remote/relay mode (non-destructive cursor-based reads), always polls so
// messages are consumed and cursor advances.
func (s *SignalProvider) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !s.remote && s.commands == nil {
				// Local mode without commands: only poll when there are pending questions
				// (destructive reads would discard unmatched messages)
				s.mu.Lock()
				hasPending := len(s.pending) > 0
				s.mu.Unlock()
				if !hasPending {
					continue
				}
			}

			s.pollOnce()
		}
	}
}

// pollOnce fetches messages and matches replies to pending questions.
// Matching priority:
//  1. Reply-to (quote): match the quoted message timestamp to a pending question
//  2. Commands: if the message parses as a command (@name, list, new, etc.), execute it
//  3. Fallback: match to the most recent pending question
func (s *SignalProvider) pollOnce() {
	result, err := s.api.Receive(s.cursor)
	if err != nil {
		s.logger("signal: receive error: %v", err)
		return
	}

	// Update cursor for next poll (relay mode)
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
				s.logger("signal: matched reply-to quote (ts=%d) to question %s", quoteTS, matchedID)
			}
		}

		// Priority 2: if the message looks like a command, execute it
		// (commands like @name, @all, list, new take priority over
		// the fallback to an unrelated pending question)
		if matchedID == "" && s.commands != nil {
			if cmd := ParseSignalCommand(text); cmd != nil {
				s.mu.Unlock()
				s.logger("signal: parsed command: %s", text)
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
				s.logger("signal: no quote, falling back to most recent question %s", matchedID)
			}
		}

		if matchedID == "" {
			s.mu.Unlock()
			continue
		}

		pq := s.pending[matchedID]
		resp := ParseResponse(text, pq.Event.Question)
		resp.EventID = matchedID
		resp.ContainerName = pq.Event.ContainerName
		resp.Provider = "signal"

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
