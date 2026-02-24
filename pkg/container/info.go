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

package container

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// infraContainers are maestro infrastructure containers that should be excluded
// from user-facing listings, stop, and cleanup operations.
var infraContainers = map[string]bool{
	"maestro-signal-cli":   true,
	"maestro-signal-relay": true,
}

// IsInfraContainer returns true if the container is a maestro infrastructure
// container (e.g. signal-cli, expose sidecars) that should not appear in
// user-facing listings.
func IsInfraContainer(name string) bool {
	return infraContainers[name] || strings.HasPrefix(name, "maestro-expose-")
}

// ReadCredentials loads and parses credentials from a file path
func ReadCredentials(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

// IsTokenExpired checks if token is expired (true) or valid (false)
func IsTokenExpired(creds *Credentials) bool {
	currentTimeMs := time.Now().UnixMilli()
	return creds.ClaudeAiOauth.ExpiresAt < currentTimeMs
}

// TimeUntilExpiration returns duration until token expires (negative if expired)
func TimeUntilExpiration(creds *Credentials) time.Duration {
	expiresAt := time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
	return time.Until(expiresAt)
}

// IsDockerResponsive checks if Docker daemon is responding
func IsDockerResponsive() bool {
	cmd := exec.Command("docker", "info")
	err := cmd.Run()
	return err == nil
}

// FormatExpiration returns human-readable expiration status
func FormatExpiration(creds *Credentials) string {
	duration := TimeUntilExpiration(creds)
	if duration < 0 {
		return fmt.Sprintf("EXPIRED %.1fh ago", -duration.Hours())
	}
	if duration < 24*time.Hour {
		return fmt.Sprintf("Valid for %.1fh", duration.Hours())
	}
	return fmt.Sprintf("Valid for %.1fd", duration.Hours()/24)
}

// GetShortName removes the prefix from a container name
func GetShortName(containerName, prefix string) string {
	if strings.HasPrefix(containerName, prefix) {
		return containerName[len(prefix):]
	}
	return containerName
}

// GetLabel reads a Docker label from a container.
func GetLabel(containerName, label string) string {
	cmd := exec.Command("docker", "inspect", "-f", fmt.Sprintf("{{index .Config.Labels %q}}", label), containerName)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	val := strings.TrimSpace(string(output))
	if val == "<no value>" {
		return ""
	}
	return val
}

// readContactsLabel reads and parses the maestro.contacts JSON label from a container.
func readContactsLabel(containerName string) map[string]map[string]string {
	raw := GetLabel(containerName, "maestro.contacts")
	if raw == "" {
		return nil
	}
	var contacts map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &contacts); err != nil {
		return nil
	}
	return contacts
}

// getWorkspaceDir returns the primary git workspace directory for a container.
// For multi-path projects this is read from the maestro.workspace label;
// for single-path and ad-hoc containers it defaults to /workspace.
func getWorkspaceDir(containerName string) string {
	if ws := GetLabel(containerName, "maestro.workspace"); ws != "" {
		return ws
	}
	return "/workspace"
}

// GetBranchName retrieves the current git branch from a container.
// For multi-path projects, it uses the maestro.workspace label to identify
// the primary repo directory. Falls back to /workspace for single-path and ad-hoc containers.
func GetBranchName(containerName string) string {
	gitDir := getWorkspaceDir(containerName)

	cmd := exec.Command("docker", "exec", containerName, "git", "-C", gitDir, "branch", "--show-current")
	output, err := cmd.Output()
	if err == nil {
		if branch := strings.TrimSpace(string(output)); branch != "" {
			return branch
		}
	}

	return "unknown"
}

