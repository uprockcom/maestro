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

package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/uprockcom/maestro/pkg/paths"
	"github.com/mistakenelf/teacup/statusbar"
	"github.com/spf13/viper"
	"go.dalton.dog/bubbleup"

	"github.com/uprockcom/maestro/pkg/container"
	"github.com/uprockcom/maestro/pkg/system"
	"github.com/uprockcom/maestro/pkg/tui/style"
	"github.com/uprockcom/maestro/pkg/tui/views"
)

// Model is the main TUI model
type Model struct {
	width               int
	height              int
	ready               bool
	result              *TUIResult
	homeView            *views.HomeModel
	containerPrefix     string
	modal               *Modal              // Active modal (nil if none)
	help                help.Model          // Help component for keybindings
	keys                keyMap              // Keybindings
	cachedCursorPos     int                 // Cursor position to restore from cache
	spinner             spinner.Model       // Loading spinner
	loading             bool                // Whether we're currently loading
	alert               bubbleup.AlertModel // Toast notifications
	statusbar           statusbar.Model     // Status bar for persistent state
	containerCount      int                 // Number of containers
	operationStatus     string              // Current operation status
	daemonRunning       bool                // Whether daemon is running
	workingDir          string              // Current working directory (relative to ~)
	animationFrame      int                 // Animation frame counter for pulsing effects
	operationInProgress bool                // Whether an operation is currently running
	operationSpinner    spinner.Model       // Spinner for operations in statusbar

	// Wizard state
	wizardMode        bool     // Whether we're in wizard/onboarding mode
	wizardStep        int      // Current wizard step (0=animation, 1=prereq, 2=welcome, 3=auth, 4=firewall, 5=defaults, 6=completion)
	animationColumn   int      // Current column being animated
	animationComplete bool     // Whether opening animation is complete
	wizardMemory      string   // Memory limit chosen in wizard
	wizardCPUs        string   // CPU limit chosen in wizard
	wizardDomains     []string // Firewall domains chosen in wizard
	wizardRunAuthNow  bool     // Whether to run maestro auth after wizard
}

// keyMap defines keybindings for different contexts
type keyMap struct {
	// Normal view keys
	Up       key.Binding
	Down     key.Binding
	Connect  key.Binding
	Actions  key.Binding
	Info     key.Binding
	New      key.Binding
	Settings key.Binding
	Firewall key.Binding
	Help     key.Binding
	Quit     key.Binding

	// Modal keys (set dynamically based on modal type)
	ModalSelect   key.Binding
	ModalNavigate key.Binding
	ModalClose    key.Binding
	ModalAction1  key.Binding
	ModalAction2  key.Binding
	ModalAction3  key.Binding
	ModalAction4  key.Binding
	ModalAction5  key.Binding
}

// ShortHelp returns keybindings to be shown in the mini help view
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Connect, k.Actions, k.Info, k.New, k.Settings, k.Firewall, k.Help, k.Quit}
}

// FullHelp returns keybindings for the expanded help view
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Connect, k.Actions, k.Info, k.New, k.Settings, k.Firewall},
		{k.Help, k.Quit},
	}
}

// New creates a new TUI model
func New(containerPrefix string) *Model {
	return NewWithCache(containerPrefix, nil)
}

// isFirstRun checks if this is the first time running the TUI
func isFirstRun() bool {
	// Check wizard.always_run config (for testing)
	if viper.GetBool("wizard.always_run") {
		return true
	}

	// Check if resuming wizard after auth
	if viper.GetBool("wizard.resume_after_auth") {
		return true
	}

	// Check if config file exists
	configPath := paths.ConfigFile()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return true // No config file = first run
	}

	// Skip credential check if Bedrock is enabled (uses AWS auth instead)
	if viper.GetBool("bedrock.enabled") {
		return false
	}

	// Check if credentials exist
	credPath := viper.GetString("claude.auth_path")
	if credPath == "" {
		credPath = paths.AuthDir()
	}
	// Expand ~ in path
	if strings.HasPrefix(credPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return false // Can't determine, skip wizard
		}
		credPath = filepath.Join(home, credPath[1:])
	}

	credsFile := filepath.Join(credPath, ".credentials.json")
	if _, err := os.Stat(credsFile); os.IsNotExist(err) {
		return true // No credentials = first run
	}

	return false
}

// NewWithCache creates a new TUI model with optional cached state
func NewWithCache(containerPrefix string, cached *CachedState) *Model {
	// Initialize spinner with Ocean Tide color
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(style.OceanTide)

	// Initialize operation spinner for statusbar (braille characters for subtle animation)
	opSpinner := spinner.New()
	opSpinner.Spinner = spinner.Spinner{
		Frames: []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"},
		FPS:    time.Second / 10, // 100ms per frame
	}
	opSpinner.Style = lipgloss.NewStyle().
		Foreground(style.OceanTide).
		Background(style.PurpleHaze) // Match Column 3 background

	// Initialize alert/toast system with Ocean Tide colors
	alertModel := bubbleup.NewAlertModel(80, false) // width=80, useNerdFont=false

	// Register custom Ocean Tide alert types
	alertModel.RegisterNewAlertType(bubbleup.AlertDefinition{
		Key:       "Success",
		ForeColor: string(style.NeonGreen), // #00FF41
		Prefix:    "✓",
	})
	alertModel.RegisterNewAlertType(bubbleup.AlertDefinition{
		Key:       "Info",
		ForeColor: string(style.OceanTide), // #00BCD4
		Prefix:    "ℹ",
	})
	alertModel.RegisterNewAlertType(bubbleup.AlertDefinition{
		Key:       "Warning",
		ForeColor: string(style.SunsetGlow), // #FCC451
		Prefix:    "⚠",
	})
	alertModel.RegisterNewAlertType(bubbleup.AlertDefinition{
		Key:       "Error",
		ForeColor: string(style.CrimsonPulse), // #C52735
		Prefix:    "✗",
	})

	// Initialize statusbar with Ocean Tide 4-column layout
	sb := statusbar.New(
		// Column 1: Container count (OceanTide - primary)
		statusbar.ColorConfig{
			Foreground: lipgloss.AdaptiveColor{Light: string(style.OceanTide), Dark: string(style.OceanTide)},
			Background: lipgloss.AdaptiveColor{Light: string(style.DeepSpace), Dark: string(style.DeepSpace)},
		},
		// Column 2: Operation status (DimGray - secondary)
		statusbar.ColorConfig{
			Foreground: lipgloss.AdaptiveColor{Light: string(style.GhostWhite), Dark: string(style.GhostWhite)},
			Background: lipgloss.AdaptiveColor{Light: string(style.DimGray), Dark: string(style.DimGray)},
		},
		// Column 3: Time/Info (PurpleHaze - tertiary)
		statusbar.ColorConfig{
			Foreground: lipgloss.AdaptiveColor{Light: string(style.GhostWhite), Dark: string(style.GhostWhite)},
			Background: lipgloss.AdaptiveColor{Light: string(style.PurpleHaze), Dark: string(style.PurpleHaze)},
		},
		// Column 4: Mode indicator (OceanAbyss)
		statusbar.ColorConfig{
			Foreground: lipgloss.AdaptiveColor{Light: string(style.GhostWhite), Dark: string(style.GhostWhite)},
			Background: lipgloss.AdaptiveColor{Light: string(style.OceanAbyss), Dark: string(style.OceanAbyss)},
		},
	)

	// Get current working directory relative to home
	cwd, _ := os.Getwd()
	homeDir, _ := os.UserHomeDir()
	relPath := cwd
	if strings.HasPrefix(cwd, homeDir) {
		relPath = "~" + strings.TrimPrefix(cwd, homeDir)
	}

	m := &Model{
		containerPrefix:     containerPrefix,
		help:                help.New(),
		spinner:             s,
		loading:             cached == nil || len(cached.Containers) == 0, // Loading if no cache
		alert:               *alertModel,
		statusbar:           sb,
		containerCount:      0,
		operationStatus:     "Ready",
		daemonRunning:       true, // TODO: Check actual daemon status
		workingDir:          relPath,
		animationFrame:      0,
		operationInProgress: false,
		operationSpinner:    opSpinner,
		keys: keyMap{
			Up: key.NewBinding(
				key.WithKeys("up", "k"),
				key.WithHelp("↑/k", "navigate"),
			),
			Down: key.NewBinding(
				key.WithKeys("down", "j"),
				key.WithHelp("↓/j", "navigate"),
			),
			Connect: key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("↵", "connect"),
			),
			Actions: key.NewBinding(
				key.WithKeys("a"),
				key.WithHelp("a", "actions"),
			),
			Info: key.NewBinding(
				key.WithKeys("i"),
				key.WithHelp("i", "details"),
			),
			New: key.NewBinding(
				key.WithKeys("n"),
				key.WithHelp("n", "new"),
			),
			Settings: key.NewBinding(
				key.WithKeys("s"),
				key.WithHelp("s", "settings"),
			),
			Firewall: key.NewBinding(
				key.WithKeys("f"),
				key.WithHelp("f", "firewall"),
			),
			Help: key.NewBinding(
				key.WithKeys("?"),
				key.WithHelp("?", "help"),
			),
			Quit: key.NewBinding(
				key.WithKeys("q", "ctrl+c"),
				key.WithHelp("q", "quit"),
			),
		},
	}

	// Check if this is first run and enable wizard mode
	if isFirstRun() {
		m.wizardMode = true
		m.loading = false // Don't show loading spinner in wizard mode

		// Check if resuming after auth
		resumingAfterAuth := viper.GetBool("wizard.resume_after_auth")
		if resumingAfterAuth {
			// Skip animation and jump directly to Authentication screen (step 3)
			m.wizardStep = 3
			m.animationColumn = 0
			m.animationComplete = true
			// Don't create modal yet - wait for WindowSizeMsg to get dimensions
			m.modal = nil
		} else {
			// Normal first run - start with animation
			m.wizardStep = 0 // 0 = animation
			m.animationColumn = 0
			m.animationComplete = false
		}

		// Initialize wizard config with values from config or sensible defaults
		m.wizardMemory = viper.GetString("containers.resources.memory")
		if m.wizardMemory == "" {
			m.wizardMemory = "4g" // Default from viper defaults
		}

		m.wizardCPUs = viper.GetString("containers.resources.cpus")
		if m.wizardCPUs == "" {
			m.wizardCPUs = "2" // Default from viper defaults
		}

		m.wizardDomains = viper.GetStringSlice("firewall.allowed_domains")
		if len(m.wizardDomains) == 0 {
			// Use default domains from viper defaults
			m.wizardDomains = []string{
				"registry.npmjs.org",
				"api.anthropic.com",
				"github.com",
				"pypi.org",
				"files.pythonhosted.org",
				"sentry.io",
				"statsig.anthropic.com",
				"statsig.com",
			}
		}

		m.wizardRunAuthNow = false
	} else {
		// Normal mode: If we have cached state, initialize with it for instant render
		if cached != nil && len(cached.Containers) > 0 {
			m.homeView = views.NewHomeModel(cached.Containers, false)
			m.ready = true // Skip "Loading..."
			m.cachedCursorPos = cached.CursorPos
		} else {
			m.cachedCursorPos = -1 // No cached cursor
		}
	}

	return m
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	// Wizard mode: just initialize alert
	if m.wizardMode {
		// If animation is already complete (resuming after auth), don't start animation ticker
		if m.animationComplete {
			return m.alert.Init()
		}
		// Start animation for normal first run
		return tea.Batch(m.alert.Init(), wizardAnimationTick())
	}

	// Normal mode: Start spinner, load containers, and initialize alert system
	cmds := []tea.Cmd{m.loadContainers(), m.alert.Init()}

	// Start spinner animation if we're loading
	if m.loading {
		cmds = append(cmds, m.spinner.Tick)
	}

	// Start animation ticker (200ms for pulsing effects)
	cmds = append(cmds, animationTick())

	// Start background refresh ticker (30s)
	cmds = append(cmds, refreshTick())

	return tea.Batch(cmds...)
}

