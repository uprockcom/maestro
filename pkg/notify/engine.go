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
	"errors"
	"strings"
	"sync"
	"time"
)

// Engine dispatches events to all registered providers and tracks pending
// questions and notification history.
type Engine struct {
	providers    []Provider
	callback     func(Response) error // routes answers back to containers
	pending      map[string]*pendingEntry
	history      []LogEntry
	logger       func(string, ...interface{})
	allowedTypes map[string]map[string]bool // provider name → allowed event types (nil = inherit global)
	mu           sync.Mutex
}

type pendingEntry struct {
	event  Event
	sentAt time.Time
	logIdx int // index into history slice for updating on response
}

// NewEngine creates a notification engine.
// providerNotifyOn maps provider names to their allowed event type lists.
// Providers not in the map inherit the global notify_on filter.
func NewEngine(providers []Provider, callback func(Response) error, logger func(string, ...interface{}), providerNotifyOn map[string][]string) *Engine {
	allowed := make(map[string]map[string]bool)
	for name, types := range providerNotifyOn {
		set := make(map[string]bool, len(types))
		for _, t := range types {
			set[t] = true
		}
		allowed[name] = set
	}
	return &Engine{
		providers:    providers,
		callback:     callback,
		pending:      make(map[string]*pendingEntry),
		logger:       logger,
		allowedTypes: allowed,
	}
}

// shouldSendTo returns whether the given event type should be dispatched to the
// named provider. If the provider has no per-provider override, it returns true
// (inheriting the global filter applied upstream).
// For prefixed names like "signal:4567", falls back to the base name "signal".
func (e *Engine) shouldSendTo(providerName, eventType string) bool {
	set, ok := e.allowedTypes[providerName]
	if !ok {
		// Try prefix match (e.g., "signal:4567" → "signal")
		if idx := strings.Index(providerName, ":"); idx > 0 {
			if set, ok = e.allowedTypes[providerName[:idx]]; ok {
				return set[eventType]
			}
		}
		return true // no override → inherit global
	}
	return set[eventType]
}

// matchesScope returns true if a provider should handle the given event.
// Non-scoped providers always match. Scoped providers are checked via MatchesEvent.
func matchesScope(p Provider, event Event) bool {
	if sp, ok := p.(ScopedProvider); ok {
		return sp.MatchesEvent(event)
	}
	return true
}

// Notify sends a one-way notification to ALL available providers concurrently.
func (e *Engine) Notify(event Event) {
	e.mu.Lock()
	entry := LogEntry{
		Direction: "outbound",
		Event:     &event,
		SentAt:    time.Now(),
	}
	var providerNames []string
	e.mu.Unlock()

	ctx := context.Background()
	var wg sync.WaitGroup
	var namesMu sync.Mutex

	for _, p := range e.providers {
		if !p.Available() {
			continue
		}
		if !e.shouldSendTo(p.Name(), string(event.Type)) {
			continue
		}
		if !matchesScope(p, event) {
			continue
		}
		wg.Add(1)
		go func(prov Provider) {
			defer wg.Done()
			if err := prov.Send(ctx, event); err != nil {
				e.logger("notify: provider %s Send error: %v", prov.Name(), err)
				return
			}
			namesMu.Lock()
			providerNames = append(providerNames, prov.Name())
			namesMu.Unlock()
		}(p)
	}
	wg.Wait()

	e.mu.Lock()
	entry.Providers = providerNames
	e.history = append(e.history, entry)
	e.mu.Unlock()
}

