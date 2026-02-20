// Copyright 2026 Christopher O'Connell
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
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func serviceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "service",
		Short: "Run persistent background service — manages idle wake-up, clear timer, heartbeat",
		RunE:  runService,
	}
}

func runService(cmd *cobra.Command, args []string) error {
	EnsureStateDirs()
	InitLog()

	LogInfo("maestro-agent service starting")

	// Write PID file
	if err := os.WriteFile(agentPIDFile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
		LogError("Failed to write PID file", "error", err.Error())
	}
	defer os.Remove(agentPIDFile)

	// Load manifest
	manifest, err := LoadManifest()
	if err != nil {
		LogInfo("No manifest found, running in minimal mode", "error", err.Error())
		manifest = &Manifest{Type: "interactive"}
	}

	// Handle signals for clean shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	// Start the main service loop
	clearTimer := time.NewTimer(time.Duration(1<<63 - 1)) // effectively infinite
	clearTimer.Stop()

	var heartbeatTimer *time.Timer
	if manifest.Heartbeat.Interval > 0 {
		heartbeatTimer = time.NewTimer(time.Duration(manifest.Heartbeat.Interval) * time.Second)
	}

	LogInfo("Service loop started",
		"state", string(ReadState()))

	clearTimerRunning := false

	for {
		state := ReadState()

		select {
		case <-sigCh:
			LogInfo("Service shutting down (signal)")
			return nil

		default:
		}

		switch state {
		case StateIdle:
			// Check if we need to wake Claude (message arrived while idle)
			if HasQueuedMessages() {
				LogInfo("Messages found while idle, waking Claude")
				wakeClaudeFromIdle()
			}

			// Start clear timer once per idle transition
			if manifest.OnIdle.ClearAfter > 0 && !clearTimerRunning {
				clearTimer.Reset(time.Duration(manifest.OnIdle.ClearAfter) * time.Second)
				clearTimerRunning = true
				LogInfo("Clear timer started",
					"state", "idle")
			}

		case StateActive, StateWaiting:
			// Stop clear timer if it was running
			clearTimer.Stop()
			clearTimerRunning = false

		case StateClearing:
			// Perform kill-restart
			LogInfo("Performing kill-restart clear")
			if err := performClear(manifest); err != nil {
				LogError("Kill-restart failed", "error", err.Error())
				WriteState(StateIdle) // fallback
			}
		}

		// Check heartbeat timer
		if heartbeatTimer != nil {
			select {
			case <-heartbeatTimer.C:
				currentState := ReadState()
				if currentState == StateWaiting ||
					(currentState == StateIdle && !manifest.Heartbeat.SuppressWhileActive) {
					deliverHeartbeat(manifest)
				}
				heartbeatTimer.Reset(time.Duration(manifest.Heartbeat.Interval) * time.Second)
			default:
			}
		}

		// Check clear timer
		select {
		case <-clearTimer.C:
			clearTimerRunning = false
			currentState := ReadState()
			if currentState == StateIdle && !isUserConnected() {
				LogInfo("Clear timer expired, transitioning to clearing")
				WriteState(StateClearing)
			} else {
				LogInfo("Clear timer expired but conditions not met, skipping",
					"state", string(currentState))
			}
		default:
		}

		// Poll interval for the service loop
		time.Sleep(2 * time.Second)
	}
}

// wakeClaudeFromIdle sends "continue" + Enter to Claude via tmux
func wakeClaudeFromIdle() {
	LogInfo("Sending 'continue' to wake Claude from idle")

	// Write "continue" to tmux buffer and paste + enter
	writeCmd := exec.Command("bash", "-c",
		`echo "continue" | tmux load-buffer - && tmux paste-buffer -t main:0 -d && tmux send-keys -t main:0 C-m`)
	if err := writeCmd.Run(); err != nil {
		LogError("Failed to wake Claude", "error", err.Error())
		return
	}

	WriteState(StateActive)
	os.Remove(claudeIdleFile)
}

