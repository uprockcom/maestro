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

// daemon-ipc is a minimal privilege-isolated binary for communicating with the
// Maestro daemon from inside containers. It is the ONLY process allowed to reach
// host.docker.internal (enforced via iptables uid-owner matching).
//
// Security properties:
//   - Hardcoded to connect only to "host.docker.internal" (no arbitrary hosts)
//   - Port read from daemon-ipc.json (not user-controlled)
//   - Token read from daemon-ipc.json (not passed as argument)
//   - Runs as dedicated "maestro-ipc" user via sudo
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	ipcFile = "/home/node/.maestro/daemon/daemon-ipc.json"
	host    = "host.docker.internal"
)

type ipcInfo struct {
	Port       int    `json:"port"`
	BridgePort int    `json:"bridge_port"`
	Token      string `json:"token"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: daemon-ipc <METHOD> <PATH> [BODY]\n")
		os.Exit(1)
	}

	method := strings.ToUpper(os.Args[1])
	path := os.Args[2]
	var body string
	if len(os.Args) > 3 {
		body = os.Args[3]
	}

	// Validate path starts with /
	if !strings.HasPrefix(path, "/") {
		fmt.Fprintf(os.Stderr, "Error: path must start with /\n")
		os.Exit(1)
	}

	// Read IPC info from mounted file
	data, err := os.ReadFile(ipcFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading %s: %v\n", ipcFile, err)
		os.Exit(1)
	}

	var info ipcInfo
	if err := json.Unmarshal(data, &info); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing %s: %v\n", ipcFile, err)
		os.Exit(1)
	}

	port := info.BridgePort
	if port == 0 {
		port = info.Port
	}
	if port == 0 {
		fmt.Fprintf(os.Stderr, "Error: no valid port in %s\n", ipcFile)
		os.Exit(1)
	}

	// Build request — only ever to host.docker.internal
	url := fmt.Sprintf("http://%s:%d%s", host, port, path)

	var reqBody io.Reader
	if body != "" {
		reqBody = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating request: %v\n", err)
		os.Exit(1)
	}

	req.Header.Set("X-Maestro-Token", info.Token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		os.Exit(1)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	fmt.Print(string(respBody))

	if resp.StatusCode >= 400 {
		os.Exit(1)
	}
}
