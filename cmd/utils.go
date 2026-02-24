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

package cmd

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/uprockcom/maestro/pkg/api"
	"github.com/uprockcom/maestro/pkg/containerservice"
)

// parseInt parses a string to int64
func parseInt(s string) int64 {
	i, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return i
}

// newContainerService creates a ContainerService using the daemon if available,
// with direct Docker fallback.
func newContainerService() containerservice.ContainerService {
	configDir := expandPath(config.Claude.AuthPath)
	svc, err := containerservice.New(configDir, config.Containers.Prefix)
	if err != nil {
		return containerservice.NewDocker(config.Containers.Prefix)
	}
	return svc
}

// isStateHashMismatch checks if an error is a 409 state hash conflict.
func isStateHashMismatch(err error) bool {
	var apiErr *api.Error
	return errors.As(err, &apiErr) && apiErr.Status == 409
}

// showDaemonNag shows a reminder to start the daemon if it's not running
func showDaemonNag() {
	if !config.Daemon.ShowNag {
		return
	}

	// Use HTTP-based check with short timeout
	if running, _ := isDaemonRunning(); running {
		return // Daemon is running, don't nag
	}

	fmt.Println("\n💡 Tip: Start the daemon for automatic token refresh and notifications:")
	fmt.Println("   maestro daemon start")
	fmt.Println("   (Disable this message: add 'show_nag: false' to daemon config)")
}

// generateTmuxConfig creates a tmux configuration string with true color support
func generateTmuxConfig(containerName, branchName string) string {
	return fmt.Sprintf(`# True color support
set -g default-terminal "tmux-256color"
set -ga terminal-overrides ",xterm-256color:Tc"
set -ga terminal-overrides ",tmux-256color:RGB"
set -as terminal-features ",*:RGB"

# Status bar configuration
set -g status-left '[%s | %s] '
set -g status-left-length 50
set -g status-right '%%%%H:%%%%M'`, containerName, branchName)
}

// resolveContainerName resolves a short name or full name to the actual container name
func resolveContainerName(shortName string) string {
	// If already has configured prefix, return as-is
	if strings.HasPrefix(shortName, config.Containers.Prefix) {
		return shortName
	}

	// If already has legacy "mcl-" prefix, return as-is (for backward compatibility)
	if strings.HasPrefix(shortName, "mcl-") {
		return shortName
	}

	// Try to find exact match with configured prefix
	fullName := config.Containers.Prefix + shortName

	// Check if this exact name exists
	checkCmd := exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("name=^%s$", fullName), "--format", "{{.Names}}")
	output, err := checkCmd.Output()
	if err == nil && len(output) > 0 {
		return strings.TrimSpace(string(output))
	}

	// Try pattern match (for cases where user omits the number)
	checkCmd = exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("name=%s", fullName), "--format", "{{.Names}}")
	output, err = checkCmd.Output()
	if err == nil && len(output) > 0 {
		names := strings.Split(string(output), "\n")
		if len(names) > 0 && names[0] != "" {
			// Return the most recent (highest numbered) match
			return strings.TrimSpace(names[0])
		}
	}

	// Try legacy "mcl-" prefix as fallback (for backward compatibility)
	if config.Containers.Prefix != "mcl-" {
		legacyFullName := "mcl-" + shortName

		// Check exact match with legacy prefix
		checkCmd = exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("name=^%s$", legacyFullName), "--format", "{{.Names}}")
		output, err = checkCmd.Output()
		if err == nil && len(output) > 0 {
			return strings.TrimSpace(string(output))
		}

		// Try pattern match with legacy prefix
		checkCmd = exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("name=%s", legacyFullName), "--format", "{{.Names}}")
		output, err = checkCmd.Output()
		if err == nil && len(output) > 0 {
			names := strings.Split(string(output), "\n")
			if len(names) > 0 && names[0] != "" {
				return strings.TrimSpace(names[0])
			}
		}
	}

	// Return the fullName as last resort
	return fullName
}
