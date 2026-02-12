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

package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/uprockcom/maestro/pkg/container"
)

// Config holds daemon configuration
type Config struct {
	CheckInterval      time.Duration
	TokenThreshold     time.Duration
	NotificationsOn    bool
	AttentionThreshold time.Duration
	NotifyOn           []string
	QuietHoursStart    string
	QuietHoursEnd      string
	ContainerPrefix    string
	CreateContainer    func(task, parentContainer, branch string) (string, error) // Callback for IPC child creation
}

// Daemon manages background monitoring and auto-refresh
type Daemon struct {
	config              Config
	logFile             *os.File
	stopChan            chan bool
	stopOnce            sync.Once
	mu                  sync.Mutex // protects containerStates
	containerStates     map[string]*ContainerState
	iconPath            string // Cached icon path for notifications
	hasTerminalNotifier bool   // Whether terminal-notifier is available
	ipcServer           *IPCServer
	startTime           time.Time
	ipcToken            string
	configDir           string
	lockFile            *os.File       // held while daemon is running to prevent races
	wg                  sync.WaitGroup // tracks background goroutines for clean shutdown
}

// ContainerState tracks container monitoring state
type ContainerState struct {
	mu                     sync.Mutex // protects all fields below
	Name                   string
	AttentionStarted       *time.Time
	LastNotified           *time.Time
	LastActivity           time.Time
	LastTokenCheck         time.Time
	NotificationSent       bool
	LastTaskCheck          time.Time
	HadOpenTasks           bool   // Whether container had incomplete tasks last check
	LastTaskProgress       string // Last seen task progress (e.g., "2/5")
	TaskCompletionNotified bool   // Whether we've notified about task completion
	LastIPCCheck           time.Time
}

// New creates a new daemon instance
func New(config Config, configDir string, iconData []byte) (*Daemon, error) {
	logPath := filepath.Join(configDir, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	// Generate auth token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		logFile.Close()
		return nil, fmt.Errorf("failed to generate auth token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	d := &Daemon{
		config:          config,
		logFile:         logFile,
		stopChan:        make(chan bool),
		containerStates: make(map[string]*ContainerState),
		startTime:       time.Now(),
		ipcToken:        token,
		configDir:       configDir,
	}

	// Check for terminal-notifier on macOS
	if runtime.GOOS == "darwin" {
		cmd := exec.Command("which", "terminal-notifier")
		if err := cmd.Run(); err == nil {
			d.hasTerminalNotifier = true
		}
	}

	// Cache icon to temp location for platforms that support it
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		if len(iconData) > 0 {
			iconPath := filepath.Join(configDir,"notification-icon.png")
			if err := os.WriteFile(iconPath, iconData, 0644); err == nil {
				// Ensure path is absolute
				if absPath, err := filepath.Abs(iconPath); err == nil {
					d.iconPath = absPath
				} else {
					d.iconPath = iconPath
				}
			}
		}
	}

	return d, nil
}

// DaemonIPCInfo is the JSON structure written to daemon-ipc.json
type DaemonIPCInfo struct {
	Port       int    `json:"port"`
	BridgePort int    `json:"bridge_port,omitempty"`
	Token      string `json:"token"`
	PID        int    `json:"pid"`
}

