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
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"
	"github.com/uprockcom/maestro/assets"
	"github.com/uprockcom/maestro/pkg/container"
	"github.com/uprockcom/maestro/pkg/daemon"
	"github.com/uprockcom/maestro/pkg/notify"
	"github.com/uprockcom/maestro/pkg/notify/signal"
)

// daemonIPCFilePath returns the path to daemon-ipc.json using the configured auth path.
func daemonIPCFilePath() string {
	return filepath.Join(expandPath(config.Claude.AuthPath), "daemon-ipc.json")
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the Maestro background daemon",
	Long: `Manage the Maestro background daemon for token refresh and notifications.

The daemon monitors running containers and:
- Auto-refreshes expired tokens
- Sends notifications when containers need attention
- Tracks container activity

Commands:
  maestro daemon start   - Start the daemon
  maestro daemon stop    - Stop the daemon
  maestro daemon status  - Show daemon status
  maestro daemon logs    - View daemon logs`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the Maestro daemon",
	RunE:  runDaemonStart,
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the Maestro daemon",
	RunE:  runDaemonStop,
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE:  runDaemonStatus,
}

var daemonLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "View daemon logs",
	RunE:  runDaemonLogs,
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonLogsCmd)
}

// readDaemonIPCInfo reads daemon-ipc.json and returns the parsed info, or nil if not found.
func readDaemonIPCInfo() *daemon.DaemonIPCInfo {
	data, err := os.ReadFile(daemonIPCFilePath())
	if err != nil {
		return nil
	}

	var info daemon.DaemonIPCInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil
	}

	if info.Port == 0 {
		return nil
	}

	return &info
}

// isDaemonRunning checks if the daemon is running by reading daemon-ipc.json
// and making an HTTP GET /status call. Returns running status and info.
func isDaemonRunning() (bool, *daemon.DaemonIPCInfo) {
	info := readDaemonIPCInfo()
	if info == nil {
		return false, nil
	}

	// Try HTTP health check
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/status", info.Port))
	if err != nil {
		// Connection refused or timeout — daemon is not running, clean up stale file
		os.Remove(daemonIPCFilePath())
		return false, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return true, info
	}

	return false, nil
}

func runDaemonStart(cmd *cobra.Command, args []string) error {
	// Check if already running
	if running, info := isDaemonRunning(); running {
		fmt.Printf("Daemon is already running (PID %d, port %d)\n", info.PID, info.Port)
		return nil
	}

	fmt.Println("Starting Maestro daemon...")

	// Check notification support if notifications are enabled
	if config.Daemon.Notifications.Enabled {
		if err := checkNotificationSupport(); err != nil {
			fmt.Printf("\n⚠️  Warning: %v\n", err)
			fmt.Println("   Daemon will run but notifications will be disabled.")
			fmt.Println("   Consider setting notifications.enabled: false in config")
		} else if runtime.GOOS == "darwin" {
			// On macOS, check for terminal-notifier for better notifications
			cmd := exec.Command("which", "terminal-notifier")
			if err := cmd.Run(); err != nil {
				fmt.Println("\n💡 Tip: Install terminal-notifier for custom icon support:")
				fmt.Println("   brew install terminal-notifier")
				fmt.Println("   (Notifications will still work with osascript)")
			}
		}
	}

	// Fork and run daemon in background
	binary, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start daemon as background process
	daemonProc := exec.Command(binary, "daemon", "_run")
	daemonProc.Stdout = nil
	daemonProc.Stderr = nil
	daemonProc.Stdin = nil

	// Set platform-specific process attributes for daemonization
	setDaemonProcessAttr(daemonProc)

	if err := daemonProc.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Poll for daemon to be ready (up to 3 seconds)
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		if running, info := isDaemonRunning(); running {
			fmt.Printf("✅ Daemon started successfully (PID %d, port %d)\n", info.PID, info.Port)
			if config.Daemon.Notifications.Enabled {
				fmt.Println("   You should receive a notification confirming it's working")
			}
			fmt.Printf("\nView logs: maestro daemon logs\n")
			return nil
		}
	}

	return fmt.Errorf("daemon failed to start - check logs")
}

