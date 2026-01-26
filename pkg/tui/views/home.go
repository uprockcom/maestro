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

package views

import (
	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/uprockcom/maestro/pkg/container"
	"github.com/uprockcom/maestro/pkg/tui/style"
)

// Column configuration for dynamic sizing
type columnConfig struct {
	title    string
	baseSize int // Base width used to calculate proportions
	minSize  int // Minimum width for this column
}

// Maximum table width before centering kicks in
const maxTableWidth = 160

// getColumnConfigs returns column definitions based on whether AWS auth is used
func getColumnConfigs(useAWSAuth bool) []columnConfig {
	configs := []columnConfig{
		{title: "NAME", baseSize: 25, minSize: 15},
		{title: "STATUS", baseSize: 14, minSize: 12},
		{title: "BRANCH", baseSize: 25, minSize: 15},
		{title: "GIT", baseSize: 10, minSize: 8},
		{title: "ACTIVITY", baseSize: 12, minSize: 10},
	}
	// Only show AUTH column when not using AWS/Bedrock auth
	if !useAWSAuth {
		configs = append(configs, columnConfig{title: "AUTH", baseSize: 12, minSize: 10})
	}
	configs = append(configs, columnConfig{title: "CREATED", baseSize: 12, minSize: 10})
	return configs
}

// getTotalBaseWidth calculates total base width for the given column configs
func getTotalBaseWidth(configs []columnConfig) int {
	total := 0
	for _, c := range configs {
		total += c.baseSize
	}
	return total
}

// HomeModel is the main container list view
type HomeModel struct {
	table         table.Model
	width         int
	height        int
	animState     int
	containers    []container.Info
	daemonRunning bool
	useAWSAuth    bool // Whether AWS/Bedrock auth is being used (hides AUTH column)
}

// calculateColumnWidths returns column widths scaled to fit the given width
func calculateColumnWidths(availableWidth int, useAWSAuth bool) []table.Column {
	columnConfigs := getColumnConfigs(useAWSAuth)
	totalBaseWidth := getTotalBaseWidth(columnConfigs)

	// Account for table borders and padding (roughly 4 chars for borders + spacing)
	usableWidth := availableWidth - 4
	if usableWidth < totalBaseWidth {
		usableWidth = totalBaseWidth
	}

	columns := make([]table.Column, len(columnConfigs))
	remainingWidth := usableWidth

	// First pass: calculate proportional widths, respecting minimums
	for i, cfg := range columnConfigs {
		// Calculate proportional width
		proportionalWidth := (cfg.baseSize * usableWidth) / totalBaseWidth

		// Ensure minimum width
		if proportionalWidth < cfg.minSize {
			proportionalWidth = cfg.minSize
		}

		columns[i] = table.Column{
			Title: cfg.title,
			Width: proportionalWidth,
		}
		remainingWidth -= proportionalWidth
	}

	// Distribute any remaining width to the expandable columns (NAME and BRANCH)
	if remainingWidth > 0 {
		expandableIndices := []int{0, 2} // NAME and BRANCH
		extraPerColumn := remainingWidth / len(expandableIndices)
		for _, idx := range expandableIndices {
			columns[idx].Width += extraPerColumn
		}
	}

	return columns
}

// NewHomeModel creates a new home view
func NewHomeModel(containers []container.Info, daemonRunning bool, useAWSAuth bool) *HomeModel {
	columnConfigs := getColumnConfigs(useAWSAuth)
	totalBaseWidth := getTotalBaseWidth(columnConfigs)

	// Start with base column widths
	columns := calculateColumnWidths(totalBaseWidth, useAWSAuth)

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(10),
	)

	// Custom styles with Ocean Tide colors
	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(style.PurpleHaze).
		BorderBottom(true).
		Bold(true).
		Foreground(style.OceanTide)

	s.Selected = s.Selected.
		Foreground(style.GhostWhite).
		Background(lipgloss.Color("237")).
		Bold(false)

	t.SetStyles(s)

	h := &HomeModel{
		table:         t,
		containers:    containers,
		daemonRunning: daemonRunning,
		useAWSAuth:    useAWSAuth,
	}

	h.updateTableRows()
	return h
}

// Init initializes the home view
func (h *HomeModel) Init() tea.Cmd {
	return nil
}

// Update handles input and state changes
func (h *HomeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return h, tea.Quit
		case "enter":
			// Get selected container
			if len(h.containers) > 0 {
				selectedIdx := h.table.Cursor()
				if selectedIdx >= 0 && selectedIdx < len(h.containers) {
					selected := h.containers[selectedIdx]
					// Return a message to signal connection request
					return h, func() tea.Msg {
						return ConnectRequestMsg{ContainerName: selected.Name}
					}
				}
			}
			return h, nil
		case "a":
			// Show actions menu for selected container
			if len(h.containers) > 0 {
				selectedIdx := h.table.Cursor()
				if selectedIdx >= 0 && selectedIdx < len(h.containers) {
					selected := h.containers[selectedIdx]
					return h, func() tea.Msg {
						return ShowActionsMenuMsg{Container: selected}
					}
				}
			}
			return h, nil
		case "up", "k":
			h.table, cmd = h.table.Update(msg)
			return h, cmd
		case "down", "j":
			h.table, cmd = h.table.Update(msg)
			return h, cmd
		}
	}

	h.table, cmd = h.table.Update(msg)
	return h, cmd
}

