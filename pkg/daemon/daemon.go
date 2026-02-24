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

	"github.com/uprockcom/maestro/pkg/api"
	"github.com/uprockcom/maestro/pkg/container"
	"github.com/uprockcom/maestro/pkg/notify"
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
	CreateContainer    func(opts CreateContainerOpts) (string, error) // Callback for IPC child creation
}

// CreateContainerOpts holds parameters for creating a child container via the daemon callback.
type CreateContainerOpts struct {
	Task            string
	ParentContainer string
	Branch          string
	Model           string // Claude model alias: opus, sonnet, haiku (default from config if empty)
	WebEnabled      bool   // Use web-enabled image
}

// pendingApproval tracks a container-initiated resource request awaiting user approval
type pendingApproval struct {
	ContainerName string
	RequestID     string
	RequestType   string // "domain", "memory", "cpus", "ip"
	RequestValue  string
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
	notifyEngine        *notify.Engine
	localProvider       *notify.LocalProvider // direct ref for GetPending/Answer
	nicknames           *NicknameStore
	containerOps        ContainerOps
	pendingApprovals    map[string]*pendingApproval
	pendingApprovalsMu  sync.Mutex
	lastTokenSync       time.Time
	containerCache      *ContainerCache // lazy cache for API v1 endpoints
}

