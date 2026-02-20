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
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func hookStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Handle Claude's Stop hook — blocking wait with concurrent watchers",
		RunE:  runHookStop,
	}
}

// watcherResult is returned by the first watcher to fire
type watcherResult struct {
	Source  string // "queue", "connected", or script name
	Content string // message/trigger content to deliver
	Exit0   bool   // true = exit 0 (allow stop), false = exit 2 (continue)
}

func runHookStop(cmd *cobra.Command, args []string) error {
	suppressStderr = true
	EnsureStateDirs()
	InitLog()

	// Re-entrancy guard: check if we're the main Claude process
	if !isMainClaude() {
		LogDebug("Stop hook: child process, exiting silently", "hook", "stop")
		return nil // exit 0
	}

	LogInfo("Stop hook fired", "hook", "stop")

	// Load manifest for watcher configuration
	manifest, err := LoadManifest()
	if err != nil {
		// No manifest — fall back to simple queue drain (backward compat)
		LogInfo("No manifest, falling back to simple queue drain", "hook", "stop")
		return fallbackStopHook()
	}

	// Check queue immediately first — if messages are already waiting, deliver instantly
	messages, _ := DrainQueue()
	if len(messages) > 0 {
		LogInfo("Stop hook: immediate messages found", "hook", "stop",
			"trigger", fmt.Sprintf("queue(%d)", len(messages)))
		WriteState(StateActive)
		fmt.Fprint(os.Stderr, FormatMessages(messages))
		os.Exit(2)
	}

	// No immediate messages — enter blocking wait
	WriteState(StateWaiting)
	LogInfo("Stop hook: entering blocking wait", "hook", "stop", "state", "waiting")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	resultCh := make(chan watcherResult, 1)
	var wg sync.WaitGroup

	// Launch configured watchers
	for _, w := range manifest.OnStop.Watchers {
		switch {
		case w.Builtin == "queue":
			wg.Add(1)
			go func() {
				defer wg.Done()
				watchQueue(ctx, resultCh)
			}()

		case w.Builtin == "connected":
			wg.Add(1)
			go func() {
				defer wg.Done()
				watchConnected(ctx, resultCh)
			}()

		case w.Script != "":
			wg.Add(1)
			script := w.Script
			go func() {
				defer wg.Done()
				watchScript(ctx, script, resultCh)
			}()
		}
	}

	// If no watchers configured, use defaults (queue + connected)
	if len(manifest.OnStop.Watchers) == 0 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			watchQueue(ctx, resultCh)
		}()
		go func() {
			defer wg.Done()
			watchConnected(ctx, resultCh)
		}()
	}

	// Wait for first result
	result := <-resultCh
	cancel() // Cancel all other watchers

	// Give goroutines a moment to clean up
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		LogDebug("Watcher cleanup timed out", "hook", "stop")
	}

	LogInfo("Stop hook: watcher fired",
		"hook", "stop",
		"trigger", result.Source)

	if result.Exit0 {
		// User connected — allow Claude to stop, go idle
		WriteState(StateIdle)
		os.Exit(0)
	}

	// Message or trigger — deliver and continue
	WriteState(StateActive)
	fmt.Fprint(os.Stderr, result.Content)
	os.Exit(2)
	return nil // unreachable
}

// watchQueue watches pending-messages/ for new files using polling
// (inotifywait may not be available; polling every 1s is fine)
func watchQueue(ctx context.Context, ch chan<- watcherResult) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		messages, _ := DrainQueue()
		if len(messages) > 0 {
			select {
			case ch <- watcherResult{
				Source:  fmt.Sprintf("queue(%d)", len(messages)),
				Content: FormatMessages(messages),
				Exit0:   false,
			}:
			case <-ctx.Done():
			}
			return
		}

		// Poll interval
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Second):
		}
	}
}

// watchConnected watches for tmux client connections
func watchConnected(ctx context.Context, ch chan<- watcherResult) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if isUserConnected() {
			select {
			case ch <- watcherResult{
				Source: "connected",
				Exit0:  true,
			}:
			case <-ctx.Done():
			}
			return
		}

		// Poll every 2 seconds
		select {
		case <-ctx.Done():
			return
		case <-time.After(2 * time.Second):
		}
	}
}