// Start begins the daemon monitoring loop
func (d *Daemon) Start() error {
	log.SetOutput(d.logFile)
	d.logInfo("Daemon started on %s", runtime.GOOS)

	// Check notification support and warn if needed
	if d.config.NotificationsOn {
		if err := d.checkNotificationSupport(); err != nil {
			d.logError("Notification support check failed: %v", err)
			d.logInfo("Continuing without notifications...")
		} else {
			// Log notification configuration
			switch runtime.GOOS {
			case "darwin":
				if d.hasTerminalNotifier {
					d.logInfo("Using terminal-notifier for notifications")
					if d.iconPath != "" {
						d.logInfo("Custom icon path: %s", d.iconPath)
					} else {
						d.logInfo("No custom icon configured")
					}
				} else {
					d.logInfo("Using osascript for notifications (no custom icon)")
				}
			case "linux":
				d.logInfo("Using notify-send for notifications")
				if d.iconPath != "" {
					d.logInfo("Custom icon path: %s", d.iconPath)
				}
			}

			// Send welcome notification
			d.notify("Daemon Started", "", "Maestro daemon is now monitoring your containers")
			d.logInfo("Notifications enabled and working")
		}
	}

	// Acquire file lock to prevent concurrent daemon starts (Issue #7)
	lockPath := filepath.Join(d.configDir, "daemon.lock")
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("failed to open lock file: %w", err)
	}
	if err := acquireFileLock(lockFile); err != nil {
		lockFile.Close()
		return fmt.Errorf("another daemon is already starting (could not acquire lock): %w", err)
	}
	d.lockFile = lockFile

	// Double-check: if daemon-ipc.json exists and daemon is responsive, bail out
	ipcFilePath := filepath.Join(d.configDir, "daemon-ipc.json")
	if data, err := os.ReadFile(ipcFilePath); err == nil {
		var existing DaemonIPCInfo
		if json.Unmarshal(data, &existing) == nil && existing.Port > 0 {
			client := &http.Client{Timeout: 2 * time.Second}
			if resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/status", existing.Port)); err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					releaseFileLock(d.lockFile)
					d.lockFile.Close()
					return fmt.Errorf("daemon is already running (port %d, PID %d)", existing.Port, existing.PID)
				}
			}
		}
	}

	// Start IPC server (always — it provides lifecycle endpoints too)
	ipcServer, err := NewIPCServer(d, d.ipcToken)
	if err != nil {
		releaseFileLock(d.lockFile)
		d.lockFile.Close()
		return fmt.Errorf("failed to start IPC server: %w", err)
	}
	d.ipcServer = ipcServer
	d.ipcServer.Start()

	// Write daemon-ipc.json so CLI and containers can find us
	ipcInfo := DaemonIPCInfo{
		Port:       d.ipcServer.LoopbackPort(),
		BridgePort: d.ipcServer.BridgePort(),
		Token:      d.ipcToken,
		PID:        os.Getpid(),
	}
	ipcInfoJSON, err := json.MarshalIndent(ipcInfo, "", "  ")
	if err != nil {
		d.ipcServer.Stop()
		releaseFileLock(d.lockFile)
		d.lockFile.Close()
		return fmt.Errorf("failed to marshal IPC info: %w", err)
	}
	if err := os.WriteFile(ipcFilePath, ipcInfoJSON, 0600); err != nil {
		d.ipcServer.Stop()
		releaseFileLock(d.lockFile)
		d.lockFile.Close()
		return fmt.Errorf("failed to write daemon-ipc.json: %w", err)
	}
	d.logInfo("IPC info written to %s (port %d, bridge_port %d)", ipcFilePath, ipcInfo.Port, ipcInfo.BridgePort)

	// Handle OS signals for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Main monitoring loop
	ticker := time.NewTicker(d.config.CheckInterval)
	defer ticker.Stop()

	// Run initial check immediately
	d.check()

	for {
		select {
		case <-ticker.C:
			d.check()
		case sig := <-sigChan:
			d.logInfo("Received signal %v, shutting down", sig)
			d.cleanup()
			return nil
		case <-d.stopChan:
			d.logInfo("Daemon stopping")
			d.cleanup()
			return nil
		}
	}
}

// Stop signals the daemon to stop (safe to call multiple times)
func (d *Daemon) Stop() {
	d.stopOnce.Do(func() { close(d.stopChan) })
}

// check performs one monitoring cycle
func (d *Daemon) check() {
	containers, err := d.getRunningContainers()
	if err != nil {
		d.logError("Failed to get containers: %v", err)
		return
	}

	for _, container := range containers {
		state := d.getOrCreateContainerState(container)

		// Check token expiry
		d.checkTokenExpiry(container, state)

		// Check attention status
		d.checkAttentionStatus(container, state)

		// Check task completion status
		d.checkTaskStatus(container, state)

		// Check for pending IPC requests (recovery)
		if d.ipcServer != nil {
			d.ipcServer.checkPendingRequests(container, state)
		}
	}

	// Notify parents of stopped child containers
	if d.ipcServer != nil {
		d.ipcServer.notifyStoppedChildren(containers)
	}

	// Cleanup states for removed containers
	d.cleanupStates(containers)
}