// wizardAnimationTick creates a command that sends animation ticks for the opening animation
func wizardAnimationTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(t time.Time) tea.Msg {
		return wizardAnimationTickMsg(t)
	})
}

// animationTick creates a command that sends animation tick messages every 750ms for subtle daemon pulsing
func animationTick() tea.Cmd {
	return tea.Tick(750*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// refreshTick creates a command that sends refresh tick messages every 30 seconds
func refreshTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return refreshTickMsg(t)
	})
}

// GetState exports the current state for caching
func (m Model) GetState() *CachedState {
	if m.homeView == nil {
		return nil
	}

	return &CachedState{
		Containers: m.homeView.GetContainers(),
		CursorPos:  m.homeView.GetCursor(),
	}
}

// loadContainers fetches container data
func (m Model) loadContainers() tea.Cmd {
	return func() tea.Msg {
		containers, err := container.GetAllContainers(m.containerPrefix)
		if err != nil {
			// For now, return empty list on error
			return containersLoadedMsg{containers: []container.Info{}, err: nil}
		}
		// TODO: Check daemon status
		return containersLoadedMsg{containers: containers, err: nil}
	}
}

// Update handles messages and updates state
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Always update alert model for lifecycle management (even when modal is active)
	outAlert, alertCmd := m.alert.Update(msg)
	m.alert = outAlert.(bubbleup.AlertModel)

	// Handle animation ticks before modal check to keep animations running
	switch msg.(type) {
	case tickMsg:
		// Animation tick (750ms) - update animation frame for subtle daemon pulsing
		m.animationFrame++
		// Schedule next animation tick and continue processing
		return m, tea.Batch(animationTick(), alertCmd)

	case wizardAnimationTickMsg:
		// Wizard opening animation tick (80ms per column)
		if m.wizardMode && !m.animationComplete {
			// Advance to next column
			m.animationColumn++
			// Check if animation is complete (title has ~87 columns)
			if m.animationColumn >= 87 {
				m.animationComplete = true
				// Don't schedule next tick, wait for Enter key
				return m, alertCmd
			}
			// Schedule next animation tick
			return m, tea.Batch(wizardAnimationTick(), alertCmd)
		}
		return m, alertCmd

	case refreshTickMsg:
		// Background refresh tick (30s)
		// Skip refresh if modal is active or operation in progress
		if m.modal != nil || m.operationInProgress {
			return m, tea.Batch(refreshTick(), alertCmd)
		}
		// Set syncing status and reload containers in background
		m.operationStatus = "Syncing..."
		return m, tea.Batch(m.loadContainers(), refreshTick(), alertCmd)

	case exitWizardMsg:
		// Exit wizard mode (Skip Wizard button)
		// If config doesn't exist, create default config so app can function
		configPath := paths.ConfigFile()
		if _, err := os.Stat(configPath); os.IsNotExist(err) {
			// No config exists - create minimal default config
			defaultConfig := saveWizardConfigMsg{
				memory:     m.wizardMemory,
				cpus:       m.wizardCPUs,
				domains:    m.wizardDomains,
				runAuthNow: false,
			}
			if err := m.saveWizardConfig(defaultConfig); err != nil {
				// Show error but continue anyway
				m.modal = NewErrorModal("Warning", "Could not create default config: "+err.Error())
				return m, alertCmd
			}
		}

		// Exit wizard mode and transition to normal operation
		m.wizardMode = false
		m.modal = nil
		m.loading = true
		m.ready = false

		// Start normal operation: load containers and start tickers
		cmds := []tea.Cmd{m.loadContainers(), alertCmd}
		cmds = append(cmds, m.spinner.Tick, animationTick(), refreshTick())
		return m, tea.Batch(cmds...)

	case wizardNextStepMsg:
		// Advance to next wizard step
		m.wizardStep++
		m.modal = m.getWizardModal()
		// If we're now on prerequisite check step, trigger checks
		if m.wizardStep == 1 {
			return m, tea.Batch(alertCmd, checkPrerequisites())
		}
		return m, alertCmd

	case wizardPrevStepMsg:
		// Go back to previous wizard step
		if m.wizardStep > 1 {
			m.wizardStep--
			m.modal = m.getWizardModal()
			// If we're back to prerequisite check step, trigger checks
			if m.wizardStep == 1 {
				return m, tea.Batch(alertCmd, checkPrerequisites())
			}
		}
		return m, alertCmd

	case updateWizardConfigMsg:
		// Update wizard config and advance
		m.wizardMemory = msg.(updateWizardConfigMsg).memory
		m.wizardCPUs = msg.(updateWizardConfigMsg).cpus
		m.wizardStep++
		m.modal = m.getWizardModal()
		return m, alertCmd

	case prerequisiteCheckResult:
		// Update prerequisite modal with check results
		result := msg.(prerequisiteCheckResult)

		// Use plain text indicators without colors
		// TODO: Find way to add colors without background conflicts (see backlog)
		claudeStatus := "✗"
		if result.claudeAvailable {
			claudeStatus = "✓"
		}
		dockerStatus := "✗"
		if result.dockerAvailable {
			dockerStatus = "✓"
		}

		content := fmt.Sprintf(`Prerequisite Check Complete

• Claude CLI: %s %s
• Docker: %s %s

`, claudeStatus, result.claudeMessage, dockerStatus, result.dockerMessage)

		// Add help text based on what's missing
		allPassed := result.claudeAvailable && result.dockerAvailable
		if !allPassed {
			content += "Please install missing prerequisites before continuing:\n\n"
			if !result.claudeAvailable {
				content += "• Install Claude CLI from https://claude.ai/download\n"
			}
			if !result.dockerAvailable {
				content += "• Install Docker from https://docker.com/get-started\n"
			}
			content += "\nStep 1 of 6"
		} else {
			content += "All prerequisites are installed! You're ready to continue.\n\nStep 1 of 6"
		}

		// Create updated modal with results
		m.modal = &Modal{
			Type:       ModalInfo,
			Title:      "System Requirements",
			Content:    content,
			Width:      70,
			DisableEsc: true,
			Actions:    []ModalAction{},
		}

		// Add appropriate actions based on results
		if allPassed {
			m.modal.Actions = []ModalAction{
				{Label: "Continue", Key: "enter", IsPrimary: true},
			}
			m.modal.Actions[0].OnSelect = func() tea.Msg {
				return wizardNextStepMsg{}
			}
		} else {
			m.modal.Actions = []ModalAction{
				{Label: "Exit", Key: "enter", IsPrimary: false},
			}
			m.modal.Actions[0].OnSelect = func() tea.Msg {
				return exitWizardMsg{}
			}
		}

		return m, alertCmd

	case saveWizardConfigMsg:
		// Save wizard configuration to file and exit wizard
		configMsg := msg.(saveWizardConfigMsg)
		err := m.saveWizardConfig(configMsg)
		if err != nil {
			// Show error modal
			m.modal = NewErrorModal("Failed to save configuration", err.Error())
			return m, alertCmd
		}

		// If runAuthNow is set, exit TUI to run auth command
		if configMsg.runAuthNow {
			m.result = &TUIResult{Action: ActionRunAuth}
			return m, tea.Quit
		}

		// Otherwise, exit wizard and start normal operation
		m.wizardMode = false
		m.modal = nil
		m.loading = true
		m.ready = false

		// Start normal operation: load containers and start tickers
		cmds := []tea.Cmd{m.loadContainers(), alertCmd}
		cmds = append(cmds, m.spinner.Tick, animationTick(), refreshTick())

		// Show success toast
		toastCmd := m.alert.NewAlertCmd("Success", "Configuration saved!")
		return m, tea.Batch(append(cmds, toastCmd)...)
	}

	// Check for 'q' to quit even when modal is active (only in wizard mode)
	if m.wizardMode {
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if keyMsg.String() == "q" || keyMsg.String() == "ctrl+c" {
				m.result = &TUIResult{Action: ActionQuit}
				return m, tea.Quit
			}
		}
	}

	// If modal is active, it gets priority for keyboard input
	if m.modal != nil {
		var modalCmd tea.Cmd
		m.modal, modalCmd = m.modal.Update(msg)
		return m, tea.Batch(modalCmd, alertCmd)
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

		// Set help width
		m.help.Width = msg.Width

		// If in wizard mode with a step but no modal yet, create it now that we have dimensions
		var wizardCheckCmd tea.Cmd
		if m.wizardMode && m.wizardStep > 0 && m.modal == nil {
			m.modal = m.getWizardModal()
			// If we just created the prerequisite modal, trigger checks
			if m.wizardStep == 1 {
				wizardCheckCmd = checkPrerequisites()
			}
		}

		// Pass size to home view if it exists
		// Subtract 9 lines: title banner (6) + help (1) + blank line (1) + statusbar (1)
		if m.homeView != nil {
			m.homeView.SetSize(msg.Width, msg.Height-9)

			// Restore cursor position from cache after sizing
			if m.cachedCursorPos >= 0 {
				m.homeView.SetCursor(m.cachedCursorPos)
				m.cachedCursorPos = -1 // Clear after restoring
			}
		}
		return m, wizardCheckCmd

	case spinner.TickMsg:
		// Update loading spinner animation if loading
		var cmds []tea.Cmd
		if m.loading {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		// Also update operation spinner if operation in progress
		if m.operationInProgress {
			var cmd tea.Cmd
			m.operationSpinner, cmd = m.operationSpinner.Update(msg)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case containersLoadedMsg:
		// Save currently selected container name for cursor preservation
		var selectedContainerName string
		if m.homeView != nil && len(m.homeView.GetContainers()) > 0 {
			cursor := m.homeView.GetCursor()
			containers := m.homeView.GetContainers()
			if cursor >= 0 && cursor < len(containers) {
				selectedContainerName = containers[cursor].Name
			}
		}

		// Initialize home view with loaded data
		m.homeView = views.NewHomeModel(msg.containers, false)
		if m.width > 0 && m.height > 0 {
			// Subtract 9 lines: title banner (6) + help (1) + blank line (1) + statusbar (1)
			m.homeView.SetSize(m.width, m.height-9)
		}

		// Restore cursor to same container if it still exists
		if selectedContainerName != "" {
			for i, c := range msg.containers {
				if c.Name == selectedContainerName {
					m.homeView.SetCursor(i)
					break
				}
			}
		}

		// Stop loading and reset operation status to Ready
		m.loading = false
		m.operationStatus = "Ready"

		// Update container count
		m.containerCount = len(msg.containers)
		m.updateStatusBar()

		// Only show toast for initial load, not background refreshes
		var toastCmd tea.Cmd
		if m.ready {
			// Background refresh - silent
			toastCmd = nil
		} else {
			// Initial load - show toast
			toastCmd = m.alert.NewAlertCmd("Success", fmt.Sprintf("Loaded %d containers", len(msg.containers)))
			// Mark as ready now that initial load is complete
			m.ready = true
		}
		return m, toastCmd

	case views.ConnectRequestMsg:
		// User pressed Enter to connect to a container
		m.result = &TUIResult{
			Action:        ActionConnect,
			ContainerName: msg.ContainerName,
		}
		return m, tea.Quit

	case views.ShowActionsMenuMsg:
		// Show actions menu for container
		m.modal = createActionsModal(msg.Container)
		return m, nil

	case createContainerMsg:
		// User submitted create container form - exit TUI and return to CLI
		m.result = &TUIResult{
			Action:          ActionCreate,
			TaskDescription: msg.taskDescription,
			BranchName:      msg.branchName,
			NoConnect:       msg.noConnect,
			Exact:           msg.exact,
		}
		return m, tea.Quit

	case saveSettingsMsg:
		// User saved settings - update viper and write config
		m.modal = nil // Close settings modal

		// Update container resource defaults
		if msg.memory != "" {
			viper.Set("containers.resources.memory", msg.memory)
		}
		if msg.cpus != "" {
			viper.Set("containers.resources.cpus", msg.cpus)
		}

		// Update daemon settings
		viper.Set("daemon.show_nag", msg.showNag)
		viper.Set("daemon.token_refresh.enabled", msg.autoRefreshTokens)
		viper.Set("daemon.notifications.enabled", msg.enableNotifications)

		// Write config to file
		if err := viper.WriteConfig(); err != nil {
			// If config file doesn't exist, create it
			if err := viper.SafeWriteConfig(); err != nil {
				toastCmd := m.alert.NewAlertCmd("Error", "Failed to save settings: "+err.Error())
				return m, toastCmd
			}
		}

		toastCmd := m.alert.NewAlertCmd("Success", "Settings saved successfully")
		return m, toastCmd

	case saveFirewallMsg:
		// User saved firewall config - update viper, optionally apply to running containers
		m.modal = nil // Close firewall modal

		// Parse domains from textarea (one per line, filter empty lines)
		lines := strings.Split(msg.domainsText, "\n")
		var newDomains []string
		for _, line := range lines {
			domain := strings.TrimSpace(line)
			if domain != "" {
				newDomains = append(newDomains, domain)
			}
		}

		// Get old domains to compare
		oldDomains := viper.GetStringSlice("firewall.allowed_domains")

		// Update config with new domains
		viper.Set("firewall.allowed_domains", newDomains)

		// Write config to file
		if err := viper.WriteConfig(); err != nil {
			if err := viper.SafeWriteConfig(); err != nil {
				toastCmd := m.alert.NewAlertCmd("Error", "Failed to save firewall: "+err.Error())
				return m, toastCmd
			}
		}

		// If "apply to running" is checked, add new domains to all running containers
		if msg.applyToRunning {
			// Find domains that are new (not in old list)
			addedDomains := []string{}
			for _, newDomain := range newDomains {
				found := false
				for _, oldDomain := range oldDomains {
					if newDomain == oldDomain {
						found = true
						break
					}
				}
				if !found {
					addedDomains = append(addedDomains, newDomain)
				}
			}

			// Apply new domains to running containers
			if len(addedDomains) > 0 {
				go func() {
					for _, domain := range addedDomains {
						// Call add-domain command for each new domain
						// This will apply to all running containers
						container.AddDomainToAllContainers(domain)
					}
				}()
				toastMsg := fmt.Sprintf("Firewall saved. Adding %d new domain(s) to running containers...", len(addedDomains))
				toastCmd := m.alert.NewAlertCmd("Info", toastMsg)
				return m, toastCmd
			}
		}

		toastCmd := m.alert.NewAlertCmd("Success", "Firewall configuration saved")
		return m, toastCmd

	case ContainerActionMsg:
		// Handle container action
		return m.handleContainerAction(msg)

	case ConfirmActionMsg:
		// Mark operation in progress and update status
		m.operationInProgress = true
		if msg.Action == container.OperationDelete {
			m.operationStatus = "Deleting..."
		} else if msg.Action == container.OperationStop {
			m.operationStatus = "Stopping..."
		}

		// Execute confirmed action asynchronously
		return m, tea.Batch(m.performDockerOperation(msg.Action, msg.ContainerName), m.operationSpinner.Tick)

	case dockerOperationResult:
		// Clear operation in progress flag
		m.operationInProgress = false

		// Handle result of Docker operation
		if msg.success {
			// Success - show toast
			actionVerb := string(msg.action)
			if msg.action == container.OperationDelete {
				actionVerb = "removed"
			} else if msg.action == container.OperationStop {
				actionVerb = "stopped"
			} else if msg.action == container.OperationRestart {
				actionVerb = "restarted"
			} else if msg.action == container.OperationRefreshTokens {
				actionVerb = "tokens refreshed for"
			}
			toastCmd := m.alert.NewAlertCmd("Success", fmt.Sprintf("Container %s %s", msg.containerName, actionVerb))

			// Reload container list immediately for all operations (to update auth status, state changes, etc.)
			m.operationStatus = "Syncing..."
			return m, tea.Batch(toastCmd, m.loadContainers())
		} else {
			// Error - reset to Ready and show modal
			m.operationStatus = "Ready"
			m.modal = NewErrorModal("Operation Failed", fmt.Sprintf("Failed to %s container %s:\n\n%v", msg.action, msg.containerName, msg.err))
			return m, nil
		}

	case tea.KeyMsg:
		// Wizard mode: Handle Enter key to proceed after animation
		if m.wizardMode && m.animationComplete && m.wizardStep == 0 {
			if msg.String() == "enter" {
				// Animation complete, user pressed Enter - show prerequisite check modal
				m.wizardStep = 1
				m.modal = createPrerequisiteCheckModal()
				// Start prerequisite checks asynchronously
				return m, checkPrerequisites()
			}
		}

		switch msg.String() {
		case "q", "ctrl+c":
			m.result = &TUIResult{Action: ActionQuit}
			return m, tea.Quit
		case "?":
			// Show help modal (skip in wizard mode)
			if !m.wizardMode {
				m.modal = createHelpModal()
			}
			return m, nil
		case "i":
			// Show container details for selected container
			if m.homeView != nil && len(m.homeView.GetContainers()) > 0 {
				selectedIdx := m.homeView.GetCursor()
				containers := m.homeView.GetContainers()
				if selectedIdx >= 0 && selectedIdx < len(containers) {
					selected := containers[selectedIdx]
					details, err := container.GetContainerDetails(selected.Name, m.containerPrefix)
					if err != nil {
						m.modal = NewErrorModal("Error", fmt.Sprintf("Failed to fetch container details:\n\n%v", err))
					} else {
						m.modal = createContainerDetailsModal(details)
					}
				}
			}
			return m, nil
		case "n":
			// Show create container form
			m.modal = createContainerCreateModal()
			return m, nil
		case "s":
			// Show settings form
			m.modal = createSettingsModal()
			return m, nil
		case "f":
			// Show firewall configuration form
			m.modal = createFirewallModal()
			return m, nil
		}
	}

	// Route to home view if ready
	var homeCmd tea.Cmd
	if m.homeView != nil {
		_, homeCmd = m.homeView.Update(msg)
	}

	// Batch home and alert commands (alert already updated at top of Update)
	return m, tea.Batch(homeCmd, alertCmd)
}

// createHelpModal creates the help/keybindings modal
func createHelpModal() *Modal {
	helpText := `Navigation:
  ↑/↓ or j/k    Navigate list
  Enter         Connect to container

Actions:
  a             Container actions menu
  i             View container details
  ?             Show this help
  q             Quit Maestro

Container Connection:
  Ctrl+b d      Detach from container
  Ctrl+b 0      Switch to Claude window
  Ctrl+b 1      Switch to shell window

Scrolling in Modals:
  ↑/↓ or j/k    Scroll line by line
  PgUp/PgDn     Scroll page by page
  Space         Scroll down one page
  Home          Jump to top
  End           Jump to bottom

This is scrollable content - try scrolling if you see
the scroll indicators (▲/▼) below this text!`

	// Use scrollable modal with 10 lines visible
	return NewScrollableHelpModal("Maestro Keybindings", helpText, 10)
}

// createPrerequisiteCheckModal creates a modal that checks for Claude CLI and Docker
// createPrerequisiteCheckModal creates the initial prerequisite check modal
func createPrerequisiteCheckModal() *Modal {
	content := `Checking Prerequisites...

• Claude CLI: Checking...
• Docker: Checking...

Please wait while we verify your system requirements.`

	modal := &Modal{
		Type:       ModalInfo,
		Title:      "System Requirements",
		Content:    content,
		Width:      70,
		DisableEsc: true, // Disable Esc during wizard
		Actions:    []ModalAction{},
	}

	return modal
}

// checkPrerequisites returns a command that performs prerequisite checks asynchronously
func checkPrerequisites() tea.Cmd {
	return func() tea.Msg {
		result := prerequisiteCheckResult{}

		// Check Claude CLI using shared utility
		result.claudeAvailable, result.claudeMessage = system.IsClaudeAvailable()

		// Check Docker using shared utility
		result.dockerAvailable, result.dockerMessage = system.IsDockerAvailable()

		return result
	}
}

// createWizardWelcomeModal creates the welcome screen for the wizard
func createWizardWelcomeModal() *Modal {
	content := `Welcome to Maestro!

Maestro manages isolated Docker containers for Claude Code development.
Each container runs an independent Claude instance with:

  • Its own git branch for clean organization
  • Network firewall for security
  • Isolated environment and dependencies

This setup wizard will help you configure:

  1. Authentication with Claude
  2. Network firewall rules
  3. Container resource limits

Step 2 of 6`

	modal := &Modal{
		Type:       ModalInfo,
		Title:      "Welcome to Maestro",
		Content:    content,
		Width:      70,
		DisableEsc: true, // Disable Esc during wizard
		Actions: []ModalAction{
			{Label: "Get Started", Key: "enter", IsPrimary: true},
			{Label: "Skip Wizard", Key: "s", IsPrimary: false},
		},
	}

	// Get Started advances to next step
	modal.Actions[0].OnSelect = func() tea.Msg {
		return wizardNextStepMsg{}
	}

	// Skip Wizard exits wizard mode
	modal.Actions[1].OnSelect = func() tea.Msg {
		return exitWizardMsg{}
	}

	return modal
}

// createWizardAuthModal creates the authentication screen for the wizard
func (m Model) createWizardAuthModal(hasCredentials bool) *Modal {
	var content string
	if hasCredentials {
		content = `Authentication: ✓ Already configured

Your Claude credentials are already set up and ready to use.

Step 3 of 6`
	} else {
		content = `Authentication: Setup required

Maestro needs to authenticate with Claude to create containers.

At any time you can authenticate by running:

  maestro auth

This will open a browser window to complete OAuth authentication.
After completing authentication, you will need to paste your
authentication code back into the claude window and hit "Enter".

IMPORTANT:
  • After authentication, you'll be asked to confirm
    "Dangerously skip prompts" - click "Yes" to accept. This
    setting only applies to Claude inside containers, not your
    host system.
  • When you're done with Claude setup, type "exit" to leave
    the Claude console and return to this wizard.
  • Depending on your git configuration, you may be prompted to
    setup a github authentication token as well for the gh
    command to work inside containers. You can choose to skip
	this step if you want to ensure that containers cannot
	access your GitHub account (but this may limit functionality).

You can authenticate now or after completing the wizard, although
doing it now is recommended.

Step 3 of 6`
	}

	actions := []ModalAction{
		{Label: "Next", Key: "enter", IsPrimary: true},
		{Label: "Back", Key: "b", IsPrimary: false},
	}

	// Add "Run Auth Now" button if credentials don't exist
	if !hasCredentials {
		actions = append([]ModalAction{
			{Label: "Run Auth Now", Key: "a", IsPrimary: true},
		}, actions...)
	}

	// Create scrollable viewport for content (15 lines visible)
	vp := viewport.New(66, 15) // Width slightly less than modal width for padding
	vp.SetContent(content)

	modal := &Modal{
		Type:        ModalInfo,
		Title:       "Authentication",
		Content:     content, // Store for fallback
		Width:       70,
		viewport:    &vp,
		useViewport: true, // Enable scrolling
		DisableEsc:  true, // Disable Esc during wizard
		Actions:     actions,
	}

	// Wire up button handlers
	if !hasCredentials {
		// Run Auth Now button
		modal.Actions[0].OnSelect = func() tea.Msg {
			// Save config and exit to run auth
			return saveWizardConfigMsg{
				memory:     m.wizardMemory,
				cpus:       m.wizardCPUs,
				domains:    m.wizardDomains,
				runAuthNow: true,
			}
		}
		// Next button
		modal.Actions[1].OnSelect = func() tea.Msg {
			return wizardNextStepMsg{}
		}
		// Back button
		modal.Actions[2].OnSelect = func() tea.Msg {
			return wizardPrevStepMsg{}
		}
	} else {
		// Next button
		modal.Actions[0].OnSelect = func() tea.Msg {
			return wizardNextStepMsg{}
		}
		// Back button
		modal.Actions[1].OnSelect = func() tea.Msg {
			return wizardPrevStepMsg{}
		}
	}

	return modal
}

// createWizardFirewallModal creates the firewall setup screen for the wizard
func (m Model) createWizardFirewallModal() *Modal {
	content := `Network Firewall

Maestro containers use a network firewall to control outbound connections.
Only whitelisted domains can be accessed from within containers.

Common domains (GitHub, NPM, PyPI, etc.) are pre-configured.
You can add more domains later with the Firewall settings (f key).

Step 4 of 6`

	modal := &Modal{
		Type:       ModalInfo,
		Title:      "Firewall Setup",
		Content:    content,
		Width:      70,
		DisableEsc: true, // Disable Esc during wizard
		Actions: []ModalAction{
			{Label: "Next", Key: "enter", IsPrimary: true},
			{Label: "Back", Key: "b", IsPrimary: false},
		},
	}

	// Next button
	modal.Actions[0].OnSelect = func() tea.Msg {
		return wizardNextStepMsg{}
	}

	// Back button
	modal.Actions[1].OnSelect = func() tea.Msg {
		return wizardPrevStepMsg{}
	}

	return modal
}

// createWizardContainerDefaultsModal creates the container defaults screen for the wizard
func (m *Model) createWizardContainerDefaultsModal() *Modal {
	content := fmt.Sprintf(`Container Defaults

Configure default resource limits for containers.

Current settings:
  Memory:  %s
  CPUs:    %s

These settings control how much memory and CPU each container can use.
You can adjust these later in Settings (s key).

Step 5 of 6`, m.wizardMemory, m.wizardCPUs)

	modal := &Modal{
		Type:       ModalInfo,
		Title:      "Container Defaults",
		Content:    content,
		Width:      70,
		DisableEsc: true, // Disable Esc during wizard
		Actions: []ModalAction{
			{Label: "Next", Key: "enter", IsPrimary: true},
			{Label: "Back", Key: "b", IsPrimary: false},
		},
	}

	// Next button
	modal.Actions[0].OnSelect = func() tea.Msg {
		return wizardNextStepMsg{}
	}

	// Back button
	modal.Actions[1].OnSelect = func() tea.Msg {
		return wizardPrevStepMsg{}
	}

	return modal
}

// createWizardCompletionModal creates the completion screen for the wizard
func (m Model) createWizardCompletionModal() *Modal {
	var content strings.Builder

	content.WriteString("Setup Complete! 🎉\n\n")
	content.WriteString("Your configuration:\n\n")
	content.WriteString(fmt.Sprintf("  Memory Limit:  %s\n", m.wizardMemory))
	content.WriteString(fmt.Sprintf("  CPU Limit:     %s\n", m.wizardCPUs))
	content.WriteString(fmt.Sprintf("  Firewall:      %d domains configured\n\n", len(m.wizardDomains)))
	content.WriteString("You're ready to start using Maestro!\n\n")
	content.WriteString("On the main screen, press 'n' to create your first container.\n")
	content.WriteString("Use 's' to adjust settings and 'f' to modify firewall rules.\n\n")
	content.WriteString("Step 6 of 6")

	modal := &Modal{
		Type:       ModalInfo,
		Title:      "Welcome Complete",
		Content:    content.String(),
		Width:      70,
		DisableEsc: true, // Disable Esc during wizard
		Actions: []ModalAction{
			{Label: "Finish", Key: "enter", IsPrimary: true},
			{Label: "Back", Key: "b", IsPrimary: false},
		},
	}

	// Finish button - save config and exit wizard
	modal.Actions[0].OnSelect = func() tea.Msg {
		return saveWizardConfigMsg{
			memory:     m.wizardMemory,
			cpus:       m.wizardCPUs,
			domains:    m.wizardDomains,
			runAuthNow: m.wizardRunAuthNow,
		}
	}

	// Back button
	modal.Actions[1].OnSelect = func() tea.Msg {
		return wizardPrevStepMsg{}
	}

	return modal
}

// getWizardModal returns the appropriate modal for the current wizard step
func (m *Model) getWizardModal() *Modal {
	switch m.wizardStep {
	case 1: // Prerequisite check
		return createPrerequisiteCheckModal()
	case 2: // Welcome
		return createWizardWelcomeModal()
	case 3: // Authentication
		// Check if credentials exist
		hasCredentials := !isFirstRun()
		return m.createWizardAuthModal(hasCredentials)
	case 4: // Firewall
		return m.createWizardFirewallModal()
	case 5: // Container defaults
		return m.createWizardContainerDefaultsModal()
	case 6: // Completion
		return m.createWizardCompletionModal()
	default:
		// Shouldn't happen, but return welcome as fallback
		return createWizardWelcomeModal()
	}
}

// saveWizardConfig saves the wizard configuration to the config file
func (m *Model) saveWizardConfig(msg saveWizardConfigMsg) error {
	// Get config file path
	configPath := paths.ConfigFile()

	// Check if config file exists
	fileExists := false
	if _, err := os.Stat(configPath); err == nil {
		fileExists = true
	}

	if fileExists {
		// Config file exists - update only the wizard keys
		// Re-read the config to ensure we have the latest values
		viper.SetConfigFile(configPath)
		if err := viper.ReadInConfig(); err != nil {
			return fmt.Errorf("failed to read existing config: %w", err)
		}
	}

	// Update only the wizard-specific keys
	viper.Set("containers.resources.memory", msg.memory)
	viper.Set("containers.resources.cpus", msg.cpus)
	viper.Set("firewall.allowed_domains", msg.domains)

	// If running auth now, enable wizard to continue after auth completes
	// (they still need to complete remaining wizard steps: firewall, defaults, completion)
	if msg.runAuthNow {
		viper.Set("wizard.resume_after_auth", true)
	} else {
		// Wizard is completing normally (Finish button) - clear resume flag
		viper.Set("wizard.resume_after_auth", false)
	}

	// Write the config file
	if err := viper.WriteConfig(); err != nil {
		// If WriteConfig fails (file doesn't exist), use WriteConfigAs
		if err := viper.WriteConfigAs(configPath); err != nil {
			return fmt.Errorf("failed to write config file: %w", err)
		}
	}

	return nil
}

// createContainerDetailsModal creates a scrollable modal showing comprehensive container information
func createContainerDetailsModal(details *container.ContainerDetails) *Modal {
	var content strings.Builder

	// Header section
	content.WriteString(fmt.Sprintf("Container: %s\n", details.ShortName))
	content.WriteString(strings.Repeat("─", 96) + "\n\n")

	// Status and Basic Info
	content.WriteString(fmt.Sprintf("Status:       %s\n", details.Status))
	if details.StatusDetails != "" {
		content.WriteString(fmt.Sprintf("Details:      %s\n", details.StatusDetails))
	}
	content.WriteString(fmt.Sprintf("Branch:       %s\n", details.Branch))
	content.WriteString(fmt.Sprintf("Git Status:   %s\n", strings.TrimSpace(details.GitStatus)))
	content.WriteString(fmt.Sprintf("Auth Status:  %s\n", details.AuthStatus))
	content.WriteString(fmt.Sprintf("Last Activity: %s\n", details.LastActivity))
	if details.Uptime != "" {
		content.WriteString(fmt.Sprintf("Uptime:       %s\n", details.Uptime))
	}
	content.WriteString("\n")

	// Resources
	content.WriteString("Resources:\n")
	content.WriteString(strings.Repeat("─", 96) + "\n")
	content.WriteString(fmt.Sprintf("CPUs:         %s\n", details.CPUs))
	content.WriteString(fmt.Sprintf("Memory:       %s\n", details.Memory))
	content.WriteString("\n")

	// Network
	content.WriteString("Network:\n")
	content.WriteString(strings.Repeat("─", 96) + "\n")
	if details.IPAddress != "" {
		content.WriteString(fmt.Sprintf("IP Address:   %s\n", details.IPAddress))
	} else {
		content.WriteString("IP Address:   (none)\n")
	}
	if len(details.Ports) > 0 {
		content.WriteString("Ports:\n")
		for _, port := range details.Ports {
			content.WriteString(fmt.Sprintf("  %s\n", port))
		}
	} else {
		content.WriteString("Ports:        (none)\n")
	}
	content.WriteString("\n")

	// Volumes
	content.WriteString("Volumes:\n")
	content.WriteString(strings.Repeat("─", 96) + "\n")
	if len(details.Volumes) > 0 {
		for _, vol := range details.Volumes {
			content.WriteString(fmt.Sprintf("  %s\n", vol))
		}
	} else {
		content.WriteString("(none)\n")
	}
	content.WriteString("\n")

	// Environment Variables
	content.WriteString("Environment Variables:\n")
	content.WriteString(strings.Repeat("─", 96) + "\n")
	if len(details.Environment) > 0 {
		for _, env := range details.Environment {
			content.WriteString(fmt.Sprintf("  %s\n", env))
		}
	} else {
		content.WriteString("(none)\n")
	}
	content.WriteString("\n")

	// Recent Logs
	content.WriteString("Recent Logs (last 50 lines):\n")
	content.WriteString(strings.Repeat("─", 96) + "\n")
	content.WriteString(details.RecentLogs)

	// Use scrollable info modal with 20 lines visible and 100 character width
	return NewScrollableInfoModalWide("Container Details", content.String(), 20, 100)
}

// createContainerCreateModal creates the interactive form for creating a new container
func createContainerCreateModal() *Modal {
	// Create textarea for task description
	ta := textarea.New()
	ta.Placeholder = "Enter task description..."
	ta.SetWidth(90)
	ta.SetHeight(5)
	ta.Focus()
	ta.CharLimit = 2000
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle() // Remove cursor line highlighting
	ta.FocusedStyle.Base = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(style.OceanTide)
	ta.BlurredStyle.Base = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(style.DimGray)
	ta.Cursor.Style = lipgloss.NewStyle().Foreground(style.OceanSurge)

	// Create text input for branch name
	ti := textinput.New()
	ti.Placeholder = "(auto-generated from description)"
	ti.Width = 90
	ti.CharLimit = 100
	// Focused styles
	ti.PromptStyle = lipgloss.NewStyle().Foreground(style.OceanTide)
	ti.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(style.OceanSurge)
	// Note: textinput doesn't have BlurredStyle, we'll handle prompt color in the blur/focus methods

	modal := &Modal{
		Type:         ModalForm,
		Title:        "Create New Container",
		Width:        100,
		Height:       30,
		textarea:     &ta,
		textinputs:   []textinput.Model{ti},
		checkboxes:   []bool{false, false}, // [0]=no-connect, [1]=exact
		focusedField: 0,                    // Start with textarea focused
		fieldLabels: []string{
			"Task Description:",
			"Branch Name:",
			"Return to TUI after creation (--no-connect)",
			"Exact prompt (don't preprocess with AI)",
		},
		Actions: []ModalAction{
			{Label: "Create", Key: "ctrl+s", IsPrimary: true},
			{Label: "Cancel", Key: "esc", IsPrimary: false},
		},
	}

	// Set OnSelect handler after modal is created (to avoid closure issues)
	modal.Actions[0].OnSelect = func() tea.Msg {
		// Extract form values and create message
		taskDesc := ""
		if modal.textarea != nil {
			taskDesc = modal.textarea.Value()
		}

		branchName := ""
		if len(modal.textinputs) > 0 {
			branchName = modal.textinputs[0].Value()
		}

		noConnect := false
		exact := false
		if len(modal.checkboxes) > 0 {
			noConnect = modal.checkboxes[0]
		}
		if len(modal.checkboxes) > 1 {
			exact = modal.checkboxes[1]
		}

		return createContainerMsg{
			taskDescription: taskDesc,
			branchName:      branchName,
			noConnect:       noConnect,
			exact:           exact,
		}
	}

	return modal
}

// createSettingsModal creates the settings configuration modal
func createSettingsModal() *Modal {
	// Load current settings from viper
	memory := viper.GetString("containers.resources.memory")
	cpus := viper.GetString("containers.resources.cpus")
	showNag := viper.GetBool("daemon.show_nag")
	autoRefreshTokens := viper.GetBool("daemon.token_refresh.enabled")
	enableNotifications := viper.GetBool("daemon.notifications.enabled")

	// Create text input for memory
	memoryInput := textinput.New()
	memoryInput.Placeholder = "e.g., 4g, 8g"
	memoryInput.SetValue(memory)
	memoryInput.Width = 90
	memoryInput.CharLimit = 10
	memoryInput.PromptStyle = lipgloss.NewStyle().Foreground(style.OceanTide)
	memoryInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	memoryInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	memoryInput.Cursor.Style = lipgloss.NewStyle().Foreground(style.OceanSurge)
	memoryInput.Focus()

	// Create text input for CPUs
	cpusInput := textinput.New()
	cpusInput.Placeholder = "e.g., 1, 2, 4"
	cpusInput.SetValue(cpus)
	cpusInput.Width = 90
	cpusInput.CharLimit = 5
	cpusInput.PromptStyle = lipgloss.NewStyle().Foreground(style.DimGray)
	cpusInput.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	cpusInput.PlaceholderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cpusInput.Cursor.Style = lipgloss.NewStyle().Foreground(style.OceanSurge)

	modal := &Modal{
		Type:         ModalForm,
		Title:        "Settings",
		Width:        100,
		Height:       25,
		textinputs:   []textinput.Model{memoryInput, cpusInput},
		checkboxes:   []bool{showNag, autoRefreshTokens, enableNotifications},
		focusedField: 0,
		fieldLabels: []string{
			"Memory Limit (for new containers):",
			"CPU Limit (for new containers):",
			"Show daemon startup reminder",
			"Auto-refresh authentication tokens",
			"Enable desktop notifications",
		},
		Actions: []ModalAction{
			{Label: "Save", Key: "ctrl+s", IsPrimary: true},
			{Label: "Cancel", Key: "esc", IsPrimary: false},
		},
	}

	// Set OnSelect handler for Save button
	modal.Actions[0].OnSelect = func() tea.Msg {
		memory := ""
		cpus := ""
		showNag := false
		autoRefresh := false
		enableNotif := false

		if len(modal.textinputs) > 0 {
			memory = modal.textinputs[0].Value()
		}
		if len(modal.textinputs) > 1 {
			cpus = modal.textinputs[1].Value()
		}
		if len(modal.checkboxes) > 0 {
			showNag = modal.checkboxes[0]
		}
		if len(modal.checkboxes) > 1 {
			autoRefresh = modal.checkboxes[1]
		}
		if len(modal.checkboxes) > 2 {
			enableNotif = modal.checkboxes[2]
		}

		return saveSettingsMsg{
			memory:              memory,
			cpus:                cpus,
			showNag:             showNag,
			autoRefreshTokens:   autoRefresh,
			enableNotifications: enableNotif,
		}
	}

	return modal
}

// createFirewallModal creates the firewall domain management modal
func createFirewallModal() *Modal {
	// Load current domains from viper
	domains := viper.GetStringSlice("firewall.allowed_domains")

	// Create textarea with all domains (one per line)
	domainsText := strings.Join(domains, "\n")
	ta := textarea.New()
	ta.Placeholder = "Enter domains, one per line (e.g., github.com)"
	ta.SetValue(domainsText)
	ta.SetWidth(90)
	ta.SetHeight(12)
	ta.Focus()
	ta.CharLimit = 5000
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle() // Remove cursor line highlighting
	ta.FocusedStyle.Base = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(style.OceanTide)
	ta.BlurredStyle.Base = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(style.DimGray)
	ta.Cursor.Style = lipgloss.NewStyle().Foreground(style.OceanSurge)

	modal := &Modal{
		Type:         ModalForm,
		Title:        "Firewall Configuration",
		Width:        100,
		Height:       30,
		textarea:     &ta,
		textinputs:   []textinput.Model{},
		checkboxes:   []bool{false}, // Apply to running containers
		focusedField: 0,
		fieldLabels: []string{
			"Allowed Domains (one per line):",
			"Apply changes to running containers",
		},
		Actions: []ModalAction{
			{Label: "Save", Key: "ctrl+s", IsPrimary: true},
			{Label: "Cancel", Key: "esc", IsPrimary: false},
		},
	}

	// Set OnSelect handler for Save button
	modal.Actions[0].OnSelect = func() tea.Msg {
		domainsText := ""
		applyToRunning := false

		if modal.textarea != nil {
			domainsText = modal.textarea.Value()
		}
		if len(modal.checkboxes) > 0 {
			applyToRunning = modal.checkboxes[0]
		}

		return saveFirewallMsg{
			domainsText:    domainsText,
			applyToRunning: applyToRunning,
		}
	}

	return modal
}

// createActionsModal creates the container actions menu modal
func createActionsModal(containerInfo container.Info) *Modal {
	content := "Select an action for: " + containerInfo.ShortName

	return &Modal{
		Type:    ModalActions,
		Title:   "Container Actions",
		Content: content,
		Width:   90,
		Actions: []ModalAction{
			{
				Label:     "Connect",
				Key:       "c",
				IsPrimary: true,
				OnSelect: func() tea.Msg {
					return views.ConnectRequestMsg{ContainerName: containerInfo.Name}
				},
			},
			{
				Label:     "Stop",
				Key:       "s",
				IsPrimary: false,
				OnSelect: func() tea.Msg {
					return ContainerActionMsg{Action: container.OperationStop, ContainerName: containerInfo.Name}
				},
			},
			{
				Label:     "Restart",
				Key:       "r",
				IsPrimary: false,
				OnSelect: func() tea.Msg {
					return ContainerActionMsg{Action: container.OperationRestart, ContainerName: containerInfo.Name}
				},
			},
			{
				Label:     "Delete",
				Key:       "d",
				IsPrimary: false,
				OnSelect: func() tea.Msg {
					return ContainerActionMsg{Action: container.OperationDelete, ContainerName: containerInfo.Name}
				},
			},
			{
				Label:     "Refresh Tokens",
				Key:       "t",
				IsPrimary: false,
				OnSelect: func() tea.Msg {
					return ContainerActionMsg{Action: container.OperationRefreshTokens, ContainerName: containerInfo.Name}
				},
			},
			{
				Label:     "Cancel",
				Key:       "esc",
				IsPrimary: false,
				OnSelect:  nil, // Just dismisses
			},
		},
		SelectedAction: 0,
	}
}

// ContainerActionMsg signals a container action should be performed
type ContainerActionMsg struct {
	Action        container.OperationType
	ContainerName string
}

// handleContainerAction processes container action requests
func (m Model) handleContainerAction(msg ContainerActionMsg) (tea.Model, tea.Cmd) {
	switch msg.Action {
	case container.OperationStop, container.OperationDelete:
		// Destructive actions - show confirmation
		actionVerb := string(msg.Action)
		if msg.Action == container.OperationDelete {
			actionVerb = "remove"
		}

		// Store action info for confirmation callback
		action := msg.Action
		containerName := msg.ContainerName

		m.modal = NewConfirmModal(
			"Confirm "+strings.Title(string(msg.Action)),
			fmt.Sprintf("Are you sure you want to %s container '%s'?", actionVerb, msg.ContainerName),
			func() tea.Msg {
				return ConfirmActionMsg{
					Action:        action,
					ContainerName: containerName,
				}
			},
			nil, // OnCancel just dismisses
		)
		return m, nil

	case container.OperationRestart:
		// Mark operation in progress and update status
		m.operationInProgress = true
		m.operationStatus = "Restarting..."

		// Show info toast and perform restart asynchronously
		toastCmd := m.alert.NewAlertCmd("Info", fmt.Sprintf("Restarting container %s...", msg.ContainerName))
		operationCmd := m.performDockerOperation(msg.Action, msg.ContainerName)
		return m, tea.Batch(toastCmd, operationCmd, m.operationSpinner.Tick)

	case container.OperationRefreshTokens:
		// Mark operation in progress and update status
		m.operationInProgress = true
		m.operationStatus = "Refreshing tokens..."

		// Show info toast and perform token refresh asynchronously
		toastCmd := m.alert.NewAlertCmd("Info", fmt.Sprintf("Refreshing tokens for %s...", msg.ContainerName))
		operationCmd := m.performDockerOperation(msg.Action, msg.ContainerName)
		return m, tea.Batch(toastCmd, operationCmd, m.operationSpinner.Tick)

	default:
		m.modal = NewErrorModal("Error", "Unknown action: "+string(msg.Action))
		return m, nil
	}
}

// ConfirmActionMsg signals that a confirmed action should be executed
type ConfirmActionMsg struct {
	Action        container.OperationType
	ContainerName string
}

// performDockerOperation executes a Docker operation asynchronously
func (m Model) performDockerOperation(action container.OperationType, containerName string) tea.Cmd {
	return func() tea.Msg {
		var err error

		switch action {
		case container.OperationStop:
			err = container.StopContainer(containerName)
		case container.OperationRestart:
			err = container.RestartContainer(containerName)
		case container.OperationDelete:
			err = container.DeleteContainer(containerName)
		case container.OperationRefreshTokens:
			err = container.RefreshTokens(containerName)
		default:
			err = fmt.Errorf("unknown operation: %s", action)
		}

		return dockerOperationResult{
			action:        action,
			containerName: containerName,
			success:       err == nil,
			err:           err,
		}
	}
}

// getActiveKeys returns the appropriate keybindings based on current state
func (m Model) getActiveKeys() keyMap {
	if m.modal == nil {
		// Normal view keybindings
		return m.keys
	}

	// Modal is active - show modal-specific keybindings
	modalKeys := keyMap{}

	// Add action-specific shortcuts from the modal
	if m.modal.Actions != nil && len(m.modal.Actions) > 0 {
		actionBindings := []*key.Binding{
			&modalKeys.ModalAction1,
			&modalKeys.ModalAction2,
			&modalKeys.ModalAction3,
			&modalKeys.ModalAction4,
			&modalKeys.ModalAction5,
		}

		for i, action := range m.modal.Actions {
			if i >= len(actionBindings) {
				break
			}
			if action.Key != "" && action.Key != "enter" && action.Key != "esc" {
				*actionBindings[i] = key.NewBinding(
					key.WithKeys(action.Key),
					key.WithHelp(action.Key, action.Label),
				)
			} else {
				// Disable this binding slot
				*actionBindings[i] = key.NewBinding(key.WithDisabled())
			}
		}

		// Navigation keys
		if len(m.modal.Actions) > 1 {
			modalKeys.ModalNavigate = key.NewBinding(
				key.WithKeys("left", "right", "h", "l", "tab"),
				key.WithHelp("←/→", "navigate"),
			)
		} else {
			modalKeys.ModalNavigate = key.NewBinding(key.WithDisabled())
		}

		modalKeys.ModalSelect = key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("↵", "select"),
		)
	}

	modalKeys.ModalClose = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "close"),
	)

	// Copy Quit key from main keys (for wizard mode help display)
	modalKeys.Quit = m.keys.Quit

	return modalKeys
}

