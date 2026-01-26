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
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
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
}

// Daemon manages background monitoring and auto-refresh
type Daemon struct {
	config            Config
	logFile           *os.File
	pidFile           string
	stopChan          chan bool
	containerStates   map[string]*ContainerState
	iconPath          string // Cached icon path for notifications
	hasTerminalNotifier bool   // Whether terminal-notifier is available
}

// ContainerState tracks container monitoring state
type ContainerState struct {
	Name                string
	AttentionStarted    *time.Time
	LastNotified        *time.Time
	LastActivity        time.Time
	LastTokenCheck      time.Time
	NotificationSent    bool
}

// New creates a new daemon instance
func New(config Config, mclDir string, iconData []byte) (*Daemon, error) {
	logPath := filepath.Join(mclDir, "daemon.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	d := &Daemon{
		config:          config,
		logFile:         logFile,
		pidFile:         filepath.Join(mclDir, "daemon.pid"),
		stopChan:        make(chan bool),
		containerStates: make(map[string]*ContainerState),
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
			iconPath := filepath.Join(mclDir, "notification-icon.png")
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

// Start begins the daemon monitoring loop
func (d *Daemon) Start() error {
	// Write PID file
	if err := d.writePID(); err != nil {
		return err
	}

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
			d.notify("Daemon Started", "Maestro daemon is now monitoring your containers")
			d.logInfo("Notifications enabled and working")
		}
	}

	// Main monitoring loop
	ticker := time.NewTicker(d.config.CheckInterval)
	defer ticker.Stop()

	// Run initial check immediately
	d.check()

	for {
		select {
		case <-ticker.C:
			d.check()
		case <-d.stopChan:
			d.logInfo("Daemon stopping")
			d.cleanup()
			return nil
		}
	}
}

// Stop signals the daemon to stop
func (d *Daemon) Stop() {
	close(d.stopChan)
}

// check performs one monitoring cycle
func (d *Daemon) check() {
	containers, err := d.getRunningContainers()
	if err != nil {
		d.logError("Failed to get containers: %v", err)
		return
	}

	for _, container := range containers {
		// Initialize state if new container
		if _, exists := d.containerStates[container]; !exists {
			d.containerStates[container] = &ContainerState{
				Name:         container,
				LastActivity: time.Now(),
			}
		}

		state := d.containerStates[container]

		// Check token expiry
		d.checkTokenExpiry(container, state)

		// Check attention status
		d.checkAttentionStatus(container, state)
	}

	// Cleanup states for removed containers
	d.cleanupStates(containers)
}

// checkTokenExpiry checks and refreshes tokens if needed
func (d *Daemon) checkTokenExpiry(container string, state *ContainerState) {
	// Don't check too frequently (every 5 minutes is enough)
	if time.Since(state.LastTokenCheck) < 5*time.Minute {
		return
	}
	state.LastTokenCheck = time.Now()

	// Extract credentials
	tmpFile := fmt.Sprintf("/tmp/maestro-creds-%s-%d.json", container, time.Now().Unix())
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
				d.notify("Token Expiring", fmt.Sprintf("Container %s token expires in %.1fh and auto-refresh failed",
					d.getShortName(container), timeLeft.Hours()))
				state.LastNotified = timeNow()
			}
		} else {
			d.logInfo("Successfully refreshed token for %s", container)
		}
	}
}

// checkAttentionStatus monitors container attention state
func (d *Daemon) checkAttentionStatus(container string, state *ContainerState) {
	needsAttention := d.checkBellStatus(container)

	if needsAttention {
		// Track when attention started
		if state.AttentionStarted == nil {
			now := time.Now()
			state.AttentionStarted = &now
			state.NotificationSent = false
			d.logInfo("Container %s needs attention", d.getShortName(container))
		}

		// Check if we should notify
		attentionDuration := time.Since(*state.AttentionStarted)
		if !state.NotificationSent && attentionDuration >= d.config.AttentionThreshold {
			if d.shouldNotify("attention_needed", state) {
				d.notify("Container Needs Attention",
					fmt.Sprintf("Container %s has needed attention for %s",
						d.getShortName(container), formatDuration(attentionDuration)))
				state.NotificationSent = true
				state.LastNotified = timeNow()
			}
		}
	} else {
		// Clear attention state
		if state.AttentionStarted != nil {
			d.logInfo("Container %s attention resolved", d.getShortName(container))
		}
		state.AttentionStarted = nil
		state.NotificationSent = false
	}
}

// refreshToken refreshes the token for a container
func (d *Daemon) refreshToken(container string) error {
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
	if state.LastNotified != nil && time.Since(*state.LastNotified) < 30*time.Minute {
		return false
	}

	return true
}

// notify sends a desktop notification
func (d *Daemon) notify(title, message string) {
	switch runtime.GOOS {
	case "darwin":
		// Try terminal-notifier first (better icon support)
		if d.hasTerminalNotifier {
			args := []string{
				"-message", message,
				"-title", fmt.Sprintf("MCL - %s", title),
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
		script := fmt.Sprintf(`display notification "%s" with title "MCL - %s"`, message, title)
		cmd := exec.Command("osascript", "-e", script)
		if err := cmd.Run(); err != nil {
			d.logError("Failed to send macOS notification: %v", err)
		}
	case "linux":
		// Linux notification via notify-send
		// Note: --icon must come before title and message
		var args []string
		if d.iconPath != "" {
			args = append(args, "--icon", d.iconPath)
		}
		args = append(args, fmt.Sprintf("Maestro - %s", title), message)
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

func (d *Daemon) checkBellStatus(container string) bool {
	cmd := exec.Command("docker", "exec", container,
		"tmux", "list-windows", "-t", "main", "-F", "#{window_bell_flag}:#{window_silence_flag}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}

	for _, line := range strings.Split(string(output), "\n") {
		parts := strings.Split(strings.TrimSpace(line), ":")
		if len(parts) == 2 {
			if parts[0] == "1" || parts[1] == "1" {
				return true
			}
		}
	}
	return false
}

func (d *Daemon) cleanupStates(activeContainers []string) {
	active := make(map[string]bool)
	for _, c := range activeContainers {
		active[c] = true
	}

	for name := range d.containerStates {
		if !active[name] {
			delete(d.containerStates, name)
		}
	}
}

func (d *Daemon) writePID() error {
	pid := os.Getpid()
	return os.WriteFile(d.pidFile, []byte(strconv.Itoa(pid)), 0644)
}

func (d *Daemon) cleanup() {
	os.Remove(d.pidFile)
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
