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

package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/uprockcom/maestro/pkg/notify"
)

// DaemonClient talks to the running daemon's IPC server.
type DaemonClient struct {
	port      int
	token     string
	client    *http.Client
	configDir string // stored so we can re-read daemon-ipc.json on reconnect
}

// daemonIPCInfo mirrors daemon.DaemonIPCInfo without importing the daemon package.
type daemonIPCInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
}

// NewDaemonClient reads daemon-ipc.json from the given config directory and
// returns a client that can communicate with the daemon. Returns nil, nil if
// the daemon is not running (file not found).
func NewDaemonClient(configDir string) (*DaemonClient, error) {
	ipcPath := filepath.Join(configDir, "daemon-ipc.json")
	data, err := os.ReadFile(ipcPath)
	if err != nil {
		return nil, nil // daemon not running
	}

	var info daemonIPCInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("invalid daemon-ipc.json: %w", err)
	}
	if info.Port == 0 {
		return nil, nil
	}

	return &DaemonClient{
		port:      info.Port,
		token:     info.Token,
		configDir: configDir,
		client: &http.Client{
			Timeout: 3 * time.Second,
		},
	}, nil
}

// Reconnect re-reads daemon-ipc.json and updates the port and token.
// This handles daemon restarts where the port/token change.
func (c *DaemonClient) Reconnect() error {
	ipcPath := filepath.Join(c.configDir, "daemon-ipc.json")
	data, err := os.ReadFile(ipcPath)
	if err != nil {
		return fmt.Errorf("daemon-ipc.json not found: %w", err)
	}

	var info daemonIPCInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return fmt.Errorf("invalid daemon-ipc.json: %w", err)
	}
	if info.Port == 0 {
		return fmt.Errorf("daemon-ipc.json has no port")
	}

	c.port = info.Port
	c.token = info.Token
	return nil
}

// GetPendingNotifications fetches pending questions from the daemon.
func (c *DaemonClient) GetPendingNotifications() ([]notify.PendingQuestion, error) {
	url := fmt.Sprintf("http://127.0.0.1:%d/notifications/pending", c.port)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Maestro-Token", c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daemon returned %d", resp.StatusCode)
	}

	var questions []notify.PendingQuestion
	if err := json.NewDecoder(resp.Body).Decode(&questions); err != nil {
		return nil, err
	}
	return questions, nil
}

// DismissNotification removes a pending notification without answering it.
func (c *DaemonClient) DismissNotification(eventID string) error {
	body := struct {
		EventID string `json:"event_id"`
	}{EventID: eventID}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/notifications/dismiss", c.port)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("X-Maestro-Token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned %d", resp.StatusCode)
	}
	return nil
}

// AnswerNotification submits an answer to a pending question.
func (c *DaemonClient) AnswerNotification(eventID string, selections []string, text string) error {
	body := struct {
		EventID    string   `json:"event_id"`
		Selections []string `json:"selections,omitempty"`
		Text       string   `json:"text,omitempty"`
	}{
		EventID:    eventID,
		Selections: selections,
		Text:       text,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/notifications/answer", c.port)
	req, err := http.NewRequest("POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return err
	}
	req.Header.Set("X-Maestro-Token", c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("daemon returned %d", resp.StatusCode)
	}
	return nil
}