// checkTokenExpiry checks and refreshes tokens if needed
func (d *Daemon) checkTokenExpiry(container string, state *ContainerState) {
	// Don't check too frequently (every 5 minutes is enough)
	state.mu.Lock()
	if time.Since(state.LastTokenCheck) < 5*time.Minute {
		state.mu.Unlock()
		return
	}
	state.LastTokenCheck = time.Now()
	state.mu.Unlock()

	// Extract credentials
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("maestro-creds-%s-%d.json", container, time.Now().Unix()))
	defer os.Remove(tmpFile)

	copyCmd := exec.Command("docker", "cp",
		fmt.Sprintf("%s:/home/node/.claude/.credentials.json", container),
		tmpFile)
	if err := copyCmd.Run(); err != nil {
		return // No credentials, skip
	}

	creds, err := readCredentials(tmpFile)
	if err != nil {
		return
	}

	timeLeft := time.Until(time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt))

	// Refresh if below threshold
	if timeLeft < d.config.TokenThreshold {
		d.logInfo("Token expiring soon for %s (%.1fh left), refreshing...", container, timeLeft.Hours())

		if err := d.refreshToken(container); err != nil {
			d.logError("Failed to refresh token for %s: %v", container, err)

			// Send notification if enabled
			if d.shouldNotify("token_expiring", state) {
				d.notify("Token Expiring", d.getShortName(container),
					fmt.Sprintf("Token expires in %.1fh and auto-refresh failed", timeLeft.Hours()))
				state.mu.Lock()
				state.LastNotified = timeNow()
				state.mu.Unlock()
			}
		} else {
			d.logInfo("Successfully refreshed token for %s", container)
		}
	}
}

// checkAttentionStatus monitors container attention state
func (d *Daemon) checkAttentionStatus(container string, state *ContainerState) {
	// Only treat idle flag as "needs attention" if Claude is actually running.
	// When Claude exits, the flag file persists but is stale.
	needsAttention := d.checkIdleStatus(container) && d.isClaudeRunning(container)

	state.mu.Lock()
	if needsAttention {
		// Track when attention started
		newAttention := false
		if state.AttentionStarted == nil {
			now := time.Now()
			state.AttentionStarted = &now
			state.NotificationSent = false
			newAttention = true
		}

		// Check if we should notify
		attentionDuration := time.Since(*state.AttentionStarted)
		shouldSend := !state.NotificationSent && attentionDuration >= d.config.AttentionThreshold
		state.mu.Unlock()

		if newAttention {
			d.logInfo("Container %s needs attention", d.getShortName(container))
		}

		if shouldSend && d.shouldNotify("attention_needed", state) {
			d.notify("Needs Attention", d.getShortName(container),
				fmt.Sprintf("Has needed attention for %s", formatDuration(attentionDuration)))
			state.mu.Lock()
			state.NotificationSent = true
			state.LastNotified = timeNow()
			state.mu.Unlock()
		}
	} else {
		// Clear attention state
		wasAttending := state.AttentionStarted != nil
		state.AttentionStarted = nil
		state.NotificationSent = false
		state.mu.Unlock()

		if wasAttending {
			d.logInfo("Container %s attention resolved", d.getShortName(container))
		}
	}
}

// checkTaskStatus monitors task completion and sends notifications
func (d *Daemon) checkTaskStatus(containerName string, state *ContainerState) {
	// Don't check too frequently (every 30 seconds is enough)
	state.mu.Lock()
	if time.Since(state.LastTaskCheck) < 30*time.Second {
		state.mu.Unlock()
		return
	}
	state.LastTaskCheck = time.Now()
	state.mu.Unlock()

	// Get task summary from the container (I/O — no lock held)
	summary := container.GetTaskSummary(containerName)

	// Determine if there are open (incomplete) tasks
	hasOpenTasks := false
	if summary.HasTasks {
		if summary.CurrentTask != "" {
			hasOpenTasks = true
		} else if summary.Progress != "" {
			var completed, total int
			if _, err := fmt.Sscanf(summary.Progress, "%d/%d", &completed, &total); err == nil {
				hasOpenTasks = completed < total
			}
		}
	}

	state.mu.Lock()
	// Detect transition from having open tasks to all complete
	shouldNotifyCompletion := state.HadOpenTasks && !hasOpenTasks && summary.HasTasks && !state.TaskCompletionNotified
	state.mu.Unlock()

	if shouldNotifyCompletion {
		d.logInfo("Container %s completed all tasks (%s)", d.getShortName(containerName), summary.Progress)

		if d.shouldNotify("tasks_completed", state) {
			d.notify("Tasks Completed", d.getShortName(containerName),
				fmt.Sprintf("Finished all tasks (%s)", summary.Progress))
			state.mu.Lock()
			state.TaskCompletionNotified = true
			state.LastNotified = timeNow()
			state.mu.Unlock()
		}
	}

	// Update state for next check
	state.mu.Lock()
	if hasOpenTasks {
		state.TaskCompletionNotified = false
	}
	state.HadOpenTasks = hasOpenTasks
	state.LastTaskProgress = summary.Progress
	state.mu.Unlock()
}

