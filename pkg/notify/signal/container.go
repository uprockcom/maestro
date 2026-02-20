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
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	containerName = "maestro-signal-cli"
	imageName     = "bbernhard/signal-cli-rest-api:latest"
	volumeName    = "maestro-signal-data"
)

// EnsureRunning ensures the signal-cli container is running. It inspects,
// starts if stopped, or creates if missing, then health-checks.
func EnsureRunning(port int, logger func(string, ...interface{})) error {
	if port == 0 {
		port = 8080
	}

	// Check if container exists
	inspectCmd := exec.Command("docker", "inspect", "--format", "{{.State.Status}}", containerName)
	output, err := inspectCmd.Output()
	if err == nil {
		status := strings.TrimSpace(string(output))
		switch status {
		case "running":
			logger("signal: container %s already running", containerName)
			return waitForHealthy(port, logger)
		case "exited", "created":
			logger("signal: starting stopped container %s", containerName)
			startCmd := exec.Command("docker", "start", containerName)
			if err := startCmd.Run(); err != nil {
				return fmt.Errorf("failed to start container: %w", err)
			}
			return waitForHealthy(port, logger)
		}
	}

	// Container doesn't exist — create it
	logger("signal: creating container %s on port %d", containerName, port)
	runCmd := exec.Command("docker", "run", "-d",
		"--name", containerName,
		"-p", fmt.Sprintf("127.0.0.1:%d:8080", port),
		"-v", fmt.Sprintf("%s:/home/.local/share/signal-cli", volumeName),
		"-e", "MODE=json",
		"--restart", "unless-stopped",
		imageName,
	)
	if out, err := runCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create container: %w\n%s", err, string(out))
	}

	return waitForHealthy(port, logger)
}

// Stop stops the signal-cli container.
func Stop(logger func(string, ...interface{})) error {
	cmd := exec.Command("docker", "stop", containerName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop %s: %w", containerName, err)
	}
	logger("signal: stopped %s", containerName)
	return nil
}

// IsRunning returns true if the signal-cli container is running.
func IsRunning() bool {
	cmd := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", containerName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// PullImage pulls the signal-cli-rest-api Docker image.
func PullImage(logger func(string, ...interface{})) error {
	logger("signal: pulling %s", imageName)
	cmd := exec.Command("docker", "pull", imageName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to pull image: %w\n%s", err, string(out))
	}
	return nil
}

// waitForHealthy polls the /v1/about endpoint until it responds or times out.
func waitForHealthy(port int, logger func(string, ...interface{})) error {
	client := NewAPIClient(fmt.Sprintf("http://127.0.0.1:%d", port), "")
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		if _, err := client.About(); err == nil {
			logger("signal: container healthy on port %d", port)
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("signal-cli container did not become healthy within 30s")
}