// ReadAgentState reads the maestro-agent state from the container.
// Returns the state string (starting, active, waiting, idle, clearing, connected)
// or empty string if the state file doesn't exist (pre-maestro-agent containers).
func ReadAgentState(containerName string) string {
	cmd := exec.Command("docker", "exec", containerName,
		"cat", "/home/node/.maestro/state/agent-state")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// IsClaudeRunning checks if Claude process is running in a container
// Excludes zombie/defunct processes
func IsClaudeRunning(containerName string) bool {
	// Search for claude processes using [c]laude to avoid grep matching itself
	// Then filter out zombies (STAT column starts with 'Z')
	// The regex matches 7 columns followed by 'Z' at the start of the STAT column
	cmd := exec.Command("docker", "exec", containerName,
		"sh", "-c", "ps aux | grep -E '[c]laude' | grep -v -E '^\\S+\\s+\\S+\\s+\\S+\\s+\\S+\\s+\\S+\\s+\\S+\\s+\\S+\\s+Z'")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	// If we got any output, claude is running (and not a zombie)
	result := strings.TrimSpace(string(output))
	return result != ""
}

// GetAuthStatus retrieves the authentication status for a container
func GetAuthStatus(containerName string) string {
	// Extract credentials from container to temp file
	tmpFile := fmt.Sprintf("/tmp/maestro-creds-%s.json", containerName)
	defer os.Remove(tmpFile)

	copyCmd := exec.Command("docker", "cp",
		fmt.Sprintf("%s:/home/node/.claude/.credentials.json", containerName),
		tmpFile)
	if err := copyCmd.Run(); err != nil {
		return "✗ NO AUTH"
	}

	creds, err := ReadCredentials(tmpFile)
	if err != nil {
		return "✗ INVALID"
	}

	if IsTokenExpired(creds) {
		return "✗ EXPIRED"
	}

	duration := TimeUntilExpiration(creds)
	if duration < 24*time.Hour {
		return fmt.Sprintf("⚠ %.1fh", duration.Hours())
	}

	return fmt.Sprintf("✓ %.1fh", duration.Hours())
}

// GetRunningContainers returns a list of all running containers with the given prefix
func GetRunningContainers(prefix string) ([]Info, error) {
	dockerCmd := exec.Command("docker", "ps", "--format",
		"{{.Names}}\t{{.Status}}\t{{.State}}\t{{.CreatedAt}}\t{{.Label \"maestro.web\"}}")
	output, err := dockerCmd.Output()
	if err != nil {
		return nil, err
	}

	// Parse basic container info first
	type basicInfo struct {
		name      string
		status    string
		state     string
		createdAt time.Time
		hasWeb    bool
	}
	var basics []basicInfo

	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 4 {
			continue
		}

		name := parts[0]
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if IsInfraContainer(name) {
			continue
		}

		// Parse creation time
		createdAt, err := time.Parse("2006-01-02 15:04:05 -0700 MST", parts[3])
		if err != nil {
			createdAt = time.Time{}
		}

		hasWeb := false
		if len(parts) > 4 && parts[4] == "true" {
			hasWeb = true
		}

		basics = append(basics, basicInfo{
			name:      name,
			status:    parts[1],
			state:     parts[2],
			createdAt: createdAt,
			hasWeb:    hasWeb,
		})
	}

	// Fetch detailed info for all containers in parallel
	containers := make([]Info, len(basics))
	var wg sync.WaitGroup

	for i, b := range basics {
		wg.Add(1)
		go func(idx int, basic basicInfo) {
			defer wg.Done()

			info := Info{
				Name:          basic.name,
				ShortName:     GetShortName(basic.name, prefix),
				Status:        basic.state,
				StatusDetails: basic.status,
				CreatedAt:     basic.createdAt,
				HasWeb:        basic.hasWeb,
			}

			// Fetch details in parallel
			var detailWg sync.WaitGroup
			var mu sync.Mutex

			// Branch name
			detailWg.Add(1)
			go func() {
				defer detailWg.Done()
				branch := GetBranchName(basic.name)
				mu.Lock()
				info.Branch = branch
				mu.Unlock()
			}()

			// Agent state
			detailWg.Add(1)
			go func() {
				defer detailWg.Done()
				agentState := ReadAgentState(basic.name)
				mu.Lock()
				info.AgentState = agentState
				mu.Unlock()
			}()

			// Claude running check
			detailWg.Add(1)
			go func() {
				defer detailWg.Done()
				isDormant := !IsClaudeRunning(basic.name)
				mu.Lock()
				info.IsDormant = isDormant
				mu.Unlock()
			}()

			// Contacts label
			detailWg.Add(1)
			go func() {
				defer detailWg.Done()
				contacts := readContactsLabel(basic.name)
				mu.Lock()
				info.Contacts = contacts
				mu.Unlock()
			}()

			detailWg.Wait()
			containers[idx] = info
		}(i, b)
	}

	wg.Wait()
	return containers, nil
}