// refreshToken refreshes the token for a container
func (d *Daemon) refreshToken(containerName string) error {
	// This will be implemented once we have the refresh-tokens command
	// For now, just log that we would refresh
	return fmt.Errorf("token refresh not yet implemented")
}

// shouldNotify checks if notification should be sent based on rules
func (d *Daemon) shouldNotify(notifyType string, state *ContainerState) bool {
	if !d.config.NotificationsOn {
		return false
	}

	// Check if this notification type is enabled
	enabled := false
	for _, nt := range d.config.NotifyOn {
		if nt == notifyType {
			enabled = true
			break
		}
	}
	if !enabled {
		return false
	}

	// Check quiet hours
	if d.isQuietHours() {
		return false
	}

	// Rate limit: don't notify more than once per 30 minutes per container
	state.mu.Lock()
	rateLimited := state.LastNotified != nil && time.Since(*state.LastNotified) < 30*time.Minute
	state.mu.Unlock()
	if rateLimited {
		return false
	}

	return true
}

// notify sends a desktop notification.
// subtitle is optional — pass "" to omit it (used for container name on IPC notifications).
func (d *Daemon) notify(title, subtitle, message string) {
	switch runtime.GOOS {
	case "darwin":
		// Try terminal-notifier first (better subtitle + icon support)
		if d.hasTerminalNotifier {
			args := []string{
				"-title", fmt.Sprintf("Maestro - %s", title),
				"-message", message,
			}
			if subtitle != "" {
				args = append(args, "-subtitle", subtitle)
			}

			// Add custom icon as content image if available
			// Note: -appIcon is often blocked by macOS security
			// -contentImage shows the icon inside the notification body
			if d.iconPath != "" {
				args = append(args, "-contentImage", d.iconPath)
			}

			cmd := exec.Command("terminal-notifier", args...)
			if err := cmd.Run(); err == nil {
				return // Success!
			}
			// Fall through to osascript on error
			d.logError("terminal-notifier failed, falling back to osascript")
		}

		// Fallback: macOS notification via osascript (no custom icon)
		// osascript supports subtitle via "with title T subtitle S"
		// Use "on run argv" to pass user strings as process arguments,
		// eliminating any injection risk (strings never touch the script source)
		if subtitle != "" {
			cmd := exec.Command("osascript",
				"-e", `on run argv`,
				"-e", `display notification (item 1 of argv) with title ("Maestro - " & item 2 of argv) subtitle (item 3 of argv)`,
				"-e", `end run`,
				"--",
				message, title, subtitle,
			)
			if err := cmd.Run(); err != nil {
				d.logError("Failed to send macOS notification: %v", err)
			}
		} else {
			cmd := exec.Command("osascript",
				"-e", `on run argv`,
				"-e", `display notification (item 1 of argv) with title ("Maestro - " & item 2 of argv)`,
				"-e", `end run`,
				"--",
				message, title,
			)
			if err := cmd.Run(); err != nil {
				d.logError("Failed to send macOS notification: %v", err)
			}
		}
	case "linux":
		// Linux notification via notify-send (no subtitle support)
		// Prepend subtitle to message if present
		var args []string
		if d.iconPath != "" {
			args = append(args, "--icon", d.iconPath)
		}
		displayMsg := message
		if subtitle != "" {
			displayMsg = fmt.Sprintf("[%s] %s", subtitle, message)
		}
		args = append(args, fmt.Sprintf("Maestro - %s", title), displayMsg)
		cmd := exec.Command("notify-send", args...)
		if err := cmd.Run(); err != nil {
			d.logError("Failed to send Linux notification: %v", err)
		}
	default:
		d.logError("Desktop notifications not supported on %s", runtime.GOOS)
	}
}

// checkNotificationSupport verifies notification system is available
func (d *Daemon) checkNotificationSupport() error {
	switch runtime.GOOS {
	case "darwin":
		// Check if osascript is available
		cmd := exec.Command("which", "osascript")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("osascript not found (required for macOS notifications)")
		}
		return nil
	case "linux":
		// Check if notify-send is available
		cmd := exec.Command("which", "notify-send")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("notify-send not found (install libnotify-bin or notification-daemon)")
		}
		return nil
	default:
		return fmt.Errorf("notifications not supported on %s (only macOS and Linux)", runtime.GOOS)
	}
}

