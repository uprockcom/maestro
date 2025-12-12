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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/uprockcom/maestro/assets"
	"github.com/uprockcom/maestro/pkg/daemon"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the MCL background daemon",
	Long: `Manage the MCL background daemon for token refresh and notifications.

The daemon monitors running containers and:
- Auto-refreshes expired tokens
- Sends notifications when containers need attention
- Tracks container activity

Commands:
  mcl daemon start   - Start the daemon
  mcl daemon stop    - Stop the daemon
  mcl daemon status  - Show daemon status
  mcl daemon logs    - View daemon logs`,
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the MCL daemon",
	RunE:  runDaemonStart,
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the MCL daemon",
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

func runDaemonStart(cmd *cobra.Command, args []string) error {
	authDir := expandPath(config.Claude.AuthPath)
	pidFile := filepath.Join(authDir, "daemon.pid")

	// Check if already running
	if pid, running := isDaemonRunning(pidFile); running {
		fmt.Printf("Daemon is already running (PID %d)\n", pid)
		return nil
	}

	fmt.Println("Starting MCL daemon...")

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
	daemonCmd := exec.Command(binary, "daemon", "_run")
	daemonCmd.Stdout = nil
	daemonCmd.Stderr = nil
	daemonCmd.Stdin = nil

	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Detach from parent
	if err := daemonCmd.Process.Release(); err != nil {
		return fmt.Errorf("failed to detach daemon: %w", err)
	}

	// Wait a moment and check if it's running
	time.Sleep(1 * time.Second)

	if pid, running := isDaemonRunning(pidFile); running {
		fmt.Printf("✅ Daemon started successfully (PID %d)\n", pid)
		if config.Daemon.Notifications.Enabled {
			fmt.Println("   You should receive a notification confirming it's working")
		}
		fmt.Printf("\nView logs: maestro daemon logs\n")
		return nil
	}

	return fmt.Errorf("daemon failed to start - check logs")
}

func runDaemonStop(cmd *cobra.Command, args []string) error {
	authDir := expandPath(config.Claude.AuthPath)
	pidFile := filepath.Join(authDir, "daemon.pid")

	pid, running := isDaemonRunning(pidFile)
	if !running {
		fmt.Println("Daemon is not running")
		return nil
	}

	fmt.Printf("Stopping daemon (PID %d)...\n", pid)

	// Send SIGTERM
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to stop daemon: %w", err)
	}

	// Wait for process to exit (up to 5 seconds)
	for i := 0; i < 50; i++ {
		if _, running := isDaemonRunning(pidFile); !running {
			fmt.Println("✅ Daemon stopped")
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	return fmt.Errorf("daemon did not stop gracefully")
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	authDir := expandPath(config.Claude.AuthPath)
	pidFile := filepath.Join(authDir, "daemon.pid")

	pid, running := isDaemonRunning(pidFile)

	if running {
		fmt.Printf("Status: Running (PID %d)\n", pid)

		// Show uptime if we can get it
		if uptime := getProcessUptime(pid); uptime != "" {
			fmt.Printf("Uptime: %s\n", uptime)
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

	// Use tail to show last 50 lines
	tailCmd := exec.Command("tail", "-n", "50", logFile)
	tailCmd.Stdout = os.Stdout
	tailCmd.Stderr = os.Stderr

	return tailCmd.Run()
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
		return fmt.Errorf("failed to create mcl directory: %w", err)
	}

	// Parse config
	daemonConfig := daemon.Config{
		CheckInterval:      parseDuration(config.Daemon.CheckInterval, 30*time.Minute),
		TokenThreshold:     parseDuration(config.Daemon.TokenRefresh.Threshold, 6*time.Hour),
		NotificationsOn:    config.Daemon.Notifications.Enabled,
		AttentionThreshold: parseDuration(config.Daemon.Notifications.AttentionThreshold, 5*time.Minute),
		NotifyOn:           config.Daemon.Notifications.NotifyOn,
		QuietHoursStart:    config.Daemon.Notifications.QuietHours.Start,
		QuietHoursEnd:      config.Daemon.Notifications.QuietHours.End,
		ContainerPrefix:    config.Containers.Prefix,
	}

	// Create and start daemon with embedded icon
	d, err := daemon.New(daemonConfig, authDir, assets.NotificationIcon)
	if err != nil {
		return fmt.Errorf("failed to create daemon: %w", err)
	}

	return d.Start()
}

// EnsureDaemonRunning starts the daemon if it's not already running.
// This is called automatically when the TUI starts.
func EnsureDaemonRunning() {
	authDir := expandPath(config.Claude.AuthPath)
	pidFile := filepath.Join(authDir, "daemon.pid")

	// Check if already running
	if _, running := isDaemonRunning(pidFile); running {
		return // Already running, nothing to do
	}

	// Start daemon silently in background
	binary, err := os.Executable()
	if err != nil {
		return // Fail silently
	}

	daemonCmd := exec.Command(binary, "daemon", "_run")
	daemonCmd.Stdout = nil
	daemonCmd.Stderr = nil
	daemonCmd.Stdin = nil

	if err := daemonCmd.Start(); err != nil {
		return // Fail silently
	}

	// Detach from parent
	daemonCmd.Process.Release()
}

// Helper functions

func isDaemonRunning(pidFile string) (int, bool) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}

	// Check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return 0, false
	}

	// Send signal 0 to check if process is alive
	err = process.Signal(syscall.Signal(0))
	return pid, err == nil
}

func getProcessUptime(pid int) string {
	// Use ps to get process start time
	cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "etime=")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

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