// GetAllContainers returns a list of all containers (including stopped) with the given prefix
func GetAllContainers(prefix string) ([]Info, error) {
	dockerCmd := exec.Command("docker", "ps", "-a", "--format",
		"{{.Names}}\t{{.Status}}\t{{.State}}\t{{.CreatedAt}}\t{{.Label \"maestro.web\"}}")
	output, err := dockerCmd.Output()
	if err != nil {
		return nil, err
	}

	// Parse basic container info first
	type basicInfo struct {
		name      string
		status    string
		state     string
		createdAt time.Time
		hasWeb    bool
	}
	var basics []basicInfo

	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 4 {
			continue
		}

		name := parts[0]
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		if IsInfraContainer(name) {
			continue
		}

		// Parse creation time
		createdAt, err := time.Parse("2006-01-02 15:04:05 -0700 MST", parts[3])
		if err != nil {
			createdAt = time.Time{}
		}

		hasWeb := false
		if len(parts) > 4 && parts[4] == "true" {
			hasWeb = true
		}

		basics = append(basics, basicInfo{
			name:      name,
			status:    parts[1],
			state:     parts[2],
			createdAt: createdAt,
			hasWeb:    hasWeb,
		})
	}

	// Fetch detailed info for all containers in parallel
	containers := make([]Info, len(basics))
	var wg sync.WaitGroup

	for i, b := range basics {
		wg.Add(1)
		go func(idx int, basic basicInfo) {
			defer wg.Done()

			info := Info{
				Name:          basic.name,
				ShortName:     GetShortName(basic.name, prefix),
				Status:        basic.state,
				StatusDetails: basic.status,
				CreatedAt:     basic.createdAt,
				HasWeb:        basic.hasWeb,
				LastActivity:  "-",
				GitStatus:     "-",
			}

			// For running containers, fetch detailed info in parallel
			if basic.state == "running" {
				var detailWg sync.WaitGroup
				var mu sync.Mutex

				// Branch name
				detailWg.Add(1)
				go func() {
					defer detailWg.Done()
					branch := GetBranchName(basic.name)
					mu.Lock()
					info.Branch = branch
					mu.Unlock()
				}()

				// Agent state
				detailWg.Add(1)
				go func() {
					defer detailWg.Done()
					agentState := ReadAgentState(basic.name)
					mu.Lock()
					info.AgentState = agentState
					mu.Unlock()
				}()

				// Claude running check
				detailWg.Add(1)
				go func() {
					defer detailWg.Done()
					isDormant := !IsClaudeRunning(basic.name)
					mu.Lock()
					info.IsDormant = isDormant
					mu.Unlock()
				}()

				// Auth status
				detailWg.Add(1)
				go func() {
					defer detailWg.Done()
					authStatus := GetAuthStatus(basic.name)
					mu.Lock()
					info.AuthStatus = authStatus
					mu.Unlock()
				}()

				// Last activity
				detailWg.Add(1)
				go func() {
					defer detailWg.Done()
					lastActivity := GetLastActivity(basic.name)
					mu.Lock()
					info.LastActivity = lastActivity
					mu.Unlock()
				}()

				// Git status
				detailWg.Add(1)
				go func() {
					defer detailWg.Done()
					gitStatus := GetGitStatus(basic.name)
					mu.Lock()
					info.GitStatus = gitStatus
					mu.Unlock()
				}()

				// Task status
				detailWg.Add(1)
				go func() {
					defer detailWg.Done()
					taskSummary := GetTaskSummary(basic.name)
					mu.Lock()
					info.CurrentTask = taskSummary.CurrentTask
					info.TaskProgress = taskSummary.Progress
					mu.Unlock()
				}()

				// Contacts label
				detailWg.Add(1)
				go func() {
					defer detailWg.Done()
					contacts := readContactsLabel(basic.name)
					mu.Lock()
					info.Contacts = contacts
					mu.Unlock()
				}()

				detailWg.Wait()
			} else {
				// For stopped containers, just get branch name
				info.Branch = GetBranchName(basic.name)
			}

			containers[idx] = info
		}(i, b)
	}

	wg.Wait()
	return containers, nil
}