// watchScript runs a custom watcher script and waits for it to exit
func watchScript(ctx context.Context, scriptPath string, ch chan<- watcherResult) {
	// Resolve script path relative to workspace
	fullPath := scriptPath
	if !filepath.IsAbs(scriptPath) {
		// Try relative to workspace first
		if _, err := os.Stat(filepath.Join("/workspace", scriptPath)); err == nil {
			fullPath = filepath.Join("/workspace", scriptPath)
		}
		// Then try relative to project dirs
		entries, _ := os.ReadDir("/workspace")
		for _, entry := range entries {
			if entry.IsDir() {
				candidate := filepath.Join("/workspace", entry.Name(), scriptPath)
				if _, err := os.Stat(candidate); err == nil {
					fullPath = candidate
					break
				}
			}
		}
	}

	cmd := exec.CommandContext(ctx, "bash", fullPath)
	// Set process group so we can kill all child processes
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var output strings.Builder
	var scriptErr strings.Builder
	cmd.Stdout = &output
	cmd.Stderr = &scriptErr // capture script errors (don't pollute hook stderr)

	err := cmd.Run()

	// Check if we were cancelled
	select {
	case <-ctx.Done():
		// Kill the process group if still running
		if cmd.Process != nil {
			syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		}
		return
	default:
	}

	if errOut := scriptErr.String(); errOut != "" {
		LogError("Custom watcher script stderr",
			"hook", "stop",
			"trigger", scriptPath,
			"error", strings.TrimSpace(errOut))
	}

	if err != nil {
		LogError("Custom watcher script failed",
			"hook", "stop",
			"trigger", scriptPath,
			"error", err.Error())
		return
	}

	scriptName := filepath.Base(scriptPath)
	content := FormatTrigger(scriptName, output.String())

	// Also check if messages arrived while the script was running
	messages, _ := DrainQueue()
	if len(messages) > 0 {
		content += "\n" + FormatMessages(messages)
	}

	select {
	case ch <- watcherResult{
		Source:  scriptName,
		Content: content,
		Exit0:   false,
	}:
	case <-ctx.Done():
	}
}

// isUserConnected checks if a tmux client is attached to the main session
func isUserConnected() bool {
	cmd := exec.Command("tmux", "list-clients", "-t", "main")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}

// isMainClaude checks if the calling Claude process is the main one (not a child like haiku)
func isMainClaude() bool {
	pidData, err := os.ReadFile(claudePIDFile)
	if err != nil {
		// No PID file — assume main (first run)
		return true
	}

	mainPID := strings.TrimSpace(string(pidData))
	if mainPID == "" {
		return true
	}

	// Check if our parent process chain includes the main Claude PID
	// The hook is called by Claude, so PPID chain should lead to the main Claude
	// For simplicity: if the PID file exists and we can read PPID, check if it matches
	ppid := fmt.Sprintf("%d", os.Getppid())

	// Walk up the process tree looking for the main PID
	currentPID := ppid
	for i := 0; i < 10; i++ { // max depth
		if currentPID == mainPID || currentPID == "1" || currentPID == "0" {
			break
		}
		// Read parent of current PID
		statData, err := os.ReadFile(fmt.Sprintf("/proc/%s/stat", currentPID))
		if err != nil {
			break
		}
		fields := strings.Fields(string(statData))
		if len(fields) < 4 {
			break
		}
		currentPID = fields[3] // ppid is field 4 (0-indexed: 3)
	}

	return currentPID == mainPID
}

// fallbackStopHook provides backward-compatible behavior when no manifest exists
func fallbackStopHook() error {
	messages, err := DrainQueue()
	if err != nil {
		return err
	}

	if len(messages) > 0 {
		WriteState(StateActive)
		// Output raw message content (backward compat — no structured headers)
		for _, msg := range messages {
			fmt.Fprint(os.Stderr, msg.Content)
		}
		os.Exit(2)
	}

	// No messages — set idle state and allow stop
	WriteState(StateIdle)
	return nil // exit 0
}
