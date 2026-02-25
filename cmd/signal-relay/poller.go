// Copyright 2026 Christopher O'Connell
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
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"time"
)

const pollInterval = 3 * time.Second

// StartPolling launches a background goroutine that polls signal-cli's
// /v1/receive/{number} endpoint for incoming messages. This is used when
// signal-cli runs in normal (json) mode instead of json-rpc+webhook mode.
func (s *Server) StartPolling(ctx context.Context) {
	log.Printf("starting signal-cli poller (interval=%s)", pollInterval)

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

func (s *Server) pollOnce() {
	encodedNumber := url.PathEscape(s.botNumber)
	reqURL := s.signalAPI + "/v1/receive/" + encodedNumber

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		log.Printf("poller: failed to create request: %v", err)
		return
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Printf("poller: request failed: %v", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return
	}

	var messages []ReceivedMessage
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		log.Printf("poller: decode error: %v", err)
		return
	}

	for _, msg := range messages {
		if msg.Envelope.DataMessage == nil {
			continue
		}
		log.Printf("poller: message from %s (text=%q)", msg.Envelope.Source, msg.Envelope.DataMessage.Message)
		s.router.Route(msg)
	}
}