// GetLastActivity gets the last activity time for a container
func GetLastActivity(containerName string) string {
	// Check docker container stats for last activity via process CPU usage
	// For now, we'll use a simpler approach: check tmux pane activity
	cmd := exec.Command("docker", "exec", containerName,
		"tmux", "display-message", "-t", "main:0", "-p", "#{pane_active_since}")
	output, err := cmd.Output()
	if err != nil {
		return "-"
	}

	// Parse Unix timestamp
	timestampStr := strings.TrimSpace(string(output))
	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	if err != nil {
		return "-"
	}

	lastActive := time.Unix(timestamp, 0)
	duration := time.Since(lastActive)

	return formatDuration(duration)
}

// formatDuration formats a duration in human-readable form
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%.1fh", d.Hours())
	}
	return fmt.Sprintf("%.1fd", d.Hours()/24)
}

// GetGitStatus gets git status indicators for a container
// Returns a fixed-width string for proper column alignment
func GetGitStatus(containerName string) string {
	wsDir := getWorkspaceDir(containerName)

	// Check if git repo exists
	checkCmd := exec.Command("docker", "exec", containerName, "test", "-d", wsDir+"/.git")
	if err := checkCmd.Run(); err != nil {
		return padGitStatus("-")
	}

	var indicators []string

	// Check for uncommitted changes
	statusCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		fmt.Sprintf("cd %s && git status --porcelain 2>/dev/null | wc -l", wsDir))
	if output, err := statusCmd.Output(); err == nil {
		count := strings.TrimSpace(string(output))
		if count != "0" {
			indicators = append(indicators, fmt.Sprintf("Δ%s", count))
		}
	}

	// Check commits ahead of remote
	aheadCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		fmt.Sprintf("cd %s && git rev-list --count @{u}..HEAD 2>/dev/null", wsDir))
	if output, err := aheadCmd.Output(); err == nil {
		count := strings.TrimSpace(string(output))
		if count != "0" && count != "" {
			indicators = append(indicators, fmt.Sprintf("↑%s", count))
		}
	}

	// Check commits behind remote
	behindCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		fmt.Sprintf("cd %s && git rev-list --count HEAD..@{u} 2>/dev/null", wsDir))
	if output, err := behindCmd.Output(); err == nil {
		count := strings.TrimSpace(string(output))
		if count != "0" && count != "" {
			indicators = append(indicators, fmt.Sprintf("↓%s", count))
		}
	}

	if len(indicators) == 0 {
		return padGitStatus("✓")
	}

	return padGitStatus(strings.Join(indicators, " "))
}

// padGitStatus pads git status to fixed width for alignment
func padGitStatus(status string) string {
	// Pad to 10 characters for consistent column width
	const width = 10
	if len(status) >= width {
		return status
	}
	return status + strings.Repeat(" ", width-len(status))
}

