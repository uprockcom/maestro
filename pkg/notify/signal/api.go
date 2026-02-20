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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// APIClient communicates with the signal-cli-rest-api container or a relay server.
type APIClient struct {
	baseURL    string
	number     string
	apiKey     string
	httpClient *http.Client
}

// NewAPIClient creates a new Signal REST API client.
func NewAPIClient(baseURL, number string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		number:  number,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// NewAPIClientWithKey creates a new Signal REST API client with API key auth (for relay mode).
func NewAPIClientWithKey(baseURL, number, apiKey string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		number:  number,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// doRequest executes an HTTP request, injecting the API key header when set.
func (c *APIClient) doRequest(req *http.Request) (*http.Response, error) {
	if c.apiKey != "" {
		req.Header.Set("X-API-Key", c.apiKey)
	}
	return c.httpClient.Do(req)
}

// AboutInfo contains information from the /v1/about endpoint.
type AboutInfo struct {
	Versions []string `json:"versions"`
}

// About checks if the signal-cli-rest-api is responsive.
func (c *APIClient) About() (*AboutInfo, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/v1/about", nil)
	if err != nil {
		return nil, fmt.Errorf("about request failed: %w", err)
	}
	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("about request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("about returned %d: %s", resp.StatusCode, string(body))
	}

	var info AboutInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to decode about response: %w", err)
	}
	return &info, nil
}

// sendRequest is a JSON payload for POST /v2/send.
type sendRequest struct {
	Number     string   `json:"number"`
	Recipients []string `json:"recipients"`
	Message    string   `json:"message"`
}

// sendResponse is the JSON response from POST /v2/send.
type sendResponse struct {
	Timestamp string `json:"timestamp"` // sent message timestamp (used for reply-to matching)
}

// SendMessage sends a text message to the given recipient.
// Returns the sent message timestamp (used to match reply-to responses).
func (c *APIClient) SendMessage(recipient, text string) (int64, error) {
	payload := sendRequest{
		Number:     c.number,
		Recipients: []string{recipient},
		Message:    text,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal send request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/v2/send", bytes.NewReader(body))
	if err != nil {
		return 0, fmt.Errorf("send request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doRequest(req)
	if err != nil {
		return 0, fmt.Errorf("send request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("send returned %d: %s", resp.StatusCode, string(respBody))
	}

	var sendResp sendResponse
	if err := json.NewDecoder(resp.Body).Decode(&sendResp); err != nil {
		// Non-fatal — message was sent, we just can't match replies to it
		return 0, nil
	}

	// Parse timestamp string to int64
	var ts int64
	fmt.Sscanf(sendResp.Timestamp, "%d", &ts)
	return ts, nil
}

// ReceivedMessage represents a message received from the Signal API.
type ReceivedMessage struct {
	Envelope struct {
		Source      string `json:"source"`
		Timestamp   int64  `json:"timestamp"`
		DataMessage *struct {
			Message   string `json:"message"`
			Timestamp int64  `json:"timestamp"`
			Quote     *struct {
				ID     int64  `json:"id"`     // timestamp of the quoted message
				Author string `json:"author"` // sender of the quoted message
				Text   string `json:"text"`   // text of the quoted message
			} `json:"quote"`
		} `json:"dataMessage"`
	} `json:"envelope"`
}

// ReceiveResult wraps received messages with the highest message ID for cursor tracking.
type ReceiveResult struct {
	Messages []ReceivedMessage
	MaxID    uint64 // highest message ID returned (for cursor-based polling)
}

// Receive fetches pending messages for the registered number.
// In local mode (direct signal-cli), this is a destructive read.
// In relay mode (afterCursor > 0 or apiKey set), uses cursor-based non-destructive polling.
func (c *APIClient) Receive(afterCursor uint64) (*ReceiveResult, error) {
	encodedNumber := url.PathEscape(c.number)
	reqURL := c.baseURL + "/v1/receive/" + encodedNumber
	if afterCursor > 0 {
		reqURL += fmt.Sprintf("?after=%d", afterCursor)
	}

	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("receive request failed: %w", err)
	}
	resp, err := c.doRequest(req)
	if err != nil {
		return nil, fmt.Errorf("receive request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("receive returned %d: %s", resp.StatusCode, string(body))
	}

	var messages []ReceivedMessage
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return nil, fmt.Errorf("failed to decode receive response: %w", err)
	}

	// Track highest message ID from response header (relay sets X-Max-ID)
	var maxID uint64
	if hdr := resp.Header.Get("X-Max-ID"); hdr != "" {
		fmt.Sscanf(hdr, "%d", &maxID)
	}

	return &ReceiveResult{Messages: messages, MaxID: maxID}, nil
}

// registerRequest is the JSON payload for POST /v1/register/<number>.
type registerRequest struct {
	UseVoice bool   `json:"use_voice"`
	Captcha  string `json:"captcha,omitempty"`
}

// Register starts the registration process for a phone number (triggers SMS).
// captcha is optional — pass "" if not required.
func (c *APIClient) Register(number, captcha string) error {
	encodedNumber := url.PathEscape(number)
	payload := registerRequest{
		UseVoice: false,
		Captcha:  captcha,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal register request: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/v1/register/"+encodedNumber, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("register request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doRequest(req)
	if err != nil {
		return fmt.Errorf("register request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// Verify completes the registration with the SMS verification code.
func (c *APIClient) Verify(number, code string) error {
	encodedNumber := url.PathEscape(number)
	req, err := http.NewRequest("POST", c.baseURL+"/v1/register/"+encodedNumber+"/verify/"+code, nil)
	if err != nil {
		return fmt.Errorf("verify request failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.doRequest(req)
	if err != nil {
		return fmt.Errorf("verify request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("verify returned %d: %s", resp.StatusCode, string(body))
	}
	return nil
}