// ShortHelp for modal keys
func (k keyMap) modalShortHelp() []key.Binding {
	bindings := []key.Binding{}
	// Add enabled action keys
	for _, b := range []key.Binding{k.ModalAction1, k.ModalAction2, k.ModalAction3, k.ModalAction4, k.ModalAction5} {
		if b.Enabled() {
			bindings = append(bindings, b)
		}
	}
	// Add navigation and control keys
	if k.ModalNavigate.Enabled() {
		bindings = append(bindings, k.ModalNavigate)
	}
	if k.ModalSelect.Enabled() {
		bindings = append(bindings, k.ModalSelect)
	}
	bindings = append(bindings, k.ModalClose)
	return bindings
}

// ShortHelp for wizard modal keys (includes quit)
func (k keyMap) wizardModalShortHelp() []key.Binding {
	bindings := k.modalShortHelp()
	// Add quit key for wizard modals only
	bindings = append(bindings, k.Quit)
	return bindings
}

// View renders the current state
// rgbColor represents an RGB color
type rgbColor struct {
	r, g, b int
}

// interpolateColor interpolates between two RGB colors
func interpolateColor(c1, c2 rgbColor, t float64) rgbColor {
	return rgbColor{
		r: int(float64(c1.r) + (float64(c2.r)-float64(c1.r))*t),
		g: int(float64(c1.g) + (float64(c2.g)-float64(c1.g))*t),
		b: int(float64(c1.b) + (float64(c2.b)-float64(c1.b))*t),
	}
}