// ConnectRequestMsg signals that the user wants to connect to a container
type ConnectRequestMsg struct {
	ContainerName string
}

// ShowActionsMenuMsg signals to show the actions menu for a container
type ShowActionsMenuMsg struct {
	Container container.Info
}

// View renders the home view
func (h *HomeModel) View() string {
	// Container table
	tableView := h.table.View()

	// Center the table horizontally
	return lipgloss.Place(
		h.width,
		h.height,
		lipgloss.Center,
		lipgloss.Top,
		tableView,
	)
}

// SetSize updates the view dimensions
func (h *HomeModel) SetSize(width, height int) {
	h.width = width
	h.height = height

	// Adjust table height to fill screen
	// Title (1) + empty (1) + empty (1) + help bar (1) = 4 lines overhead
	tableHeight := height - 4
	if tableHeight < 5 {
		tableHeight = 5
	}
	// Don't limit by container count - let table scroll if needed
	h.table.SetHeight(tableHeight)

	// Calculate effective table width (capped at max)
	effectiveWidth := width
	if effectiveWidth > maxTableWidth {
		effectiveWidth = maxTableWidth
	}

	// Update column widths proportionally
	columns := calculateColumnWidths(effectiveWidth, h.useAWSAuth)
	h.table.SetColumns(columns)

	// Only set table viewport width if we're filling the space
	// When viewport > max, don't set width so lipgloss.Place can center
	if width <= maxTableWidth {
		h.table.SetWidth(width)
	} else {
		h.table.SetWidth(maxTableWidth)
	}
}

// SetAnimationState updates the animation state for pulsing indicators
func (h *HomeModel) SetAnimationState(state int) {
	h.animState = state
}

// RefreshContainers updates the container list
func (h *HomeModel) RefreshContainers(containers []container.Info, daemonRunning bool) {
	h.containers = containers
	h.daemonRunning = daemonRunning
	h.updateTableRows()
}

// updateTableRows converts container data to table rows
func (h *HomeModel) updateTableRows() {
	rows := make([]table.Row, 0, len(h.containers))

	for _, c := range h.containers {
		row := table.Row{
			h.formatName(c),
			h.formatStatus(c),
			h.formatBranch(c),
			h.formatGit(c),
			h.formatActivity(c),
		}
		// Only include AUTH column when not using AWS auth
		if !h.useAWSAuth {
			row = append(row, h.formatAuth(c))
		}
		row = append(row, h.formatCreated(c))
		rows = append(rows, row)
	}

	h.table.SetRows(rows)
}

// formatName returns the container short name
func (h *HomeModel) formatName(c container.Info) string {
	return c.ShortName
}

// formatStatus returns the status indicator
// Using plain text without colors to avoid ANSI bleeding issues in the table
func (h *HomeModel) formatStatus(c container.Info) string {
	switch c.Status {
	case "running":
		if c.NeedsAttention {
			return "⚠ Waiting"
		}
		return "● Running"
	case "exited":
		return "○ Stopped"
	default:
		return "? " + c.Status
	}
}

// formatBranch returns the branch name
func (h *HomeModel) formatBranch(c container.Info) string {
	if c.Branch == "" {
		return "—"
	}
	return c.Branch
}

// formatGit returns git status
func (h *HomeModel) formatGit(c container.Info) string {
	if c.GitStatus == "" {
		return "—"
	}
	return c.GitStatus
}

// formatActivity returns time since last activity
func (h *HomeModel) formatActivity(c container.Info) string {
	if c.LastActivity == "" {
		return "—"
	}
	return c.LastActivity
}

// formatAuth returns authentication status
func (h *HomeModel) formatAuth(c container.Info) string {
	if c.AuthStatus == "" {
		return "—"
	}
	return c.AuthStatus
}

// formatCreated returns when the container was created
func (h *HomeModel) formatCreated(c container.Info) string {
	if c.CreatedAt.IsZero() {
		return "—"
	}
	return c.CreatedAt.Format("Jan 2 15:04")
}

// GetContainers returns the current container list for caching
func (h *HomeModel) GetContainers() []container.Info {
	return h.containers
}

// GetCursor returns the current cursor position for caching
func (h *HomeModel) GetCursor() int {
	return h.table.Cursor()
}

// SetCursor sets the cursor position (used when restoring from cache)
func (h *HomeModel) SetCursor(pos int) {
	if pos >= 0 && pos < len(h.containers) {
		h.table.SetCursor(pos)
	}
}
