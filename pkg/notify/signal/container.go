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
	"net/http"
	"os/exec"
	"strings"
	"time"
)

const (
	signalCLIContainer = "maestro-signal-cli"
	signalCLIImage     = "bbernhard/signal-cli-rest-api:latest"
	signalCLIVolume    = "maestro-signal-data"

	relayContainer = "maestro-signal-relay"
	relayImage     = "maestro-signal-relay:latest"
	relayConfigVol = "maestro-signal-relay-config"
	networkName    = "maestro-signal"
)

// EnsureRunning ensures both signal-cli and signal-relay containers are running
// on a shared Docker network. The relay is the only externally-accessible endpoint.
func EnsureRunning(relayPort int, botNumber string, logger func(string, ...interface{})) error {
	if relayPort == 0 {
		relayPort = 8080
	}

	// 1. Create Docker network (idempotent)
	if err := ensureNetwork(logger); err != nil {
		return err
	}

	// 2. Start signal-cli container
	if err := ensureContainer(signalCLIContainer, signalCLIImage, []string{
		"--network", networkName,
		"-v", fmt.Sprintf("%s:/home/.local/share/signal-cli", signalCLIVolume),
		"-e", "MODE=json",
		"--restart", "unless-stopped",
	}, logger); err != nil {
		return fmt.Errorf("signal-cli container: %w", err)
	}

	// 3. Start relay container
	if err := ensureContainer(relayContainer, relayImage, []string{
		"--network", networkName,
		"-p", fmt.Sprintf("127.0.0.1:%d:8080", relayPort),
		"-e", fmt.Sprintf("SIGNAL_API=http://%s:8080", signalCLIContainer),
		"-e", fmt.Sprintf("SIGNAL_NUMBER=%s", botNumber),
		"-e", "API_KEYS_FILE=/config/keys.json",
		"-v", fmt.Sprintf("%s:/config", relayConfigVol),
		"--restart", "unless-stopped",
	}, logger); err != nil {
		return fmt.Errorf("relay container: %w", err)
	}

	// 4. Health check relay
	return waitForRelayHealthy(relayPort, logger)
}

// Stop stops both signal-cli and relay containers.
func Stop(logger func(string, ...interface{})) error {
	var errs []string
	for _, name := range []string{relayContainer, signalCLIContainer} {
		cmd := exec.Command("docker", "stop", name)
		if err := cmd.Run(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		} else {
			logger("signal: stopped %s", name)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("failed to stop: %s", strings.Join(errs, "; "))
	}
	return nil
}

// IsRunning returns true if both signal-cli and relay containers are running.
func IsRunning() bool {
	return containerRunning(signalCLIContainer) && containerRunning(relayContainer)
}

// PullImage pulls the signal-cli-rest-api Docker image.
func PullImage(logger func(string, ...interface{})) error {
	logger("signal: pulling %s", signalCLIImage)
	cmd := exec.Command("docker", "pull", signalCLIImage)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to pull image: %w\n%s", err, string(out))
	}
	return nil
}

// RelayConfigVolume returns the name of the relay config volume.
func RelayConfigVolume() string {
	return relayConfigVol
}

// ensureNetwork creates the Docker network if it doesn't exist.
func ensureNetwork(logger func(string, ...interface{})) error {
	cmd := exec.Command("docker", "network", "inspect", networkName)
	if err := cmd.Run(); err == nil {
		return nil // already exists
	}
	logger("signal: creating network %s", networkName)
	cmd = exec.Command("docker", "network", "create", networkName)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create network %s: %w\n%s", networkName, err, string(out))
	}
	return nil
}

// ensureContainer starts a container if stopped, or creates it if missing.
func ensureContainer(name, image string, extraArgs []string, logger func(string, ...interface{})) error {
	inspectCmd := exec.Command("docker", "inspect", "--format", "{{.State.Status}}", name)
	output, err := inspectCmd.Output()
	if err == nil {
		status := strings.TrimSpace(string(output))
		switch status {
		case "running":
			logger("signal: container %s already running", name)
			return nil
		case "exited", "created":
			logger("signal: starting stopped container %s", name)
			startCmd := exec.Command("docker", "start", name)
			if err := startCmd.Run(); err != nil {
				return fmt.Errorf("failed to start %s: %w", name, err)
			}
			return nil
		}
	}

	// Container doesn't exist — create it
	logger("signal: creating container %s", name)
	args := append([]string{"run", "-d", "--name", name}, extraArgs...)
	args = append(args, image)
	runCmd := exec.Command("docker", args...)
	if out, err := runCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create %s: %w\n%s", name, err, string(out))
	}
	return nil
}

// containerRunning returns true if the named container is running.
func containerRunning(name string) bool {
	cmd := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", name)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// waitForRelayHealthy polls the relay's /health endpoint until it responds.
func waitForRelayHealthy(port int, logger func(string, ...interface{})) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(deadline) {
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/health", port))
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				logger("signal: relay healthy on port %d", port)
				return nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("signal relay did not become healthy within 30s")
}