// toANSI256 converts RGB to lipgloss color
func (c rgbColor) toANSI256() lipgloss.Color {
	return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", c.r, c.g, c.b))
}

// renderTitleBanner creates the ASCII art title with horizontal smooth gradient
func (m Model) renderTitleBanner() string {
	banner := []string{
		"░  ░░░░  ░░░      ░░░        ░░░      ░░░        ░░       ░░░░      ░░",
		"▒   ▒▒   ▒▒  ▒▒▒▒  ▒▒  ▒▒▒▒▒▒▒▒  ▒▒▒▒▒▒▒▒▒▒▒  ▒▒▒▒▒  ▒▒▒▒  ▒▒  ▒▒▒▒  ▒",
		"▓        ▓▓  ▓▓▓▓  ▓▓      ▓▓▓▓▓      ▓▓▓▓▓▓  ▓▓▓▓▓       ▓▓▓  ▓▓▓▓  ▓",
		"█  █  █  ██        ██  ██████████████  █████  █████  ███  ███  ████  █",
		"█  ████  ██  ████  ██        ███      ██████  █████  ████  ███      ██",
	}

	// Define gradient stops (left to right)
	// Use intermediate colors to avoid muddy transitions
	stops := []struct {
		position float64
		color    rgbColor
	}{
		{0.0, rgbColor{112, 56, 152}}, // PurpleHaze #703898
		{0.33, rgbColor{0, 150, 180}}, // Blue-Cyan (between purple and cyan)
		{0.66, rgbColor{0, 188, 212}}, // OceanTide #00BCD4
		{1.0, rgbColor{252, 196, 81}}, // SunsetGlow #FCC451
	}

	// Find the maximum line length
	maxLen := 0
	for _, line := range banner {
		lineLen := len([]rune(line))
		if lineLen > maxLen {
			maxLen = lineLen
		}
	}

	var renderedLines []string
	for _, line := range banner {
		// Apply horizontal gradient left to right
		var coloredLine strings.Builder

		// Calculate where this line will start when centered
		leftPadding := (m.width - maxLen) / 2
		if leftPadding < 0 {
			leftPadding = 0
		}

		for i, char := range line {
			// Calculate gradient position based on SCREEN position (after centering)
			screenPosition := leftPadding + i
			position := float64(screenPosition) / float64(m.width-1)

			// Clamp position to [0, 1] range
			if position < 0 {
				position = 0
			} else if position > 1 {
				position = 1
			}

			// Find which two stops we're between
			var c1, c2 rgbColor
			var t float64
			found := false

			for j := 0; j < len(stops)-1; j++ {
				if position >= stops[j].position && position <= stops[j+1].position {
					c1 = stops[j].color
					c2 = stops[j+1].color
					segmentLength := stops[j+1].position - stops[j].position
					if segmentLength > 0 {
						t = (position - stops[j].position) / segmentLength
					}
					found = true
					break
				}
			}

			// Fallback: if position doesn't fall in any range, use last color
			if !found {
				c1 = stops[len(stops)-1].color
				c2 = stops[len(stops)-1].color
				t = 0
			}

			// Interpolate the color
			interpolated := interpolateColor(c1, c2, t)

			// Apply color to character
			styled := lipgloss.NewStyle().Foreground(interpolated.toANSI256()).Render(string(char))
			coloredLine.WriteString(styled)
		}

		// Center the line
		centeredLine := lipgloss.Place(m.width, 1, lipgloss.Center, lipgloss.Center, coloredLine.String())
		renderedLines = append(renderedLines, centeredLine)
	}

	// Add empty line for breathing space
	renderedLines = append(renderedLines, "")

	return strings.Join(renderedLines, "\n")
}