func runDaemonStop(cmd *cobra.Command, args []string) error {
	info := readDaemonIPCInfo()
	if info == nil {
		fmt.Println("Daemon is not running")
		return nil
	}

	// Verify it's actually running
	running, _ := isDaemonRunning()
	if !running {
		fmt.Println("Daemon is not running")
		return nil
	}

	fmt.Printf("Stopping daemon (PID %d)...\n", info.PID)

	// Send POST /shutdown with auth token
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("POST", fmt.Sprintf("http://127.0.0.1:%d/shutdown", info.Port), nil)
	if err != nil {
		return fmt.Errorf("failed to create shutdown request: %w", err)
	}
	req.Header.Set("X-Maestro-Token", info.Token)

	resp, err := client.Do(req)
	if err != nil {
		// If we can't connect, daemon may have already stopped
		fmt.Println("✅ Daemon stopped")
		os.Remove(daemonIPCFilePath())
		return nil
	}
	resp.Body.Close()

	// Poll until daemon is no longer responding (up to 5 seconds)
	for i := 0; i < 50; i++ {
		time.Sleep(100 * time.Millisecond)
		if running, _ := isDaemonRunning(); !running {
			fmt.Println("✅ Daemon stopped")
			return nil
		}
	}

	// Fallback: if HTTP shutdown didn't work and we have PID, kill the process
	if info.PID > 0 {
		// Re-check if daemon is still running to avoid killing an unrelated process
		// that may have reused the PID
		if stillRunning, _ := isDaemonRunning(); stillRunning {
			process, err := os.FindProcess(info.PID)
			if err == nil {
				process.Kill()
			}
		}
		os.Remove(daemonIPCFilePath())
		fmt.Println("✅ Daemon stopped (forced)")
		return nil
	}

	return fmt.Errorf("daemon did not stop gracefully")
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	running, info := isDaemonRunning()

	if running {
		// Get detailed status from HTTP endpoint
		client := &http.Client{Timeout: 2 * time.Second}
		resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/status", info.Port))
		if err == nil {
			defer resp.Body.Close()
			var status daemon.IPCStatusResponse
			if err := json.NewDecoder(resp.Body).Decode(&status); err == nil {
				fmt.Printf("Status: Running (PID %d, port %d)\n", status.PID, info.Port)
				fmt.Printf("Uptime: %s\n", status.Uptime)
				fmt.Printf("Containers: %d\n", len(status.Containers))
				for _, c := range status.Containers {
					fmt.Printf("  - %s\n", c)
				}
			} else {
				fmt.Printf("Status: Running (PID %d, port %d)\n", info.PID, info.Port)
			}
		} else {
			fmt.Printf("Status: Running (PID %d, port %d)\n", info.PID, info.Port)
		}

		// Show config
		fmt.Printf("\nConfiguration:\n")
		fmt.Printf("  Check interval: %s\n", config.Daemon.CheckInterval)
		fmt.Printf("  Token threshold: %s\n", config.Daemon.TokenRefresh.Threshold)
		fmt.Printf("  Notifications: %v\n", config.Daemon.Notifications.Enabled)
		if config.Daemon.Notifications.Enabled {
			fmt.Printf("  Attention threshold: %s\n", config.Daemon.Notifications.AttentionThreshold)
		}
	} else {
		fmt.Println("Status: Not running")
	}

	return nil
}

func runDaemonLogs(cmd *cobra.Command, args []string) error {
	authDir := expandPath(config.Claude.AuthPath)
	logFile := filepath.Join(authDir, "daemon.log")

	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		fmt.Println("No daemon logs found")
		return nil
	}

	// Read last 50 lines using Go file I/O
	f, err := os.Open(logFile)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer f.Close()

	// Read all lines and keep the last 50
	var lines []string
	scanner := bufio.NewScanner(f)
	// Increase buffer size for potentially long log lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}

	start := 0
	if len(lines) > 50 {
		start = len(lines) - 50
	}
	for _, line := range lines[start:] {
		fmt.Println(line)
	}

	return nil
}

// Hidden command that actually runs the daemon
var daemonRunCmd = &cobra.Command{
	Use:    "_run",
	Hidden: true,
	RunE:   runDaemonBackground,
}

func init() {
	daemonCmd.AddCommand(daemonRunCmd)
}

