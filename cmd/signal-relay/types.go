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

// ReceivedMessage represents a message received from signal-cli-rest-api.
// Duplicated from pkg/notify/signal to keep the relay module independent.
type ReceivedMessage struct {
	Envelope struct {
		Source      string `json:"source"`
		Timestamp   int64  `json:"timestamp"`
		DataMessage *struct {
			Message   string `json:"message"`
			Timestamp int64  `json:"timestamp"`
			Quote     *struct {
				ID     int64  `json:"id"`
				Author string `json:"author"`
				Text   string `json:"text"`
			} `json:"quote"`
		} `json:"dataMessage"`
	} `json:"envelope"`
}

// sendRequest is the JSON payload for POST /v2/send.
type sendRequest struct {
	Number     string   `json:"number"`
	Recipients []string `json:"recipients"`
	Message    string   `json:"message"`
}