// renderWizardAnimation renders the opening animation (column-by-column reveal)
func (m Model) renderWizardAnimation() string {
	banner := []string{
		"░  ░░░░  ░░░      ░░░        ░░░      ░░░        ░░       ░░░░      ░░",
		"▒   ▒▒   ▒▒  ▒▒▒▒  ▒▒  ▒▒▒▒▒▒▒▒  ▒▒▒▒▒▒▒▒▒▒▒  ▒▒▒▒▒  ▒▒▒▒  ▒▒  ▒▒▒▒  ▒",
		"▓        ▓▓  ▓▓▓▓  ▓▓      ▓▓▓▓▓      ▓▓▓▓▓▓  ▓▓▓▓▓       ▓▓▓  ▓▓▓▓  ▓",
		"█  █  █  ██        ██  ██████████████  █████  █████  ███  ███  ████  █",
		"█  ████  ██  ████  ██        ███      ██████  █████  ████  ███      ██",
	}

	// Same gradient as normal title
	stops := []struct {
		position float64
		color    rgbColor
	}{
		{0.0, rgbColor{112, 56, 152}},
		{0.33, rgbColor{0, 150, 180}},
		{0.66, rgbColor{0, 188, 212}},
		{1.0, rgbColor{252, 196, 81}},
	}

	maxLen := 0
	for _, line := range banner {
		lineLen := len([]rune(line))
		if lineLen > maxLen {
			maxLen = lineLen
		}
	}

	var renderedLines []string
	for _, line := range banner {
		runes := []rune(line)
		// Truncate to current animation column
		visibleLen := m.animationColumn
		if visibleLen > len(runes) {
			visibleLen = len(runes)
		}
		visibleLine := string(runes[:visibleLen])

		// Apply gradient
		var coloredLine strings.Builder
		leftPadding := (m.width - maxLen) / 2
		if leftPadding < 0 {
			leftPadding = 0
		}

		for i, char := range visibleLine {
			screenPosition := leftPadding + i
			position := float64(screenPosition) / float64(m.width-1)
			if position < 0 {
				position = 0
			} else if position > 1 {
				position = 1
			}

			var c1, c2 rgbColor
			var t float64
			for j := 0; j < len(stops)-1; j++ {
				if position >= stops[j].position && position <= stops[j+1].position {
					c1 = stops[j].color
					c2 = stops[j+1].color
					segmentLength := stops[j+1].position - stops[j].position
					if segmentLength > 0 {
						t = (position - stops[j].position) / segmentLength
					}
					break
				}
			}

			r := uint8(float64(c1.r) + t*float64(c2.r-c1.r))
			g := uint8(float64(c1.g) + t*float64(c2.g-c1.g))
			b := uint8(float64(c1.b) + t*float64(c2.b-c1.b))

			coloredLine.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(fmt.Sprintf("#%02x%02x%02x", r, g, b))).Render(string(char)))
		}

		renderedLines = append(renderedLines, coloredLine.String())
	}

	// Center the entire banner vertically and horizontally
	bannerText := strings.Join(renderedLines, "\n")

	// If animation complete, show help text
	var helpText string
	if m.animationComplete {
		helpText = lipgloss.NewStyle().
			Foreground(style.OceanTide).
			Render("↵ begin")
	}

	// Combine banner and help
	var fullView string
	if helpText != "" {
		fullView = bannerText + "\n\n\n" + helpText
	} else {
		fullView = bannerText
	}

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		fullView,
	)
}

