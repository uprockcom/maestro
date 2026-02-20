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

package main

import (
	"context"
	"sync"
	"time"
)

// QueuedMessage wraps a received message with a monotonic ID and expiry.
type QueuedMessage struct {
	ID        uint64          `json:"id"`
	Message   ReceivedMessage `json:"message"`
	ExpiresAt time.Time       `json:"-"`
}

// UserQueue is an append-only queue of messages for a single sender phone number.
type UserQueue struct {
	mu       sync.RWMutex
	messages []QueuedMessage
	nextID   uint64
}

// Enqueue adds a message to the queue and returns its assigned ID.
func (q *UserQueue) Enqueue(msg ReceivedMessage, ttl time.Duration) uint64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nextID++
	q.messages = append(q.messages, QueuedMessage{
		ID:        q.nextID,
		Message:   msg,
		ExpiresAt: time.Now().Add(ttl),
	})
	return q.nextID
}

// Receive returns messages with ID > afterID (non-destructive read).
// Also returns the highest ID in the result set for cursor tracking.
func (q *UserQueue) Receive(afterID uint64) ([]ReceivedMessage, uint64) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	var result []ReceivedMessage
	var maxID uint64
	for _, qm := range q.messages {
		if qm.ID > afterID {
			result = append(result, qm.Message)
			if qm.ID > maxID {
				maxID = qm.ID
			}
		}
	}
	return result, maxID
}

// Prune removes expired messages.
func (q *UserQueue) Prune(now time.Time) {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.messages[:0]
	for _, qm := range q.messages {
		if qm.ExpiresAt.After(now) {
			kept = append(kept, qm)
		}
	}
	q.messages = kept
}

// MessageRouter dispatches incoming messages into per-sender queues.
type MessageRouter struct {
	mu     sync.RWMutex
	queues map[string]*UserQueue // keyed by sender phone number
	ttl    time.Duration
}

// NewMessageRouter creates a new router with the given message TTL.
func NewMessageRouter(ttl time.Duration) *MessageRouter {
	return &MessageRouter{
		queues: make(map[string]*UserQueue),
		ttl:    ttl,
	}
}

// Route dispatches a message into the queue for its sender.
func (r *MessageRouter) Route(msg ReceivedMessage) {
	sender := msg.Envelope.Source
	if sender == "" {
		return
	}

	r.mu.Lock()
	q, ok := r.queues[sender]
	if !ok {
		q = &UserQueue{}
		r.queues[sender] = q
	}
	r.mu.Unlock()

	q.Enqueue(msg, r.ttl)
}

// Receive returns messages from a specific sender with ID > afterID.
func (r *MessageRouter) Receive(senderPhone string, afterID uint64) ([]ReceivedMessage, uint64) {
	r.mu.RLock()
	q, ok := r.queues[senderPhone]
	r.mu.RUnlock()

	if !ok {
		return nil, 0
	}
	return q.Receive(afterID)
}

// StartPruning runs a background goroutine that prunes expired messages every 30s.
func (r *MessageRouter) StartPruning(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			r.mu.RLock()
			queues := make([]*UserQueue, 0, len(r.queues))
			for _, q := range r.queues {
				queues = append(queues, q)
			}
			r.mu.RUnlock()
			for _, q := range queues {
				q.Prune(now)
			}
		}
	}
}
