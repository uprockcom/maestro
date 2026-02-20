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

import (
	"context"
	"sync"
	"time"
)

// LocalProvider stores events in memory for TUI consumption and supports
// interactive question/response via response channels.
type LocalProvider struct {
	mu      sync.Mutex
	pending map[string]*localPendingEvent
}

type localPendingEvent struct {
	Event  Event
	RespCh chan Response // nil for non-interactive events
	SentAt time.Time
}

// NewLocalProvider creates a new local provider.
func NewLocalProvider() *LocalProvider {
	return &LocalProvider{
		pending: make(map[string]*localPendingEvent),
	}
}

func (l *LocalProvider) Name() string { return "local" }

func (l *LocalProvider) Send(_ context.Context, event Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending[event.ID] = &localPendingEvent{
		Event:  event,
		SentAt: time.Now(),
	}
	return nil
}

func (l *LocalProvider) SendInteractive(_ context.Context, event Event) (<-chan Response, bool, error) {
	ch := make(chan Response, 1)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pending[event.ID] = &localPendingEvent{
		Event:  event,
		RespCh: ch,
		SentAt: time.Now(),
	}
	return ch, false, nil
}

func (l *LocalProvider) Available() bool { return true }

func (l *LocalProvider) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Close any open response channels
	for _, pe := range l.pending {
		if pe.RespCh != nil {
			close(pe.RespCh)
		}
	}
	l.pending = make(map[string]*localPendingEvent)
	return nil
}

// GetPending returns a snapshot of pending events for TUI display.
func (l *LocalProvider) GetPending() []PendingQuestion {
	l.mu.Lock()
	defer l.mu.Unlock()

	result := make([]PendingQuestion, 0, len(l.pending))
	for _, pe := range l.pending {
		result = append(result, PendingQuestion{
			Event:  pe.Event,
			SentAt: pe.SentAt,
		})
	}
	return result
}

// Answer submits a response for a pending event and removes it.
func (l *LocalProvider) Answer(eventID string, resp Response) {
	l.mu.Lock()
	pe, ok := l.pending[eventID]
	if ok {
		delete(l.pending, eventID)
	}
	l.mu.Unlock()

	if ok && pe.RespCh != nil {
		pe.RespCh <- resp
		close(pe.RespCh)
	}
}

// OnQuestionResolved implements QuestionListener. It clears the local pending
// entry when a question is answered via another provider.
func (l *LocalProvider) OnQuestionResolved(eventID string, _ Response) {
	l.ClearEvent(eventID)
}

// OnQuestionCancelled implements QuestionListener. It clears the local pending
// entry when a question becomes irrelevant (answered externally or vanished).
func (l *LocalProvider) OnQuestionCancelled(eventID string) {
	l.ClearEvent(eventID)
}

// ClearEvent removes a pending event without answering (cancellation).
func (l *LocalProvider) ClearEvent(eventID string) {
	l.mu.Lock()
	pe, ok := l.pending[eventID]
	if ok {
		delete(l.pending, eventID)
	}
	l.mu.Unlock()

	if ok && pe.RespCh != nil {
		close(pe.RespCh)
	}
}