// updateStatusBar refreshes the statusbar content with manual background styling
func (m *Model) updateStatusBar() {
	// Column 1: Daemon status + Container count (DeepSpace background)
	var daemonIndicator string
	if m.daemonRunning {
		// Animate daemon indicator with ping-pong effect using pure greens from xterm-256 palette
		// 16 distinct colors: 0→15→0 = 30 frame cycle @ 750ms = 22.5s full cycle (very subtle)
		numShades := 16
		cycleLength := (numShades - 1) * 2 // 30 frames per cycle
		step := m.animationFrame % cycleLength
		var shade int
		if step < numShades-1 {
			shade = step
		} else {
			shade = cycleLength - step
		}
		daemonColor := style.GetDaemonShade(shade)
		daemonIndicator = lipgloss.NewStyle().Foreground(daemonColor).Render("●")
	} else {
		daemonIndicator = "○" // Not running
	}
	containerText := fmt.Sprintf("%d containers", m.containerCount)
	if m.containerCount == 1 {
		containerText = "1 container"
	}
	col1Text := fmt.Sprintf("%s %s", daemonIndicator, containerText)
	col1 := lipgloss.NewStyle().
		Foreground(style.GhostWhite).
		Background(style.DeepSpace).
		Render(col1Text)

	// Column 2: Current path (DimGray background)
	pathText := m.workingDir
	maxPathLen := 40
	if len(pathText) > maxPathLen {
		// Truncate from the middle, keeping start and end
		pathText = pathText[:15] + "..." + pathText[len(pathText)-22:]
	}
	col2 := lipgloss.NewStyle().
		Foreground(style.GhostWhite).
		Background(style.DimGray).
		Render(pathText)

	// Column 3: Operation status with spinner if operation in progress (PurpleHaze background)
	var col3 string
	if m.operationInProgress {
		// Style both spinner and text with matching background
		spinnerPart := m.operationSpinner.View()
		textPart := lipgloss.NewStyle().
			Foreground(style.GhostWhite).
			Background(style.PurpleHaze).
			Render(" " + m.operationStatus)
		col3 = spinnerPart + textPart
	} else {
		col3 = lipgloss.NewStyle().
			Foreground(style.GhostWhite).
			Background(style.PurpleHaze).
			Render(m.operationStatus)
	}

	// Column 4: Time + Mode indicator (OceanAbyss background)
	timeText := time.Now().Format("15:04")
	modeIndicator := "●" // Normal mode
	if m.modal != nil {
		modeIndicator = "◆" // Modal active
	}
	col4Text := fmt.Sprintf("%s %s", timeText, modeIndicator)
	col4 := lipgloss.NewStyle().
		Foreground(style.GhostWhite).
		Background(style.OceanAbyss).
		Render(col4Text)

	m.statusbar.SetContent(col1, col2, col3, col4)
}

