package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Client communicates with the daemon's typed HTTP API.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// DaemonIPCInfo is the JSON structure in daemon-ipc.json.
// Defined here in the api package so both daemon and clients share the same type.
type DaemonIPCInfo struct {
	Port       int    `json:"port"`
	BridgePort int    `json:"bridge_port,omitempty"`
	Token      string `json:"token"`
	PID        int    `json:"pid"`
}

// NewClientFromConfig reads daemon-ipc.json and creates a Client.
// Returns nil, nil if the daemon is not running (file not found or port=0).
func NewClientFromConfig(configDir string) (*Client, error) {
	ipcPath := filepath.Join(configDir, "daemon-ipc.json")
	data, err := os.ReadFile(ipcPath)
	if err != nil {
		return nil, nil // daemon not running
	}

	var info DaemonIPCInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("invalid daemon-ipc.json: %w", err)
	}
	if info.Port == 0 {
		return nil, nil
	}

	return &Client{
		BaseURL: fmt.Sprintf("http://127.0.0.1:%d", info.Port),
		Token:   info.Token,
		HTTPClient: &http.Client{
			// Generous fallback timeout to prevent indefinite hangs if the
			// daemon becomes unresponsive. Callers that need tighter control
			// use per-call context deadlines (which take precedence).
			Timeout: 5 * time.Minute,
		},
	}, nil
}

// Call invokes a typed API endpoint. For POST, the request is JSON-encoded as
// body. For GET, the request is encoded as query parameters. The response is
// JSON-decoded into the Resp type parameter.
func Call[Req, Resp any](ctx context.Context, c *Client, ep Endpoint[Req, Resp], req *Req) (*Resp, error) {
	var body *bytes.Buffer

	reqURL := c.BaseURL + ep.Path

	if ep.Method == http.MethodGet && req != nil {
		// Encode request as query parameters for GET
		if q := encodeQueryParams(req); q != "" {
			reqURL += "?" + q
		}
	} else if req != nil {
		body = &bytes.Buffer{}
		if err := json.NewEncoder(body).Encode(req); err != nil {
			return nil, fmt.Errorf("encoding request: %w", err)
		}
	}

	var httpReq *http.Request
	var err error
	if body != nil {
		httpReq, err = http.NewRequestWithContext(ctx, ep.Method, reqURL, body)
	} else {
		httpReq, err = http.NewRequestWithContext(ctx, ep.Method, reqURL, nil)
	}
	if err != nil {
		return nil, err
	}

	httpReq.Header.Set("X-Maestro-Token", c.Token)
	if body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	httpResp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != http.StatusOK {
		// Read body into buffer so we can try JSON decode, falling back to raw text
		body, _ := io.ReadAll(io.LimitReader(httpResp.Body, 4096))
		var errResp ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, &Error{
				Status:  httpResp.StatusCode,
				Message: errResp.Error,
			}
		}
		// Non-JSON error (proxy, gateway, etc.)
		msg := string(body)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", httpResp.StatusCode)
		}
		return nil, &Error{
			Status:  httpResp.StatusCode,
			Message: msg,
		}
	}

	var resp Resp
	if err := json.NewDecoder(httpResp.Body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &resp, nil
}

// encodeQueryParams serializes a struct to URL query parameters via JSON round-trip.
// Only non-zero-value fields are included.
func encodeQueryParams(req any) string {
	data, err := json.Marshal(req)
	if err != nil {
		return ""
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return ""
	}
	vals := url.Values{}
	for k, v := range m {
		s := string(v)
		// Strip JSON quotes from strings
		if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
			s = s[1 : len(s)-1]
		}
		// Skip null, empty string, and false booleans
		if s == "null" || s == "" || s == "false" {
			continue
		}
		vals.Set(k, s)
	}
	return vals.Encode()
}
