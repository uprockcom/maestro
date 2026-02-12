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
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const daemonIPCFile = "/home/node/.maestro/daemon/daemon-ipc.json"

// daemonCall executes the privilege-isolated daemon-ipc binary as the maestro-ipc user.
func daemonCall(method, path, body string) (string, error) {
	args := []string{"-u", "maestro-ipc", "daemon-ipc", method, path}
	if body != "" {
		args = append(args, body)
	}

	cmd := exec.Command("sudo", args...)
	cmd.Stderr = nil // suppress stderr from daemon-ipc
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("daemon-ipc %s %s failed: %w", method, path, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// daemonAvailable checks if the daemon IPC config file exists.
func daemonAvailable() bool {
	_, err := os.Stat(daemonIPCFile)
	return err == nil
}