// GetContainerDetails fetches comprehensive information about a container
func GetContainerDetails(containerName, prefix string) (*ContainerDetails, error) {
	// Use docker inspect to get detailed container info
	inspectCmd := exec.Command("docker", "inspect", containerName)
	output, err := inspectCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	// Parse JSON output
	var inspectData []map[string]interface{}
	if err := json.Unmarshal(output, &inspectData); err != nil {
		return nil, fmt.Errorf("failed to parse inspect data: %w", err)
	}

	if len(inspectData) == 0 {
		return nil, fmt.Errorf("no container data returned")
	}

	data := inspectData[0]

	details := &ContainerDetails{
		Name:      containerName,
		ShortName: GetShortName(containerName, prefix),
	}

	// Extract state information
	if state, ok := data["State"].(map[string]interface{}); ok {
		if status, ok := state["Status"].(string); ok {
			details.Status = status
		}
		if startedAt, ok := state["StartedAt"].(string); ok {
			if started, err := time.Parse(time.RFC3339Nano, startedAt); err == nil {
				uptime := time.Since(started)
				details.Uptime = formatDuration(uptime)
			}
		}
	}

	// Extract host config (resources)
	if hostConfig, ok := data["HostConfig"].(map[string]interface{}); ok {
		if cpuCount, ok := hostConfig["NanoCpus"].(float64); ok && cpuCount > 0 {
			details.CPUs = fmt.Sprintf("%.1f", cpuCount/1e9)
		} else {
			details.CPUs = "unlimited"
		}

		if memory, ok := hostConfig["Memory"].(float64); ok && memory > 0 {
			details.Memory = fmt.Sprintf("%.1f GB", memory/(1024*1024*1024))
		} else {
			details.Memory = "unlimited"
		}
	}

	// Extract network settings
	if networkSettings, ok := data["NetworkSettings"].(map[string]interface{}); ok {
		if ipAddress, ok := networkSettings["IPAddress"].(string); ok {
			details.IPAddress = ipAddress
		}

		if ports, ok := networkSettings["Ports"].(map[string]interface{}); ok {
			for containerPort, bindings := range ports {
				if bindingsList, ok := bindings.([]interface{}); ok && len(bindingsList) > 0 {
					for _, binding := range bindingsList {
						if b, ok := binding.(map[string]interface{}); ok {
							hostPort := b["HostPort"].(string)
							details.Ports = append(details.Ports, fmt.Sprintf("%s -> %s", hostPort, containerPort))
						}
					}
				}
			}
		}
	}

	// Extract mounts (volumes)
	if mounts, ok := data["Mounts"].([]interface{}); ok {
		for _, mount := range mounts {
			if m, ok := mount.(map[string]interface{}); ok {
				source := m["Source"].(string)
				destination := m["Destination"].(string)
				details.Volumes = append(details.Volumes, fmt.Sprintf("%s -> %s", source, destination))
			}
		}
	}

	// Extract environment variables (filter sensitive ones)
	if config, ok := data["Config"].(map[string]interface{}); ok {
		if env, ok := config["Env"].([]interface{}); ok {
			for _, e := range env {
				if envStr, ok := e.(string); ok {
					// Filter out sensitive variables
					if !strings.Contains(envStr, "TOKEN") && !strings.Contains(envStr, "SECRET") && !strings.Contains(envStr, "PASSWORD") {
						details.Environment = append(details.Environment, envStr)
					}
				}
			}
		}

		// Get status string
		if status, ok := config["Status"].(string); ok {
			details.StatusDetails = status
		}
	}

	// Get branch, git status, and auth status from existing functions
	details.Branch = GetBranchName(containerName)
	if details.Status == "running" {
		details.GitStatus = GetGitStatus(containerName)
		details.AuthStatus = GetAuthStatus(containerName)
		details.LastActivity = GetLastActivity(containerName)
	} else {
		details.GitStatus = "-"
		details.AuthStatus = "-"
		details.LastActivity = "-"
	}

	// Get recent logs (last 50 lines)
	logsCmd := exec.Command("docker", "logs", "--tail", "50", containerName)
	logsOutput, err := logsCmd.CombinedOutput()
	if err == nil {
		details.RecentLogs = string(logsOutput)
	} else {
		details.RecentLogs = "(logs unavailable)"
	}

	return details, nil
}