// ContainerState tracks container monitoring state
type ContainerState struct {
	mu                     sync.Mutex // protects all fields below
	Name                   string
	AttentionStarted       *time.Time
	LastIdleNotified       *time.Time // Last time an idle/attention notification was sent (scoped rate limit)
	LastActivity           time.Time
	LastTokenCheck         time.Time
	NotificationSent       bool
	LastTaskCheck          time.Time
	HadOpenTasks           bool   // Whether container had incomplete tasks last check
	LastTaskProgress       string // Last seen task progress (e.g., "2/5")
	TaskCompletionNotified bool   // Whether we've notified about task completion
	LastIPCCheck           time.Time
	LastQuestionFile       string // Serialized question content for change detection
	LastQuestionEventID    string // Event ID of the pending question notification
	QuestionNotified       bool   // Whether we've notified for the current question
	TokenExpiryNotified    bool   // Whether we've sent a token_expiring notification for current expiry
	LastTokenExpiry        int64  // ExpiresAt millis — detect token refresh
	WasClaudeRunning       bool   // Whether Claude was running in the last check cycle
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

	prefix := config.ContainerPrefix
	if prefix == "" {
		prefix = "maestro-"
	}

	d := &Daemon{
		config:           config,
		logFile:          logFile,
		stopChan:         make(chan bool),
		containerStates:  make(map[string]*ContainerState),
		startTime:        time.Now(),
		ipcToken:         token,
		configDir:        configDir,
		nicknames:        NewNicknameStore(filepath.Join(configDir, "nicknames.yml")),
		containerOps:     &dockerContainerOps{},
		pendingApprovals: make(map[string]*pendingApproval),
		containerCache:   NewContainerCache(prefix),
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

// SetEngine configures the notification engine and local provider.
func (d *Daemon) SetEngine(engine *notify.Engine, localProvider *notify.LocalProvider) {
	d.notifyEngine = engine
	d.localProvider = localProvider
}

// IconPath returns the cached icon path (for DesktopProvider setup).
func (d *Daemon) IconPath() string { return d.iconPath }

// HasTerminalNotifier returns whether terminal-notifier is available.
func (d *Daemon) HasTerminalNotifier() bool { return d.hasTerminalNotifier }

// IPCServer returns the daemon's IPC server (may be nil before Start is called).
func (d *Daemon) IPCServer() *IPCServer { return d.ipcServer }

// LogInfo exposes logging to external callers.
func (d *Daemon) LogInfo(format string, args ...interface{}) { d.logInfo(format, args...) }

// Nicknames returns the daemon's nickname store.
func (d *Daemon) Nicknames() *NicknameStore { return d.nicknames }

// sendNotification routes an event through the engine if available, else falls
// back to the legacy notify() method.
func (d *Daemon) sendNotification(event notify.Event) {
	if d.notifyEngine != nil {
		if event.Question != nil {
			d.notifyEngine.AskQuestion(event)
		} else {
			d.notifyEngine.Notify(event)
		}
		return
	}
	// Legacy fallback
	d.notify(event.Title, event.ShortName, event.Message)
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
		var existing api.DaemonIPCInfo
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
	ipcInfo := api.DaemonIPCInfo{
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

	// Run initial check immediately and warm the container cache in background
	d.check()
	go d.containerCache.ForceRefresh() //nolint:errcheck

	for {
		select {
		case <-ticker.C:
			d.check()
			go d.containerCache.ForceRefresh() //nolint:errcheck
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

	// Batch token sync: find freshest token and distribute to expired containers
	d.syncTokensAcrossContainers(containers)

	for _, container := range containers {
		state := d.getOrCreateContainerState(container)

		// Check dormant state (Claude process exited)
		claudeRunning := d.isClaudeRunning(container)
		state.mu.Lock()
		wasPreviouslyRunning := state.WasClaudeRunning
		state.WasClaudeRunning = claudeRunning
		state.mu.Unlock()

		if wasPreviouslyRunning && !claudeRunning {
			d.logInfo("Claude became dormant in %s", d.getShortName(container))
			if d.shouldNotify("dormant", state) {
				shortName := d.getShortName(container)
				event := notify.Event{
					ID:            fmt.Sprintf("dormant-%s-%d", container, time.Now().UnixMilli()),
					ContainerName: container,
					ShortName:     shortName,
					Title:         "Dormant",
					Message:       "Claude process has exited",
					Type:          notify.EventDormant,
					Timestamp:     time.Now(),
					Contacts:      d.getContainerContacts(container),
				}
				d.sendNotification(event)
			}
		}

		// Check for pending questions (fast path — no gating)
		d.checkQuestionStatus(container, state)

		// Check token expiry
		d.checkTokenExpiry(container, state)

		// Check attention status (idle notifications — gated by threshold + rate limit)
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

// checkQuestionStatus checks for pending questions every cycle with no gating.
// Questions are time-sensitive (interactive Q&A) and should fire within one check cycle.
func (d *Daemon) checkQuestionStatus(containerName string, state *ContainerState) {
	// Only check if Claude is running
	if !d.isClaudeRunning(containerName) {
		return
	}

	qd, err := notify.ReadContainerQuestion(containerName)
	if err != nil {
		d.logError("Failed to read question from %s: %v", containerName, err)
		return
	}

	state.mu.Lock()
	if qd != nil {
		// Serialize to compare with last known question
		qdJSON, _ := json.Marshal(qd)
		qdStr := string(qdJSON)

		if qdStr != state.LastQuestionFile {
			// New question detected — notify immediately
			state.LastQuestionFile = qdStr
			state.QuestionNotified = true

			shortName := d.getShortName(containerName)
			eventID := fmt.Sprintf("question-%s-%d", containerName, time.Now().UnixMilli())
			state.LastQuestionEventID = eventID
			state.mu.Unlock()

			event := notify.Event{
				ID:            eventID,
				ContainerName: containerName,
				ShortName:     shortName,
				Title:         "Question",
				Message:       "Has a question waiting for your answer",
				Type:          notify.EventQuestion,
				Timestamp:     time.Now(),
				Question:      qd,
				Contacts:      d.getContainerContacts(containerName),
			}
			d.sendNotification(event)
			d.logInfo("Question detected in %s, notifying immediately", shortName)
			return
		}
		// Same question as before — already notified
		state.mu.Unlock()
	} else {
		// No question file — if we previously had one, cancel the pending notification
		prevEventID := state.LastQuestionEventID
		state.LastQuestionFile = ""
		state.LastQuestionEventID = ""
		state.QuestionNotified = false
		state.mu.Unlock()

		if prevEventID != "" && d.notifyEngine != nil {
			d.notifyEngine.CancelQuestionWithNotify(prevEventID)
		}
	}
}

// syncTokensAcrossContainers finds the freshest valid token and syncs it to all
// containers (and the host) that have expired or older tokens. Runs once per check
// cycle with a 5-minute rate limit.
func (d *Daemon) syncTokensAcrossContainers(containers []string) {
	d.mu.Lock()
	if time.Since(d.lastTokenSync) < 5*time.Minute {
		d.mu.Unlock()
		return
	}
	d.lastTokenSync = time.Now()
	d.mu.Unlock()

	// Find the freshest valid token across host + all containers
	freshest, err := container.FindFreshestToken(d.config.ContainerPrefix)
	if err != nil {
		d.logInfo("Token sync: no valid token found anywhere (%v)", err)
		return
	}
	defer func() {
		if freshest.IsTempFile {
			os.Remove(freshest.Path)
		}
	}()

	d.logInfo("Token sync: freshest token from %s (expires %s)", freshest.Source, freshest.ExpiresAt.Format(time.RFC1123))

	synced := 0

	// Sync to host if needed
	hostCredPath := filepath.Join(d.configDir, ".credentials.json")
	if freshest.Source != "host" {
		needsHostSync := false
		if hostCreds, err := readCredentials(hostCredPath); err != nil {
			needsHostSync = true
		} else if time.Now().UnixMilli() >= hostCreds.ClaudeAiOauth.ExpiresAt {
			needsHostSync = true
		} else if freshest.ExpiresAt.After(time.UnixMilli(hostCreds.ClaudeAiOauth.ExpiresAt)) {
			needsHostSync = true
		}

		if needsHostSync {
			data, err := os.ReadFile(freshest.Path)
			if err == nil {
				if err := os.WriteFile(hostCredPath, data, 0600); err == nil {
					synced++
					d.logInfo("Token sync: updated host credentials")
				} else {
					d.logError("Token sync: failed to write host credentials: %v", err)
				}
			}
		}
	}

	// Sync to containers that need it
	for _, containerName := range containers {
		if containerName == freshest.Source {
			continue
		}

		// Check container's current token
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("maestro-sync-%s-%d.json", containerName, time.Now().Unix()))
		copyCmd := exec.Command("docker", "cp",
			fmt.Sprintf("%s:/home/node/.claude/.credentials.json", containerName),
			tmpFile)

		needsSync := false
		if err := copyCmd.Run(); err != nil {
			needsSync = true // No credentials at all
		} else {
			creds, err := readCredentials(tmpFile)
			os.Remove(tmpFile)
			if err != nil {
				needsSync = true
			} else if time.Now().UnixMilli() >= creds.ClaudeAiOauth.ExpiresAt {
				needsSync = true
			} else if freshest.ExpiresAt.After(time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)) {
				needsSync = true
			}
		}

		if needsSync {
			destPath := fmt.Sprintf("%s:/home/node/.claude/.credentials.json", containerName)
			syncCmd := exec.Command("docker", "cp", freshest.Path, destPath)
			if err := syncCmd.Run(); err != nil {
				d.logError("Token sync: failed to copy to %s: %v", d.getShortName(containerName), err)
				continue
			}

			chownCmd := exec.Command("docker", "exec", "-u", "root", containerName,
				"chown", "node:node", "/home/node/.claude/.credentials.json")
			if err := chownCmd.Run(); err != nil {
				d.logError("Token sync: failed to fix ownership on %s: %v", d.getShortName(containerName), err)
			}

			synced++
			d.logInfo("Token sync: updated %s", d.getShortName(containerName))
		}
	}

	if synced > 0 {
		d.logInfo("Token sync complete: updated %d location(s) from %s", synced, freshest.Source)
	}
}

// checkTokenExpiry sends notifications for tokens that are expiring soon or expired.
// Actual token syncing is handled by syncTokensAcrossContainers() which runs before
// the per-container loop.
func (d *Daemon) checkTokenExpiry(containerName string, state *ContainerState) {
	// Don't check too frequently (every 5 minutes is enough)
	state.mu.Lock()
	if time.Since(state.LastTokenCheck) < 5*time.Minute {
		state.mu.Unlock()
		return
	}
	state.LastTokenCheck = time.Now()
	state.mu.Unlock()

	// Extract credentials
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("maestro-creds-%s-%d.json", containerName, time.Now().Unix()))
	defer os.Remove(tmpFile)

	copyCmd := exec.Command("docker", "cp",
		fmt.Sprintf("%s:/home/node/.claude/.credentials.json", containerName),
		tmpFile)
	if err := copyCmd.Run(); err != nil {
		return // No credentials, skip
	}

	creds, err := readCredentials(tmpFile)
	if err != nil {
		return
	}

	timeLeft := time.Until(time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt))

	// Detect token changes (e.g. from batch sync) and clear notification flag
	state.mu.Lock()
	if creds.ClaudeAiOauth.ExpiresAt != state.LastTokenExpiry {
		state.TokenExpiryNotified = false
		state.LastTokenExpiry = creds.ClaudeAiOauth.ExpiresAt
	}
	alreadyNotified := state.TokenExpiryNotified
	state.mu.Unlock()

	// Notify if token is expired or expiring within threshold
	if timeLeft < d.config.TokenThreshold && !alreadyNotified && d.shouldNotify("token_expiring", state) {
		shortName := d.getShortName(containerName)
		var message string
		if timeLeft <= 0 {
			message = fmt.Sprintf("Token expired %.1fh ago — no fresh token available to sync", -timeLeft.Hours())
		} else {
			message = fmt.Sprintf("Token expires in %.1fh", timeLeft.Hours())
		}

		event := notify.Event{
			ID:            fmt.Sprintf("token-%s-%d", containerName, time.Now().UnixMilli()),
			ContainerName: containerName,
			ShortName:     shortName,
			Title:         "Token Expiring",
			Message:       message,
			Type:          notify.EventTokenExpiring,
			Timestamp:     time.Now(),
			Contacts:      d.getContainerContacts(containerName),
		}
		d.sendNotification(event)
		state.mu.Lock()
		state.TokenExpiryNotified = true
		state.mu.Unlock()
	}
}

// checkAttentionStatus monitors container idle/attention state via agent state.
// Questions are handled separately by checkQuestionStatus(); this only sends
// EventAttentionNeeded notifications, gated by the attention threshold and a
// 30-minute idle-specific rate limit.
func (d *Daemon) checkAttentionStatus(containerName string, state *ContainerState) {
	// Read agent state — only idle/waiting are "needs attention".
	// Question state is handled by checkQuestionStatus separately.
	agentState := container.ReadAgentState(containerName)
	needsAttention := (agentState == "idle" || agentState == "waiting") && d.isClaudeRunning(containerName)

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

		// Check if we should notify (threshold + idle-specific rate limit)
		// Skip if a question is already pending — the question notification is sufficient
		attentionDuration := time.Since(*state.AttentionStarted)
		idleRateLimited := state.LastIdleNotified != nil && time.Since(*state.LastIdleNotified) < 30*time.Minute
		hasQuestion := state.QuestionNotified
		shouldSend := !state.NotificationSent && !hasQuestion && attentionDuration >= d.config.AttentionThreshold && !idleRateLimited
		state.mu.Unlock()

		if newAttention {
			d.logInfo("Container %s needs attention", d.getShortName(containerName))
		}

		if shouldSend && d.shouldNotify("attention_needed", state) {
			shortName := d.getShortName(containerName)
			event := notify.Event{
				ID:            fmt.Sprintf("attn-%s-%d", containerName, time.Now().UnixMilli()),
				ContainerName: containerName,
				ShortName:     shortName,
				Title:         "Needs Attention",
				Message:       fmt.Sprintf("Has needed attention for %s", formatDuration(attentionDuration)),
				Type:          notify.EventAttentionNeeded,
				Timestamp:     time.Now(),
				Contacts:      d.getContainerContacts(containerName),
			}

			d.sendNotification(event)

			state.mu.Lock()
			state.NotificationSent = true
			state.LastIdleNotified = timeNow()
			state.mu.Unlock()
		}
	} else {
		// Clear attention state — also clear LastIdleNotified so the next
		// idle event can fire immediately after attention is resolved.
		wasAttending := state.AttentionStarted != nil
		state.AttentionStarted = nil
		state.NotificationSent = false
		if wasAttending {
			state.LastIdleNotified = nil
		}
		state.mu.Unlock()

		if wasAttending {
			d.logInfo("Container %s attention resolved", d.getShortName(containerName))
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

		// Send notification if type is enabled (no rate limit — TaskCompletionNotified dedup is sufficient)
		if d.shouldNotify("tasks_completed", state) {
			shortName := d.getShortName(containerName)
			event := notify.Event{
				ID:            fmt.Sprintf("tasks-%s-%d", containerName, time.Now().UnixMilli()),
				ContainerName: containerName,
				ShortName:     shortName,
				Title:         "Tasks Completed",
				Message:       fmt.Sprintf("Finished all tasks (%s)", summary.Progress),
				Type:          notify.EventTasksCompleted,
				Timestamp:     time.Now(),
				Contacts:      d.getContainerContacts(containerName),
			}
			d.sendNotification(event)
			state.mu.Lock()
			state.TaskCompletionNotified = true
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

// shouldNotify checks if a notification type is enabled and not in quiet hours.
// Rate limiting is handled per-notification-type by the caller.
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

	// Check quiet hours — blockers bypass quiet hours
	if notifyType != "blocker" && d.isQuietHours() {
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
			if name == "maestro-signal-cli" {
				continue
			}
			containers = append(containers, name)
		}
	}
	return containers, nil
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
	if d.notifyEngine != nil {
		d.notifyEngine.Close()
	}
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

// StartBackgroundTask runs a function in a tracked goroutine. The function
// receives the daemon's stop channel so it can shut down gracefully.
func (d *Daemon) StartBackgroundTask(fn func(stopChan <-chan bool)) {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		fn(d.stopChan)
	}()
}

// RegisterApproval stores a pending approval keyed by event ID
func (d *Daemon) RegisterApproval(eventID string, approval *pendingApproval) {
	d.pendingApprovalsMu.Lock()
	d.pendingApprovals[eventID] = approval
	d.pendingApprovalsMu.Unlock()
}

// PopApproval retrieves and removes a pending approval by event ID
func (d *Daemon) PopApproval(eventID string) *pendingApproval {
	d.pendingApprovalsMu.Lock()
	approval, ok := d.pendingApprovals[eventID]
	if ok {
		delete(d.pendingApprovals, eventID)
	}
	d.pendingApprovalsMu.Unlock()
	if !ok {
		return nil
	}
	return approval
}

// ExecuteApproval performs the approved resource action
func (d *Daemon) ExecuteApproval(approval *pendingApproval, resp notify.Response) (bool, error) {
	// Check if "Approve" was selected
	approved := false
	for _, s := range resp.Selections {
		if s == "Approve" {
			approved = true
			break
		}
	}

	if !approved {
		return false, nil
	}

	var err error
	switch approval.RequestType {
	case "domain":
		err = container.AddDomainToContainer(approval.ContainerName, approval.RequestValue)
	case "memory":
		err = container.UpdateContainerResources(approval.ContainerName, approval.RequestValue, "")
	case "cpus":
		err = container.UpdateContainerResources(approval.ContainerName, "", approval.RequestValue)
	case "ip":
		err = container.AddIPToContainer(approval.ContainerName, approval.RequestValue)
	default:
		err = fmt.Errorf("unknown request type: %s", approval.RequestType)
	}

	return true, err
}

// createChildContainer delegates to the configured CreateContainer callback
func (d *Daemon) createChildContainer(opts CreateContainerOpts) (string, error) {
	if d.config.CreateContainer == nil {
		return "", fmt.Errorf("container creation not configured")
	}
	return d.config.CreateContainer(opts)
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

// getContainerContacts reads the maestro.contacts Docker label and parses it
// into a nested map suitable for notify.Event.Contacts.
func (d *Daemon) getContainerContacts(containerName string) map[string]map[string]string {
	raw := d.containerOps.GetLabel(containerName, "maestro.contacts")
	if raw == "" {
		return nil
	}
	var contacts map[string]map[string]string
	if err := json.Unmarshal([]byte(raw), &contacts); err != nil {
		d.logInfo("getContainerContacts: failed to parse label for %s: %v", containerName, err)
		return nil
	}
	return contacts
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
