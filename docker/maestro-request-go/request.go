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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const requestDir = "/home/node/.maestro/requests"

// Message represents a message from a Claude session.
type Message struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

// RequestFile mirrors the daemon's IPCRequestFile structure.
type RequestFile struct {
	ID              string    `json:"id"`
	Action          string    `json:"action"`
	Task            string    `json:"task,omitempty"`
	Title           string    `json:"title,omitempty"`
	Message         string    `json:"message,omitempty"`
	Parent          string    `json:"parent"`
	Branch          string    `json:"branch,omitempty"`
	Status          string    `json:"status"`
	RequestedAt     string    `json:"requested_at"`
	ChildContainer  *string   `json:"child_container"`
	ChildExitedAt   *string   `json:"child_exited_at,omitempty"`
	FulfilledAt     *string   `json:"fulfilled_at"`
	Error           *string   `json:"error"`
	TargetRequestID string    `json:"target_request_id,omitempty"`
	Messages        []Message `json:"messages,omitempty"`
	Count           int       `json:"count,omitempty"`
	Timeout         int       `json:"timeout,omitempty"`
}

// requestFilePath returns the path to a request file by ID.
func requestFilePath(id string) string {
	return filepath.Join(requestDir, id+".json")
}

// readRequestFile reads and parses a request file by ID.
func readRequestFile(id string) (*RequestFile, error) {
	data, err := os.ReadFile(requestFilePath(id))
	if err != nil {
		return nil, fmt.Errorf("reading request file: %w", err)
	}
	var r RequestFile
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parsing request file: %w", err)
	}
	return &r, nil
}

// writeRequestFile writes a RequestFile to disk.
func writeRequestFile(r *RequestFile) error {
	if err := os.MkdirAll(requestDir, 0755); err != nil {
		return fmt.Errorf("creating request directory: %w", err)
	}

	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling request file: %w", err)
	}

	path := requestFilePath(r.ID)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing request file: %w", err)
	}
	return nil
}

// nowUTC returns the current time in UTC formatted as ISO 8601.
func nowUTC() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// generateUUID reads a UUID from /proc/sys/kernel/random/uuid.
func generateUUID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/uuid")
	if err != nil {
		return "", fmt.Errorf("generating UUID: %w", err)
	}
	return string(data[:len(data)-1]), nil // trim trailing newline
}

// hostname returns the container hostname.
func hostname() string {
	h, _ := os.Hostname()
	return h
}

// statusOrder returns the numeric order of a request status.
// Higher values indicate further progress in the lifecycle.
// failed returns -1 (special case).
func statusOrder(status string) int {
	switch status {
	case "pending":
		return 0
	case "fulfilled":
		return 1
	case "child_exited":
		return 2
	case "failed":
		return -1
	default:
		return -2
	}
}
