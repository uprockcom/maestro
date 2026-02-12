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

package daemon

import "time"

// IPCAction represents the type of IPC request
type IPCAction string

const (
	IPCActionNew          IPCAction = "new"
	IPCActionNotify       IPCAction = "notify"
	IPCActionExit         IPCAction = "exit"
	IPCActionReadMessages IPCAction = "read_messages"
	IPCActionSendMessage  IPCAction = "send_message"
	IPCActionWaitIdle     IPCAction = "wait_idle"
)

// IPCRequestStatus represents the status of a persisted IPC request
type IPCRequestStatus string

const (
	IPCRequestStatusPending     IPCRequestStatus = "pending"
	IPCRequestStatusFulfilled   IPCRequestStatus = "fulfilled"
	IPCRequestStatusFailed      IPCRequestStatus = "failed"
	IPCRequestStatusChildExited IPCRequestStatus = "child_exited"
)

// IPCMessage represents a message from a Claude session
type IPCMessage struct {
	Role      string `json:"role"`      // "user" or "assistant"
	Content   string `json:"content"`   // Text content (truncated at 10KB)
	Timestamp string `json:"timestamp"` // ISO 8601
}

// IPCRequest is the wire format for IPC requests from containers
type IPCRequest struct {
	ID              string    `json:"id"`
	Action          IPCAction `json:"action"`
	Task            string    `json:"task,omitempty"`              // For "new" action
	Title           string    `json:"title,omitempty"`             // For "notify" action
	Message         string    `json:"message,omitempty"`           // For "notify" and "send_message" actions
	Parent          string    `json:"parent"`                      // Container hostname (verified)
	Branch          string    `json:"branch,omitempty"`            // Optional branch for "new" action
	TargetRequestID string    `json:"target_request_id,omitempty"` // For child-targeting actions
	Count           int       `json:"count,omitempty"`             // read_messages: how many (default 10, max 50)
	Timeout         int       `json:"timeout,omitempty"`           // wait_idle: seconds (default 300)
}

// IPCResponse is the wire format for IPC responses to containers
type IPCResponse struct {
	Status    string `json:"status"`              // "ok", "accepted", "error"
	Container string `json:"container,omitempty"` // For "new" action (immediate only)
	Error     string `json:"error,omitempty"`
}

// IPCRequestFile is the persistence format stored in the container filesystem
type IPCRequestFile struct {
	ID              string           `json:"id"`
	Action          IPCAction        `json:"action"`
	Task            string           `json:"task,omitempty"`
	Title           string           `json:"title,omitempty"`
	Message         string           `json:"message,omitempty"`
	Parent          string           `json:"parent"`
	Branch          string           `json:"branch,omitempty"`
	Status          IPCRequestStatus `json:"status"`
	RequestedAt     time.Time        `json:"requested_at"`
	ChildContainer  *string          `json:"child_container,omitempty"`
	FulfilledAt     *time.Time       `json:"fulfilled_at,omitempty"`
	ChildExitedAt   *time.Time       `json:"child_exited_at,omitempty"`
	Error           *string          `json:"error,omitempty"`
	TargetRequestID string           `json:"target_request_id,omitempty"`
	Messages        []IPCMessage     `json:"messages,omitempty"`
	Count           int              `json:"count,omitempty"`
	Timeout         int              `json:"timeout,omitempty"`
}

// IPCStatusResponse is returned by the /status endpoint
type IPCStatusResponse struct {
	Running    bool     `json:"running"`
	PID        int      `json:"pid"`
	Containers []string `json:"containers"`
	Uptime     string   `json:"uptime"`
}