// isQuietHours checks if current time is in quiet hours
func (d *Daemon) isQuietHours() bool {
	if d.config.QuietHoursStart == "" || d.config.QuietHoursEnd == "" {
		return false
	}

	now := time.Now()
	currentTime := now.Format("15:04")

	// Simple time comparison (doesn't handle wrap-around midnight yet)
	if d.config.QuietHoursStart < d.config.QuietHoursEnd {
		return currentTime >= d.config.QuietHoursStart && currentTime < d.config.QuietHoursEnd
	} else {
		// Wraps around midnight
		return currentTime >= d.config.QuietHoursStart || currentTime < d.config.QuietHoursEnd
	}
}

// Helper functions

func (d *Daemon) getRunningContainers() ([]string, error) {
	cmd := exec.Command("docker", "ps", "--format", "{{.Names}}")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	prefix := d.config.ContainerPrefix
	if prefix == "" {
		prefix = "maestro-" // Default prefix
	}

	var containers []string
	for _, line := range strings.Split(string(output), "\n") {
		name := strings.TrimSpace(line)
		if name != "" && strings.HasPrefix(name, prefix) {
			containers = append(containers, name)
		}
	}
	return containers, nil
}

func (d *Daemon) checkIdleStatus(container string) bool {
	cmd := exec.Command("docker", "exec", container,
		"test", "-f", "/home/node/.maestro/claude-idle")
	return cmd.Run() == nil
}

func (d *Daemon) isClaudeRunning(containerName string) bool {
	cmd := exec.Command("docker", "exec", containerName,
		"sh", "-c", "ps aux | grep -E '[c]laude' | grep -v -E '^\\S+\\s+\\S+\\s+\\S+\\s+\\S+\\s+\\S+\\s+\\S+\\s+\\S+\\s+Z'")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) != ""
}

func (d *Daemon) cleanupStates(activeContainers []string) {
	active := make(map[string]bool)
	for _, c := range activeContainers {
		active[c] = true
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	for name := range d.containerStates {
		if !active[name] {
			delete(d.containerStates, name)
		}
	}
}

// getOrCreateContainerState returns the state for a container, creating it if needed (thread-safe)
func (d *Daemon) getOrCreateContainerState(name string) *ContainerState {
	d.mu.Lock()
	defer d.mu.Unlock()
	state := d.containerStates[name]
	if state == nil {
		state = &ContainerState{
			Name:         name,
			LastActivity: time.Now(),
		}
		d.containerStates[name] = state
	}
	return state
}

func (d *Daemon) cleanup() {
	if d.ipcServer != nil {
		d.ipcServer.Stop()
	}
	// Wait for in-flight background goroutines (with timeout)
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		d.logInfo("All background goroutines finished")
	case <-time.After(15 * time.Second):
		d.logError("Timed out waiting for background goroutines")
	}
	// Remove daemon-ipc.json
	ipcFilePath := filepath.Join(d.configDir, "daemon-ipc.json")
	os.Remove(ipcFilePath)
	// Release file lock
	if d.lockFile != nil {
		releaseFileLock(d.lockFile)
		d.lockFile.Close()
	}
	d.logFile.Close()
}

func (d *Daemon) logInfo(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[INFO] %s\n", msg)
}

func (d *Daemon) logError(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("[ERROR] %s\n", msg)
}

// createChildContainer delegates to the configured CreateContainer callback
func (d *Daemon) createChildContainer(task, parent, branch string) (string, error) {
	if d.config.CreateContainer == nil {
		return "", fmt.Errorf("container creation not configured")
	}
	return d.config.CreateContainer(task, parent, branch)
}

// getUptime returns formatted daemon uptime
func (d *Daemon) getUptime() string {
	return formatDuration(time.Since(d.startTime))
}

// Credentials represents OAuth credentials
type Credentials struct {
	ClaudeAiOauth struct {
		AccessToken      string   `json:"accessToken"`
		RefreshToken     string   `json:"refreshToken"`
		ExpiresAt        int64    `json:"expiresAt"`
		Scopes           []string `json:"scopes"`
		SubscriptionType string   `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

func readCredentials(path string) (*Credentials, error) {
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

func (d *Daemon) getShortName(containerName string) string {
	prefix := d.config.ContainerPrefix
	if prefix == "" {
		prefix = "maestro-"
	}
	if strings.HasPrefix(containerName, prefix) {
		return containerName[len(prefix):]
	}
	return containerName
}

func timeNow() *time.Time {
	t := time.Now()
	return &t
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
	if d < time.Hour {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	return fmt.Sprintf("%.1fh", d.Hours())
}
