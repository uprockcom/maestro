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
	"sync"
	"sync/atomic"
	"time"

	"github.com/uprockcom/maestro/pkg/notify"
)

// pendingSignalQ tracks a question sent to Signal that is awaiting a reply.
type pendingSignalQ struct {
	Event    notify.Event
	RespCh   chan notify.Response // buffered(1), engine reads from this
	SentAt   time.Time
	MsgTimestamp int64 // Signal message timestamp for reply-to matching
}

// SignalProvider sends notifications via Signal and supports interactive
// question/response by polling for incoming messages.
type SignalProvider struct {
	api        *APIClient
	recipient  string
	port       int
	remote     bool   // true when using a remote relay (URL is set)
	cursor     uint64 // cursor for relay's non-destructive polling
	pending    map[string]*pendingSignalQ
	commands   notify.CommandHandler // provider-agnostic command handler
	logger     func(string, ...interface{})
	pollCancel context.CancelFunc
	connected  atomic.Bool
	mu         sync.Mutex
}

// New creates a new SignalProvider.
func New(cfg Config, commands notify.CommandHandler, logger func(string, ...interface{})) *SignalProvider {
	sp := &SignalProvider{
		recipient: cfg.Recipient,
		pending:   make(map[string]*pendingSignalQ),
		commands:  commands,
		logger:    logger,
	}

	if cfg.URL != "" {
		// Remote relay mode — use relay URL with API key auth
		sp.api = NewAPIClientWithKey(cfg.URL, cfg.Number, cfg.APIKey)
		sp.remote = true
	} else {
		// Local mode — talk to local signal-cli container
		port := cfg.Port
		if port == 0 {
			port = 8080
		}
		sp.api = NewAPIClient(fmt.Sprintf("http://127.0.0.1:%d", port), cfg.Number)
		sp.port = port
	}

	return sp
}

func (s *SignalProvider) Name() string { return "signal" }

// Send delivers a plain (non-interactive) notification via Signal.
func (s *SignalProvider) Send(_ context.Context, event notify.Event) error {
	text := FormatEvent(event)
	_, err := s.api.SendMessage(s.recipient, text)
	return err
}

// SendInteractive sends a numbered question via Signal and returns a response
// channel. Never exclusive — allows other providers to also participate.
func (s *SignalProvider) SendInteractive(_ context.Context, event notify.Event) (<-chan notify.Response, bool, error) {
	text := FormatEvent(event)
	msgTS, err := s.api.SendMessage(s.recipient, text)
	if err != nil {
		return nil, false, fmt.Errorf("signal send failed: %w", err)
	}

	ch := make(chan notify.Response, 1)
	s.mu.Lock()
	s.pending[event.ID] = &pendingSignalQ{
		Event:        event,
		RespCh:       ch,
		SentAt:       time.Now(),
		MsgTimestamp: msgTS,
	}
	s.mu.Unlock()

	return ch, false, nil
}

// Available reports whether the Signal container is connected and healthy.
func (s *SignalProvider) Available() bool {
	return s.connected.Load()
}

// Close cancels the poll loop. It does NOT stop the Docker container
// (preserves Signal registration across daemon restarts).
func (s *SignalProvider) Close() error {
	if s.pollCancel != nil {
		s.pollCancel()
	}
	s.connected.Store(false)

	// Close any open response channels
	s.mu.Lock()
	for _, pq := range s.pending {
		close(pq.RespCh)
	}
	s.pending = make(map[string]*pendingSignalQ)
	s.mu.Unlock()

	return nil
}

// Run starts the Signal backend and enters the polling loop. In local mode,
// this starts the Docker container. In remote mode, it health-checks the relay.
// Blocks until ctx is cancelled.
func (s *SignalProvider) Run(ctx context.Context) error {
	if s.remote {
		// Remote mode — health-check the relay instead of starting a container
		if _, err := s.api.About(); err != nil {
			return fmt.Errorf("signal relay health check failed: %w", err)
		}
		s.logger("signal: connected to remote relay, starting poll loop")
	} else {
		// Local mode — start signal-cli container
		if err := EnsureRunning(s.port, s.logger); err != nil {
			return fmt.Errorf("signal container failed to start: %w", err)
		}
		s.logger("signal: provider connected, starting poll loop")
	}

	s.connected.Store(true)

	pollCtx, cancel := context.WithCancel(ctx)
	s.pollCancel = cancel
	s.pollLoop(pollCtx)

	s.connected.Store(false)
	return nil
}

// OnQuestionResolved implements notify.QuestionListener. It removes the
// pending question and sends a follow-up message to Signal.
func (s *SignalProvider) OnQuestionResolved(eventID string, resp notify.Response) {
	s.mu.Lock()
	pq, ok := s.pending[eventID]
	if ok {
		delete(s.pending, eventID)
		close(pq.RespCh)
	}
	s.mu.Unlock()

	// Send follow-up only if the question was answered by a different provider
	if ok && resp.Provider != "signal" {
		text := FormatResolved(resp.Provider)
		if _, err := s.api.SendMessage(s.recipient, text); err != nil {
			s.logger("signal: failed to send resolved follow-up: %v", err)
		}
	}
}

// OnQuestionCancelled implements notify.QuestionListener. It cleans up the
// pending question when it becomes irrelevant (answered externally or vanished).
func (s *SignalProvider) OnQuestionCancelled(eventID string) {
	s.mu.Lock()
	pq, ok := s.pending[eventID]
	if ok {
		delete(s.pending, eventID)
		close(pq.RespCh)
	}
	s.mu.Unlock()
}
