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
)

// ErrNotInteractive is returned by SendInteractive when a provider does not
// support interactive question/response flows.
var ErrNotInteractive = errors.New("provider does not support interactive responses")

// ErrSkipped is returned by SendInteractive when a provider declines to handle
// a particular question (e.g. Slack user is away). The engine skips the
// provider entirely — no plain Send fallback.
var ErrSkipped = errors.New("provider declined this question")

// Provider is the interface every notification channel must implement.
type Provider interface {
	// Name returns a short identifier for the provider (e.g. "desktop", "local").
	Name() string

	// Send delivers a one-way notification. It should not block for user input.
	Send(ctx context.Context, event Event) error

	// SendInteractive sends a notification that expects a response. It returns
	// a channel that will receive the user's response and whether the provider
	// is exclusive (if true, the engine stops the cascading chain).
	// Providers that cannot collect responses should return nil, false, ErrNotInteractive.
	// Providers that decline a specific question return nil, false, ErrSkipped.
	SendInteractive(ctx context.Context, event Event) (ch <-chan Response, exclusive bool, err error)

	// Available reports whether the provider is currently usable.
	Available() bool

	// Close releases any resources held by the provider.
	Close() error
}

// QuestionListener is an optional interface that providers can implement to be
// notified when a question is resolved (answered via any provider). This allows
// providers to clean up pending state and send follow-up messages.
type QuestionListener interface {
	OnQuestionResolved(eventID string, resp Response)
	// OnQuestionCancelled is called when a question becomes irrelevant
	// (dismissed by user or answered externally).
	OnQuestionCancelled(eventID string)
}

// ScopedProvider is an optional interface for providers that should only handle
// events for specific contacts. The engine skips scoped providers whose
// MatchesEvent returns false.
type ScopedProvider interface {
	Provider
	MatchesEvent(event Event) bool
	IsDefault() bool
}