func runDaemonBackground(cmd *cobra.Command, args []string) error {
	authDir := expandPath(config.Claude.AuthPath)

	// Ensure directory exists
	if err := os.MkdirAll(authDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Parse config
	daemonConfig := daemon.Config{
		CheckInterval:      parseDuration(config.Daemon.CheckInterval, 30*time.Minute),
		TokenThreshold:     parseDuration(config.Daemon.TokenRefresh.Threshold, 30*time.Minute),
		NotificationsOn:    config.Daemon.Notifications.Enabled,
		AttentionThreshold: parseDuration(config.Daemon.Notifications.AttentionThreshold, 5*time.Minute),
		NotifyOn:           config.Daemon.Notifications.NotifyOn,
		QuietHoursStart:    config.Daemon.Notifications.QuietHours.Start,
		QuietHoursEnd:      config.Daemon.Notifications.QuietHours.End,
		ContainerPrefix:    config.Containers.Prefix,
		CreateContainer:    CreateContainerFromDaemon,
	}

	// Create and start daemon with embedded icon
	d, err := daemon.New(daemonConfig, authDir, assets.NotificationIcon)
	if err != nil {
		return fmt.Errorf("failed to create daemon: %w", err)
	}

	// Build notification providers
	var providers []notify.Provider

	if config.Daemon.Notifications.Providers.Desktop.Enabled {
		desktopProvider := notify.NewDesktopProvider(d.IconPath(), d.HasTerminalNotifier())
		providers = append(providers, desktopProvider)
	}

	var localProvider *notify.LocalProvider
	if config.Daemon.Notifications.Providers.Local.Enabled {
		localProvider = notify.NewLocalProvider()
		providers = append(providers, localProvider)
	}

	if config.Daemon.Notifications.Providers.Signal.Enabled {
		port := config.Daemon.Notifications.Providers.Signal.ContainerPort
		if port == 0 {
			port = 8080
		}
		signalProvider := signal.New(signal.Config{
			Number:    config.Daemon.Notifications.Providers.Signal.Number,
			Recipient: config.Daemon.Notifications.Providers.Signal.Recipient,
			Port:      port,
			URL:       config.Daemon.Notifications.Providers.Signal.URL,
			APIKey:    config.Daemon.Notifications.Providers.Signal.APIKey,
		}, d, d.LogInfo)
		providers = append(providers, signalProvider)
	}

	// Response callback: handle approval responses or write answer to container's
	// question-response.txt file. The maestro-agent ask hook picks it up and
	// feeds it to Claude via stderr.
	callback := func(resp notify.Response) error {
		// Check if this is a resource request approval
		if approval := d.PopApproval(resp.EventID); approval != nil {
			approved, err := d.ExecuteApproval(approval, resp)
			if ipcSrv := d.IPCServer(); ipcSrv != nil {
				if approved {
					ipcSrv.UpdateRequestFile(approval.ContainerName, approval.RequestID, daemon.IPCRequestStatusFulfilled, "", "")
				} else {
					ipcSrv.UpdateRequestFile(approval.ContainerName, approval.RequestID, daemon.IPCRequestStatusFailed, "", "Request denied by user")
				}
			}
			return err
		}

		// Original behavior: write question response
		if resp.ContainerName == "" {
			return fmt.Errorf("no container name in response")
		}
		return container.WriteQuestionResponse(resp.ContainerName, resp.Selections, resp.Text)
	}

	providerNotifyOn := make(map[string][]string)
	if len(config.Daemon.Notifications.Providers.Desktop.NotifyOn) > 0 {
		providerNotifyOn["desktop"] = config.Daemon.Notifications.Providers.Desktop.NotifyOn
	}
	if len(config.Daemon.Notifications.Providers.Local.NotifyOn) > 0 {
		providerNotifyOn["local"] = config.Daemon.Notifications.Providers.Local.NotifyOn
	}
	if len(config.Daemon.Notifications.Providers.Slack.NotifyOn) > 0 {
		providerNotifyOn["slack"] = config.Daemon.Notifications.Providers.Slack.NotifyOn
	}
	if len(config.Daemon.Notifications.Providers.Signal.NotifyOn) > 0 {
		providerNotifyOn["signal"] = config.Daemon.Notifications.Providers.Signal.NotifyOn
	}

	engine := notify.NewEngine(providers, callback, d.LogInfo, providerNotifyOn)
	d.SetEngine(engine, localProvider)

	// Start Signal provider's background goroutine (container + poll loop)
	for _, p := range providers {
		if sp, ok := p.(*signal.SignalProvider); ok {
			d.StartBackgroundTask(func(stopChan <-chan bool) {
				ctx, cancel := context.WithCancel(context.Background())
				go func() { <-stopChan; cancel() }()
				if err := sp.Run(ctx); err != nil {
					d.LogInfo("Signal provider stopped: %v", err)
				}
			})
		}
	}

	return d.Start()
}

// EnsureDaemonRunning starts the daemon if it's not already running.
// This is called automatically when the TUI starts.
func EnsureDaemonRunning() {
	// Check if already running via HTTP
	if running, _ := isDaemonRunning(); running {
		return // Already running, nothing to do
	}

	// Start daemon silently in background
	binary, err := os.Executable()
	if err != nil {
		return // Fail silently
	}

	daemonProc := exec.Command(binary, "daemon", "_run")
	daemonProc.Stdout = nil
	daemonProc.Stderr = nil
	daemonProc.Stdin = nil

	// Set platform-specific process attributes for daemonization
	setDaemonProcessAttr(daemonProc)

	if err := daemonProc.Start(); err != nil {
		return // Fail silently
	}
}

// Helper functions

func parseDuration(s string, defaultDur time.Duration) time.Duration {
	if s == "" {
		return defaultDur
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return defaultDur
	}
	return d
}

// checkNotificationSupport verifies notification system is available
func checkNotificationSupport() error {
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
			return fmt.Errorf("notify-send not found - install with: sudo apt install libnotify-bin")
		}
		return nil
	default:
		return fmt.Errorf("notifications not supported on %s (only macOS and Linux)", runtime.GOOS)
	}
}