func (m Model) View() string {
	// Wizard mode: Show opening animation
	if m.wizardMode && m.wizardStep == 0 {
		return m.renderWizardAnimation()
	}

	// Wizard mode with modal screens: Show blank view with modal overlay and help
	if m.wizardMode && m.wizardStep > 0 {
		// If modal not created yet (waiting for WindowSizeMsg), show blank screen
		if m.modal == nil {
			return ""
		}

		// Render blank background with modal on top
		blankView := ""
		var mainView string
		// Reserve space for help at bottom (1 line help + 1 blank line = 2 lines)
		// Ensure height is positive to avoid panic
		modalHeight := m.height - 2
		if modalHeight < 10 {
			modalHeight = 10 // Minimum height to prevent panic
		}
		mainView = m.modal.RenderWithBackground(blankView, m.width, modalHeight)

		// Render help bar for wizard (show modal navigation keys including quit)
		activeKeys := m.getActiveKeys()
		helpView := m.help.ShortHelpView(activeKeys.wizardModalShortHelp())

		// Combine main view with help
		finalView := mainView + "\n" + helpView + "\n"

		// Overlay toasts on top of everything (always on top)
		return m.alert.Render(finalView)
	}

	if !m.ready || m.homeView == nil {
		// Show spinner while loading
		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			m.spinner.View()+" Loading containers...",
		)
	}

	// Render title banner
	titleBanner := m.renderTitleBanner()

	baseView := m.homeView.View()

	// Combine title and main view for modal background
	combinedView := titleBanner + "\n" + baseView

	// If modal is active, render it on top (not including help, blank line, and statusbar)
	var mainView string
	if m.modal != nil {
		mainView = m.modal.RenderWithBackground(combinedView, m.width, m.height-3)
	} else {
		mainView = combinedView
	}

	// Render help bar (always visible, not dimmed by modal)
	activeKeys := m.getActiveKeys()
	var helpView string
	if m.modal != nil {
		// Modal help - check if modal provides context-specific help
		contextHelp := m.modal.GetContextHelp()
		if contextHelp != nil {
			// Use modal's context-specific help
			helpView = m.help.ShortHelpView(contextHelp)
		} else {
			// Use default modal help
			helpView = m.help.ShortHelpView(activeKeys.modalShortHelp())
		}
	} else {
		// Normal help
		helpView = m.help.View(activeKeys)
	}

	// Update and render statusbar at bottom (always visible, not dimmed by modal)
	// Create a mutable copy for updateStatusBar
	mCopy := m
	mCopy.updateStatusBar()
	mCopy.statusbar.SetSize(m.width)
	statusView := mCopy.statusbar.View()

	// Combine main view with help and statusbar
	// Layout: Title → Content → Help → (blank line) → Statusbar
	finalView := mainView + "\n" + helpView + "\n\n" + statusView

	// Overlay toasts on top of everything (always on top)
	return m.alert.Render(finalView)
}

// GetResult returns the TUI result after it exits
func (m Model) GetResult() *TUIResult {
	return m.result
}