// performClear does a kill-restart of Claude
func performClear(manifest *Manifest) error {
	// Check user connection one more time before killing
	if isUserConnected() {
		LogInfo("User connected, aborting clear")
		WriteState(StateIdle)
		return nil
	}

	// Run pre-clear script if configured
	if manifest.OnIdle.PreClear != "" {
		fullPath := resolvePath(manifest.OnIdle.PreClear)
		if fullPath != "" {
			LogInfo("Running pre-clear script", "trigger", manifest.OnIdle.PreClear)
			cmd := exec.Command("bash", fullPath)
			cmd.Dir = "/workspace"
			if output, err := cmd.CombinedOutput(); err != nil {
				LogError("Pre-clear script failed",
					"error", err.Error(),
					"trigger", string(output))
			}
		}
	}

	WriteState(StateClearing)

	// Remove session-ready flag
	os.Remove(sessionReadyFile)

	// Kill Claude process — try PID file first, fall back to pgrep
	killed := false
	pidData, err := os.ReadFile(claudePIDFile)
	if err == nil {
		pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
		if err == nil {
			LogInfo("Killing Claude process via PID file", "trigger", fmt.Sprintf("pid=%d", pid))
			syscall.Kill(pid, syscall.SIGTERM)
			time.Sleep(2 * time.Second)
			syscall.Kill(pid, syscall.SIGKILL)
			killed = true
		}
	}
	if !killed {
		// No PID file — find Claude by process search
		LogInfo("No PID file, finding Claude via pgrep")
		pgrepCmd := exec.Command("pgrep", "-f", "^claude")
		if output, err := pgrepCmd.Output(); err == nil {
			for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
				if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
					LogInfo("Killing Claude process via pgrep", "trigger", fmt.Sprintf("pid=%d", pid))
					syscall.Kill(pid, syscall.SIGTERM)
				}
			}
			time.Sleep(2 * time.Second)
			// Force kill any remaining
			if output, err := exec.Command("pgrep", "-f", "^claude").Output(); err == nil {
				for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
					if pid, err := strconv.Atoi(strings.TrimSpace(line)); err == nil {
						syscall.Kill(pid, syscall.SIGKILL)
					}
				}
			}
		}
	}

	// Wait for process to fully exit
	time.Sleep(1 * time.Second)

	// Rebuild tmux window with new Claude — kill the pane and respawn
	WriteState(StateStarting)

	// Assemble bootstrap prompt
	prompt := BuildBootstrapPrompt(manifest)
	if prompt == "" {
		prompt = "You are an AI assistant. Ready for input."
	}

	// Write bootstrap prompt to temp file
	tmpFile := "/tmp/maestro-bootstrap.txt"
	if err := os.WriteFile(tmpFile, []byte(prompt), 0644); err != nil {
		return fmt.Errorf("failed to write bootstrap prompt: %w", err)
	}

	// Restart Claude in tmux — try respawn-pane first, fall back to new session
	bootstrapShell := fmt.Sprintf("cat %s | claude --dangerously-skip-permissions", tmpFile)
	respawnCmd := exec.Command("tmux", "respawn-pane", "-k", "-t", "main:0", bootstrapShell)
	if err := respawnCmd.Run(); err != nil {
		// Tmux server may have died — create a fresh session
		LogInfo("Tmux respawn failed, creating new session", "error", err.Error())
		newSessionCmd := exec.Command("tmux", "new-session", "-d", "-s", "main", bootstrapShell)
		if err := newSessionCmd.Run(); err != nil {
			return fmt.Errorf("failed to start Claude in tmux: %w", err)
		}
	}

	LogInfo("Claude restarted, waiting for readiness")

	// Wait for session-ready flag (SessionStart hook touches it)
	for i := 0; i < 60; i++ { // max 60 seconds
		if _, err := os.Stat(sessionReadyFile); err == nil {
			LogInfo("Claude ready after restart")
			WriteState(StateActive)
			return nil
		}
		time.Sleep(1 * time.Second)
	}

	LogError("Timeout waiting for Claude readiness after restart")
	WriteState(StateActive) // proceed anyway
	return nil
}

// deliverHeartbeat generates and queues a heartbeat message
func deliverHeartbeat(manifest *Manifest) {
	LogInfo("Generating heartbeat")

	var message string
	if manifest.Heartbeat.Script != "" {
		fullPath := resolvePath(manifest.Heartbeat.Script)
		if fullPath != "" {
			cmd := exec.Command("bash", fullPath)
			cmd.Dir = "/workspace"
			output, err := cmd.Output()
			if err != nil {
				LogError("Heartbeat script failed", "error", err.Error())
				message = "Periodic heartbeat check-in."
			} else {
				message = strings.TrimSpace(string(output))
			}
		}
	} else {
		message = "Periodic heartbeat check-in."
	}

	// Write heartbeat as a message file in the queue
	ts := fmt.Sprintf("%d", time.Now().UnixNano())
	filename := fmt.Sprintf("%s/%s.txt", pendingMsgDir, ts)
	content := FormatTrigger("heartbeat", message)
	os.WriteFile(filename, []byte(content), 0644)

	LogInfo("Heartbeat queued")
}