// AskQuestion sends an interactive question using a cascading chain of providers.
// Each provider returns one of four outcomes:
//   - Accept + stop (exclusive=true): add to race, stop chain
//   - Accept + continue (exclusive=false): add to race, continue
//   - ErrSkipped: skip entirely, continue
//   - ErrNotInteractive: fall back to plain Send, continue
//
// The engine races on all collected response channels — first response wins.
func (e *Engine) AskQuestion(event Event) {
	ctx := context.Background()
	var respChannels []<-chan Response
	var channelNames []string
	var providerNames []string

	for _, p := range e.providers {
		if !p.Available() {
			continue
		}
		if !e.shouldSendTo(p.Name(), string(event.Type)) {
			continue
		}
		if !matchesScope(p, event) {
			continue
		}

		ch, exclusive, err := p.SendInteractive(ctx, event)
		if err == nil && ch != nil {
			// Provider accepted the question
			respChannels = append(respChannels, ch)
			channelNames = append(channelNames, p.Name())
			providerNames = append(providerNames, p.Name())
			if exclusive {
				break // stop chain — this provider has it covered
			}
			continue
		}
		if errors.Is(err, ErrSkipped) {
			continue // provider declined — skip entirely, no plain notification
		}
		// ErrNotInteractive or other error — fall back to plain Send
		if sendErr := p.Send(ctx, event); sendErr != nil {
			e.logger("notify: provider %s Send error: %v", p.Name(), sendErr)
		} else {
			providerNames = append(providerNames, p.Name())
		}
	}

	e.mu.Lock()
	entry := LogEntry{
		Direction: "outbound",
		Event:     &event,
		SentAt:    time.Now(),
		Providers: providerNames,
	}
	e.history = append(e.history, entry)
	logIdx := len(e.history) - 1

	e.pending[event.ID] = &pendingEntry{
		event:  event,
		sentAt: time.Now(),
		logIdx: logIdx,
	}
	e.mu.Unlock()

	// Race on all collected response channels — first response wins
	if len(respChannels) > 0 {
		merged := make(chan Response, 1)
		for i, ch := range respChannels {
			go func(c <-chan Response, name string) {
				resp, ok := <-c
				if ok {
					resp.Provider = name
					select {
					case merged <- resp:
					default: // another response already won
					}
				}
			}(ch, channelNames[i])
		}
		go func() {
			resp := <-merged
			e.handleResponse(event.ID, resp)
		}()
	}
}

// CancelQuestion removes a pending question without answering it.
// It does NOT notify providers — use CancelQuestionWithNotify for that.
func (e *Engine) CancelQuestion(eventID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.pending, eventID)
}

// CancelQuestionWithNotify removes a pending question and notifies all
// QuestionListener providers so they can clean up their UI state.
func (e *Engine) CancelQuestionWithNotify(eventID string) {
	e.mu.Lock()
	_, ok := e.pending[eventID]
	if ok {
		delete(e.pending, eventID)
	}
	e.mu.Unlock()
	if !ok {
		return
	}
	for _, p := range e.providers {
		if ql, ok := p.(QuestionListener); ok {
			ql.OnQuestionCancelled(eventID)
		}
	}
}

// GetPendingQuestions returns a snapshot of all pending questions.
func (e *Engine) GetPendingQuestions() []PendingQuestion {
	e.mu.Lock()
	defer e.mu.Unlock()

	result := make([]PendingQuestion, 0, len(e.pending))
	for _, p := range e.pending {
		result = append(result, PendingQuestion{
			Event:  p.event,
			SentAt: p.sentAt,
		})
	}
	return result
}

// SubmitAnswer handles an externally submitted answer (e.g. from TUI or HTTP).
func (e *Engine) SubmitAnswer(eventID string, resp Response) {
	e.handleResponse(eventID, resp)
}

// GetHistory returns the most recent N log entries.
func (e *Engine) GetHistory(limit int) []LogEntry {
	e.mu.Lock()
	defer e.mu.Unlock()

	if limit <= 0 || limit > len(e.history) {
		limit = len(e.history)
	}
	start := len(e.history) - limit
	out := make([]LogEntry, limit)
	copy(out, e.history[start:])
	return out
}

// Close shuts down all providers.
func (e *Engine) Close() {
	for _, p := range e.providers {
		if err := p.Close(); err != nil {
			e.logger("notify: provider %s Close error: %v", p.Name(), err)
		}
	}
}

// handleResponse processes a response, updates history, calls the callback,
// and notifies all QuestionListener providers so they can clean up.
func (e *Engine) handleResponse(eventID string, resp Response) {
	e.mu.Lock()
	pe, ok := e.pending[eventID]
	if ok {
		delete(e.pending, eventID)
		// Update the log entry with the response
		now := time.Now()
		if pe.logIdx < len(e.history) {
			e.history[pe.logIdx].Response = &resp
			e.history[pe.logIdx].RespondedAt = &now
		}
	}
	e.mu.Unlock()

	if !ok {
		return // already answered or cancelled, skip
	}

	if e.callback != nil {
		if err := e.callback(resp); err != nil {
			e.logger("notify: callback error for event %s: %v", eventID, err)
		}
	}

	// Notify all providers that implement QuestionListener
	for _, p := range e.providers {
		if ql, ok := p.(QuestionListener); ok {
			ql.OnQuestionResolved(eventID, resp)
		}
	}
}
