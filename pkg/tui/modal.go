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
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone"

	"github.com/uprockcom/maestro/pkg/tui/style"
)

// ModalType defines the type of modal
type ModalType int

const (
	ModalNone             ModalType = iota
	ModalInfo                       // Information message
	ModalError                      // Error message (Crimson Pulse)
	ModalConfirm                    // Yes/No confirmation
	ModalHelp                       // Help/keybindings
	ModalActions                    // Container actions menu
	ModalContainerDetails           // Container info (i key)
	ModalLoading                    // Loading with progress bar
	ModalForm                       // Interactive form with multiple fields
)

// Modal represents a modal dialog
type Modal struct {
	Type           ModalType
	Title          string
	Content        string          // Main content (can be multi-line) - used if viewport is nil
	Width          int             // Modal width (0 = auto)
	Height         int             // Modal height (0 = auto)
	Actions        []ModalAction   // Buttons
	SelectedAction int             // Currently selected action index
	progress       *progress.Model // Progress bar for ModalLoading (nil if not used)
	spinner        *spinner.Model  // Spinner for ModalLoading indeterminate (nil if not used)
	viewport       *viewport.Model // Viewport for scrollable content (nil if not used)
	useViewport    bool            // Whether to use viewport for content
	DisableEsc     bool            // Disable Esc key for modal dismissal (for wizard)

	// Form fields (for ModalForm)
	textarea     *textarea.Model   // Multiline text input
	textinputs   []textinput.Model // Text input fields
	checkboxes   []bool            // Checkbox states
	focusedField int               // Currently focused field index
	fieldLabels  []string          // Labels for form fields

	// Mouse click state for textarea scroll tracking
	lastTextareaLine  int  // Cursor line after last click
	lastScrollOffset  int  // Estimated scroll offset at last click
	scrollOffsetValid bool // Whether lastScrollOffset is valid
}

// ModalAction represents a button in the modal
type ModalAction struct {
	Label     string
	Key       string // Keyboard shortcut (e.g., "y", "n", "enter")
	IsPrimary bool   // Primary actions highlighted
	OnSelect  func() tea.Msg
}

// NewInfoModal creates an info modal
func NewInfoModal(title, content string) *Modal {
	return &Modal{
		Type:    ModalInfo,
		Title:   title,
		Content: content,
		Width:   60,
		Actions: []ModalAction{
			{Label: "OK", Key: "enter", IsPrimary: true},
		},
	}
}

// NewErrorModal creates an error modal
func NewErrorModal(title, content string) *Modal {
	return &Modal{
		Type:    ModalError,
		Title:   title,
		Content: content,
		Width:   60,
		Actions: []ModalAction{
			{Label: "OK", Key: "enter", IsPrimary: true},
		},
	}
}

// NewConfirmModal creates a confirmation modal
func NewConfirmModal(title, content string, onConfirm, onCancel func() tea.Msg) *Modal {
	return &Modal{
		Type:    ModalConfirm,
		Title:   title,
		Content: content,
		Width:   60,
		Actions: []ModalAction{
			{Label: "Yes", Key: "y", IsPrimary: true, OnSelect: onConfirm},
			{Label: "No", Key: "n", IsPrimary: false, OnSelect: onCancel},
		},
		SelectedAction: 0,
	}
}

// NewLoadingModal creates a loading modal with progress or spinner
func NewLoadingModal(title, message string, determinate bool) *Modal {
	m := &Modal{
		Type:    ModalLoading,
		Title:   title,
		Content: message,
		Width:   60,
		Actions: []ModalAction{}, // No actions for loading modal
	}

	if determinate {
		// Use progress bar for determinate operations
		p := progress.New(
			progress.WithDefaultGradient(),
			progress.WithWidth(50),
		)
		m.progress = &p
	} else {
		// Use spinner for indeterminate operations
		s := spinner.New()
		s.Spinner = spinner.Dot
		s.Style = lipgloss.NewStyle().Foreground(style.OceanTide)
		m.spinner = &s
	}

	return m
}

// NewScrollableInfoModal creates an info modal with scrollable content
func NewScrollableInfoModal(title, content string, contentHeight int) *Modal {
	vp := viewport.New(56, contentHeight) // Width slightly less than modal width
	vp.SetContent(content)

	return &Modal{
		Type:        ModalInfo,
		Title:       title,
		Content:     content, // Store for fallback
		Width:       60,
		viewport:    &vp,
		useViewport: true,
		Actions: []ModalAction{
			{Label: "Close", Key: "enter", IsPrimary: true},
		},
	}
}

// NewScrollableInfoModalWide creates an info modal with scrollable content and custom width
func NewScrollableInfoModalWide(title, content string, contentHeight, width int) *Modal {
	vpWidth := width - 4 // Viewport width slightly less than modal width for padding
	vp := viewport.New(vpWidth, contentHeight)
	vp.SetContent(content)

	return &Modal{
		Type:        ModalInfo,
		Title:       title,
		Content:     content, // Store for fallback
		Width:       width,
		viewport:    &vp,
		useViewport: true,
		Actions: []ModalAction{
			{Label: "Close", Key: "enter", IsPrimary: true},
		},
	}
}

// NewScrollableHelpModal creates a help modal with scrollable content
func NewScrollableHelpModal(title, content string, contentHeight int) *Modal {
	vp := viewport.New(66, contentHeight) // Slightly wider for help
	vp.SetContent(content)

	return &Modal{
		Type:        ModalHelp,
		Title:       title,
		Content:     content, // Store for fallback
		Width:       70,
		viewport:    &vp,
		useViewport: true,
		Actions: []ModalAction{
			{Label: "Close", Key: "enter", IsPrimary: true},
		},
	}
}

// SetProgress updates the progress bar percentage (0.0 to 1.0)
func (m *Modal) SetProgress(percent float64) tea.Cmd {
	if m.progress != nil {
		return m.progress.SetPercent(percent)
	}
	return nil
}

// Init starts the spinner animation for loading modals
func (m *Modal) Init() tea.Cmd {
	if m.spinner != nil {
		return m.spinner.Tick
	}
	return nil
}

// Update handles input for the modal
func (m *Modal) Update(msg tea.Msg) (*Modal, tea.Cmd) {
	if m == nil {
		return m, nil
	}

	// Handle progress/spinner updates for loading modals
	switch msg := msg.(type) {
	case progress.FrameMsg:
		if m.progress != nil {
			newProgress, cmd := m.progress.Update(msg)
			*m.progress = newProgress.(progress.Model)
			return m, cmd
		}

	case spinner.TickMsg:
		if m.spinner != nil {
			newSpinner, cmd := m.spinner.Update(msg)
			*m.spinner = newSpinner
			return m, cmd
		}

	case tea.MouseMsg:
		// Handle mouse clicks on buttons and form fields
		if msg.Action != tea.MouseActionRelease || msg.Button != tea.MouseButtonLeft {
			return m, nil
		}

		// Check if a button was clicked
		for i, action := range m.Actions {
			if zone.Get(fmt.Sprintf("modal-action-%d", i)).InBounds(msg) {
				m.SelectedAction = i
				if m.Type == ModalForm {
					// Focus the action button
					actionsStartIdx := 1 + len(m.textinputs) + len(m.checkboxes)
					m.blurFocused()
					m.focusedField = actionsStartIdx + i
				}
				// Execute the action
				if action.OnSelect != nil {
					cmd := action.OnSelect()
					if cmd != nil {
						return nil, func() tea.Msg { return cmd }
					}
				}
				return nil, nil
			}
		}

		// Check if a form field was clicked (for ModalForm)
		if m.Type == ModalForm {
			// Check textarea
			textareaZone := zone.Get("modal-textarea")
			if textareaZone.InBounds(msg) {
				// Only change focus if not already focused
				if m.focusedField != 0 {
					m.blurFocused()
					m.focusedField = 0
					m.focusField()
				}

				// Position cursor based on click position
				if m.textarea != nil {
					x, y := textareaZone.Pos(msg)
					if x >= 0 && y >= 0 {
						currentLine := m.textarea.Line()
						viewportHeight := m.textarea.Height()

						// Determine scroll offset
						var scrollOffset int
						if m.scrollOffsetValid && currentLine == m.lastTextareaLine {
							// Cursor hasn't moved since last click - reuse scroll offset
							scrollOffset = m.lastScrollOffset
						} else {
							// Cursor moved (user scrolled/typed) - recalculate scroll offset
							// The textarea keeps cursor visible, so if cursor is beyond viewport,
							// the content has been scrolled
							if currentLine >= viewportHeight {
								scrollOffset = currentLine - viewportHeight + 1
							} else {
								scrollOffset = 0
							}
						}

						// Calculate target line accounting for scroll
						targetLine := y + scrollOffset
						if targetLine >= m.textarea.LineCount() {
							targetLine = m.textarea.LineCount() - 1
						}
						if targetLine < 0 {
							targetLine = 0
						}

						// Move to target line
						if targetLine < currentLine {
							for i := 0; i < currentLine-targetLine; i++ {
								m.textarea.CursorUp()
							}
						} else if targetLine > currentLine {
							for i := 0; i < targetLine-currentLine; i++ {
								m.textarea.CursorDown()
							}
						}

						// Set column position on the target line
						// Account for line numbers, prompt, and padding
						// Layout: " " + lineNum + " " + "> " + text
						// Approximately 6 characters of prefix
						promptOffset := 6
						col := x - promptOffset
						if col < 0 {
							col = 0
						}
						m.textarea.SetCursor(col)

						// Save state for next click
						m.lastTextareaLine = m.textarea.Line()
						m.lastScrollOffset = scrollOffset
						m.scrollOffsetValid = true
					}
				}
				return m, nil
			}

			// Check text inputs
			for i := range m.textinputs {
				inputZone := zone.Get(fmt.Sprintf("modal-textinput-%d", i))
				if inputZone.InBounds(msg) {
					// Only change focus if not already focused on this field
					targetField := 1 + i
					if m.focusedField != targetField {
						m.blurFocused()
						m.focusedField = targetField
						m.focusField()
					}

					// Position cursor based on click X position
					x, _ := inputZone.Pos(msg)
					if x >= 0 {
						// Account for prompt width (use raw length since prompt has no styling)
						promptLen := len(m.textinputs[i].Prompt)
						cursorPos := x - promptLen
						if cursorPos < 0 {
							cursorPos = 0
						}
						// Clamp to text length
						textLen := len(m.textinputs[i].Value())
						if cursorPos > textLen {
							cursorPos = textLen
						}
						m.textinputs[i].SetCursor(cursorPos)
					}
					return m, nil
				}
			}

			// Check checkboxes
			checkboxStartIdx := 1 + len(m.textinputs)
			for i := range m.checkboxes {
				if zone.Get(fmt.Sprintf("modal-checkbox-%d", i)).InBounds(msg) {
					// Toggle the checkbox and focus it
					m.blurFocused()
					m.focusedField = checkboxStartIdx + i
					m.checkboxes[i] = !m.checkboxes[i]
					return m, nil
				}
			}
		}

		return m, nil

	case tea.KeyMsg:
		// Handle form input for ModalForm
		if m.Type == ModalForm {
			// Determine which field type is focused
			actionsStartIdx := 1 + len(m.textinputs) + len(m.checkboxes)
			checkboxStartIdx := 1 + len(m.textinputs)
			onActionButton := m.focusedField >= actionsStartIdx
			onCheckbox := m.focusedField >= checkboxStartIdx && m.focusedField < actionsStartIdx
			onTextarea := m.focusedField == 0
			onTextinput := m.focusedField > 0 && m.focusedField < checkboxStartIdx

			switch msg.String() {
			case "tab":
				// Tab: move to next field (including action buttons)
				m.blurFocused()
				totalFields := 1 + len(m.textinputs) + len(m.checkboxes) + len(m.Actions)
				m.focusedField = (m.focusedField + 1) % totalFields
				m.focusField()
				return m, nil
			case "shift+tab":
				// Shift+Tab: move to previous field
				m.blurFocused()
				m.focusedField--
				if m.focusedField < 0 {
					totalFields := 1 + len(m.textinputs) + len(m.checkboxes) + len(m.Actions)
					m.focusedField = totalFields - 1
				}
				m.focusField()
				return m, nil
			case "ctrl+s":
				// Ctrl+S: submit form (works from any field)
				if len(m.Actions) > 0 && m.Actions[0].OnSelect != nil {
					cmd := m.Actions[0].OnSelect()
					if cmd != nil {
						return nil, func() tea.Msg { return cmd }
					}
				}
				return nil, nil
			case "esc":
				// Esc: cancel (works from any field) - unless disabled
				if !m.DisableEsc {
					return nil, nil
				}
				return m, nil
			case "left", "h":
				// Left arrow: move between action buttons ONLY when focused on them
				if onActionButton && m.focusedField > actionsStartIdx {
					m.focusedField--
					return m, nil
				}
				// Not on action buttons, fall through to textarea/textinput
			case "right", "l":
				// Right arrow: move between action buttons ONLY when focused on them
				actionsEndIdx := actionsStartIdx + len(m.Actions) - 1
				if onActionButton && m.focusedField < actionsEndIdx {
					m.focusedField++
					return m, nil
				}
				// Not on action buttons, fall through to textarea/textinput
			case "enter":
				// Enter: execute focused action button OR newline in textarea
				if onActionButton {
					actionIdx := m.focusedField - actionsStartIdx
					if actionIdx < len(m.Actions) && m.Actions[actionIdx].OnSelect != nil {
						cmd := m.Actions[actionIdx].OnSelect()
						if cmd != nil {
							return nil, func() tea.Msg { return cmd }
						}
					}
					return nil, nil
				}
				// Not on action button, fall through to textarea/textinput
			case " ":
				// Space: toggle checkbox ONLY if focused on checkbox
				if onCheckbox {
					checkboxIdx := m.focusedField - checkboxStartIdx
					if checkboxIdx < len(m.checkboxes) {
						m.checkboxes[checkboxIdx] = !m.checkboxes[checkboxIdx]
						return m, nil
					}
				}
				// Not on checkbox, fall through to textarea/textinput
			}

			// Default: delegate input to focused field (textarea or textinput)
			var cmd tea.Cmd
			if onTextarea && m.textarea != nil {
				// Textarea is focused - delegate all unhandled keys
				*m.textarea, cmd = m.textarea.Update(msg)
				return m, cmd
			} else if onTextinput {
				// Text input is focused - delegate all unhandled keys
				inputIdx := m.focusedField - 1
				if inputIdx < len(m.textinputs) {
					m.textinputs[inputIdx], cmd = m.textinputs[inputIdx].Update(msg)
					return m, cmd
				}
			}

			return m, nil
		}

		// If viewport is active, delegate scroll keys to it
		if m.useViewport && m.viewport != nil {
			switch msg.String() {
			case "up", "k":
				m.viewport.LineUp(1)
				return m, nil
			case "down", "j":
				m.viewport.LineDown(1)
				return m, nil
			case "pgup":
				m.viewport.ViewUp()
				return m, nil
			case "pgdown", " ":
				m.viewport.ViewDown()
				return m, nil
			case "home":
				m.viewport.GotoTop()
				return m, nil
			case "end":
				m.viewport.GotoBottom()
				return m, nil
			}
		}

		switch msg.String() {
		case "esc":
			// Esc dismisses modal unless disabled
			if !m.DisableEsc {
				return nil, nil
			}
			return m, nil

		case "enter":
			// Execute selected action
			if len(m.Actions) > 0 {
				action := m.Actions[m.SelectedAction]
				if action.OnSelect != nil {
					cmd := action.OnSelect()
					if cmd != nil {
						return nil, func() tea.Msg { return cmd }
					}
				}
			}
			return nil, nil

		case "left", "h":
			// Move selection left
			if m.SelectedAction > 0 {
				m.SelectedAction--
			}

		case "right", "l":
			// Move selection right
			if m.SelectedAction < len(m.Actions)-1 {
				m.SelectedAction++
			}

		case "tab":
			// Tab cycles through actions
			m.SelectedAction = (m.SelectedAction + 1) % len(m.Actions)

		default:
			// Check for action shortcuts
			for i, action := range m.Actions {
				if msg.String() == action.Key {
					m.SelectedAction = i
					if action.OnSelect != nil {
						cmd := action.OnSelect()
						if cmd != nil {
							return nil, func() tea.Msg { return cmd }
						}
					}
					return nil, nil
				}
			}
		}
	}

	return m, nil
}

// blurFocused removes focus from the currently focused form field
func (m *Modal) blurFocused() {
	if m.focusedField == 0 && m.textarea != nil {
		m.textarea.Blur()
	} else if m.focusedField > 0 && m.focusedField-1 < len(m.textinputs) {
		idx := m.focusedField - 1
		m.textinputs[idx].Blur()
		// Change prompt color to dim when blurred
		m.textinputs[idx].PromptStyle = lipgloss.NewStyle().Foreground(style.DimGray)
	}
}

// focusField sets focus on the currently selected form field
func (m *Modal) focusField() {
	if m.focusedField == 0 && m.textarea != nil {
		m.textarea.Focus()
	} else if m.focusedField > 0 && m.focusedField-1 < len(m.textinputs) {
		idx := m.focusedField - 1
		m.textinputs[idx].Focus()
		// Change prompt color to Ocean Tide when focused
		m.textinputs[idx].PromptStyle = lipgloss.NewStyle().Foreground(style.OceanTide)
	}
}

// GetContextHelp returns context-specific help bindings based on modal state
// Returns nil if the modal doesn't support context-specific help
func (m *Modal) GetContextHelp() []key.Binding {
	if m == nil {
		return nil
	}

	// Only ModalForm currently supports context-specific help
	if m.Type != ModalForm {
		return nil
	}

	// Determine which field type is focused
	actionsStartIdx := 1 + len(m.textinputs) + len(m.checkboxes)
	checkboxStartIdx := 1 + len(m.textinputs)
	onActionButton := m.focusedField >= actionsStartIdx
	onCheckbox := m.focusedField >= checkboxStartIdx && m.focusedField < actionsStartIdx
	onTextarea := m.focusedField == 0
	onTextinput := m.focusedField > 0 && m.focusedField < checkboxStartIdx

	var bindings []key.Binding

	if onTextarea {
		// Textarea: show newline capability
		bindings = append(bindings,
			key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("↵", "new line"),
			),
			key.NewBinding(
				key.WithKeys("tab", "shift+tab"),
				key.WithHelp("⇥/⇧⇥", "navigate fields"),
			),
			key.NewBinding(
				key.WithKeys("ctrl+s"),
				key.WithHelp("ctrl+s", "create"),
			),
			key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "cancel"),
			),
		)
	} else if onTextinput {
		// Textinput: enter submits
		bindings = append(bindings,
			key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("↵", "create"),
			),
			key.NewBinding(
				key.WithKeys("tab", "shift+tab"),
				key.WithHelp("⇥/⇧⇥", "navigate fields"),
			),
			key.NewBinding(
				key.WithKeys("ctrl+s"),
				key.WithHelp("ctrl+s", "create"),
			),
			key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "cancel"),
			),
		)
	} else if onCheckbox {
		// Checkbox: space toggles
		bindings = append(bindings,
			key.NewBinding(
				key.WithKeys(" "),
				key.WithHelp("space", "toggle"),
			),
			key.NewBinding(
				key.WithKeys("tab", "shift+tab"),
				key.WithHelp("⇥/⇧⇥", "navigate fields"),
			),
			key.NewBinding(
				key.WithKeys("ctrl+s"),
				key.WithHelp("ctrl+s", "create"),
			),
			key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "cancel"),
			),
		)
	} else if onActionButton {
		// Action buttons: enter executes, arrows navigate buttons
		bindings = append(bindings,
			key.NewBinding(
				key.WithKeys("enter"),
				key.WithHelp("↵", "execute"),
			),
			key.NewBinding(
				key.WithKeys("left", "right", "h", "l"),
				key.WithHelp("←→/h/l", "navigate buttons"),
			),
			key.NewBinding(
				key.WithKeys("tab", "shift+tab"),
				key.WithHelp("⇥/⇧⇥", "navigate all"),
			),
			key.NewBinding(
				key.WithKeys("esc"),
				key.WithHelp("esc", "cancel"),
			),
		)
	}

	return bindings
}

// View renders just the modal box (not placed)
func (m *Modal) View(screenWidth, screenHeight int) string {
	if m == nil {
		return ""
	}

	// Calculate modal dimensions
	modalWidth := m.Width
	if modalWidth == 0 {
		modalWidth = 60
	}
	if modalWidth > screenWidth-4 {
		modalWidth = screenWidth - 4
	}

	// Title style based on modal type
	var titleColor lipgloss.Color
	switch m.Type {
	case ModalError:
		titleColor = style.CrimsonPulse
	case ModalInfo, ModalHelp:
		titleColor = style.OceanTide
	case ModalConfirm:
		titleColor = style.SunsetGlow
	default:
		titleColor = style.OceanTide
	}

	// Modal background color
	modalBg := lipgloss.Color("235")

	titleStyle := lipgloss.NewStyle().
		Foreground(titleColor).
		Background(modalBg).
		Bold(true).
		Width(modalWidth - 4).
		Align(lipgloss.Center)

	title := titleStyle.Render(m.Title)

	// Content - use viewport if available, form if ModalForm, otherwise render normally
	var content string
	var scrollIndicators string

	if m.Type == ModalForm {
		// Render form fields
		var formParts []string
		modalBg := lipgloss.Color("235")

		// Render each field with its label
		fieldIdx := 0

		// Textarea (field 0)
		if m.textarea != nil && fieldIdx < len(m.fieldLabels) {
			labelStyle := lipgloss.NewStyle().
				Foreground(style.OceanTide).
				Background(modalBg).
				Bold(true).
				Width(modalWidth - 4)
			formParts = append(formParts, labelStyle.Render(m.fieldLabels[fieldIdx]))

			// Wrap textarea in a style with explicit background and padding to match textinput
			textareaStyle := lipgloss.NewStyle().
				Background(lipgloss.Color("237")). // Slightly lighter than modal bg
				Width(modalWidth - 4).
				Align(lipgloss.Left)
			// Mark textarea zone for mouse click detection
			formParts = append(formParts, zone.Mark("modal-textarea", textareaStyle.Render(m.textarea.View())))
			formParts = append(formParts, "") // Spacing after textarea
			fieldIdx++
		}

		// Text inputs (first batch - before viewport if present)
		textinputsBeforeViewport := len(m.textinputs)
		if m.viewport != nil && !m.useViewport {
			// If viewport is part of form (not fullscreen), render first 2 textinputs before it
			textinputsBeforeViewport = 2
		}

		for i := 0; i < textinputsBeforeViewport && i < len(m.textinputs); i++ {
			ti := m.textinputs[i]
			if fieldIdx < len(m.fieldLabels) {
				labelStyle := lipgloss.NewStyle().
					Foreground(style.OceanTide).
					Background(modalBg).
					Bold(true).
					Width(modalWidth - 4)
				formParts = append(formParts, labelStyle.Render(m.fieldLabels[fieldIdx]))

				// Wrap textinput in a style with explicit background
				textinputStyle := lipgloss.NewStyle().
					Background(lipgloss.Color("237")). // Slightly lighter than modal bg
					Width(modalWidth - 4).
					Align(lipgloss.Left)
				// Mark textinput zone for mouse click detection
				formParts = append(formParts, zone.Mark(fmt.Sprintf("modal-textinput-%d", i), textinputStyle.Render(ti.View())))
				formParts = append(formParts, "") // Spacing after text input
				fieldIdx++
			}
		}

		// Viewport (if part of form, not fullscreen)
		if m.viewport != nil && !m.useViewport && fieldIdx < len(m.fieldLabels) {
			labelStyle := lipgloss.NewStyle().
				Foreground(style.OceanTide).
				Background(modalBg).
				Bold(true).
				Width(modalWidth - 4)
			formParts = append(formParts, labelStyle.Render(m.fieldLabels[fieldIdx]))

			// Wrap viewport in a style with explicit background
			viewportStyle := lipgloss.NewStyle().
				Background(lipgloss.Color("237")).
				Width(modalWidth - 4).
				Align(lipgloss.Left)
			formParts = append(formParts, viewportStyle.Render(m.viewport.View()))
			formParts = append(formParts, "") // Spacing after viewport
			fieldIdx++
		}

		// Remaining text inputs (after viewport)
		for i := textinputsBeforeViewport; i < len(m.textinputs); i++ {
			ti := m.textinputs[i]
			if fieldIdx < len(m.fieldLabels) {
				labelStyle := lipgloss.NewStyle().
					Foreground(style.OceanTide).
					Background(modalBg).
					Bold(true).
					Width(modalWidth - 4)
				formParts = append(formParts, labelStyle.Render(m.fieldLabels[fieldIdx]))

				// Wrap textinput in a style with explicit background
				textinputStyle := lipgloss.NewStyle().
					Background(lipgloss.Color("237")).
					Width(modalWidth - 4).
					Align(lipgloss.Left)
				// Mark textinput zone for mouse click detection
				formParts = append(formParts, zone.Mark(fmt.Sprintf("modal-textinput-%d", i), textinputStyle.Render(ti.View())))
				formParts = append(formParts, "") // Spacing after text input
				fieldIdx++
			}
		}

		// Checkboxes
		for i, checked := range m.checkboxes {
			if fieldIdx < len(m.fieldLabels) {
				checkboxIcon := "☐"
				checkboxColor := style.SilverMist
				if checked {
					checkboxIcon = "☑"
					checkboxColor = style.OceanTide
				}

				// Highlight if focused
				labelColor := style.GhostWhite
				if m.focusedField == 1+len(m.textinputs)+i {
					labelColor = style.OceanSurge
				}

				// Render checkbox and label together in one style to avoid gaps
				checkboxLineStyle := lipgloss.NewStyle().
					Background(modalBg).
					Width(modalWidth - 4)

				// Build the full line with colored parts
				checkboxPart := lipgloss.NewStyle().
					Foreground(checkboxColor).
					Background(modalBg).
					Render(checkboxIcon)

				labelPart := lipgloss.NewStyle().
					Foreground(labelColor).
					Background(modalBg).
					Render(" " + m.fieldLabels[fieldIdx])

				checkboxLine := checkboxPart + labelPart
				// Mark checkbox zone for mouse click detection
				formParts = append(formParts, zone.Mark(fmt.Sprintf("modal-checkbox-%d", i), checkboxLineStyle.Render(checkboxLine)))
				fieldIdx++
			}
		}

		// Join form parts with newlines
		contentStyle := lipgloss.NewStyle().
			Foreground(style.GhostWhite).
			Background(modalBg).
			Width(modalWidth - 4).
			Align(lipgloss.Left)
		content = contentStyle.Render(lipgloss.JoinVertical(lipgloss.Left, formParts...))

	} else if m.useViewport && m.viewport != nil {
		// Render viewport content
		content = m.viewport.View()

		// Add scroll indicators if not at top/bottom
		if !m.viewport.AtTop() && !m.viewport.AtBottom() {
			scrollIndicators = lipgloss.NewStyle().
				Foreground(style.SilverMist).
				Background(modalBg).
				Width(modalWidth - 4).
				Align(lipgloss.Center).
				Render("▲ Scroll ▼")
		} else if !m.viewport.AtTop() {
			scrollIndicators = lipgloss.NewStyle().
				Foreground(style.SilverMist).
				Background(modalBg).
				Width(modalWidth - 4).
				Align(lipgloss.Center).
				Render("▲ Scroll up for more")
		} else if !m.viewport.AtBottom() {
			scrollIndicators = lipgloss.NewStyle().
				Foreground(style.SilverMist).
				Background(modalBg).
				Width(modalWidth - 4).
				Align(lipgloss.Center).
				Render("▼ Scroll down for more")
		}
	} else {
		// Normal content rendering
		contentStyle := lipgloss.NewStyle().
			Foreground(style.GhostWhite).
			Background(modalBg).
			Width(modalWidth - 4).
			Align(lipgloss.Left)
		content = contentStyle.Render(m.Content)
	}

	// Add progress bar or spinner for loading modals
	var loadingIndicator string
	if m.progress != nil {
		loadingIndicator = m.progress.View()
	} else if m.spinner != nil {
		// Center the spinner
		spinnerStyle := lipgloss.NewStyle().
			Background(modalBg).
			Width(modalWidth - 4).
			Align(lipgloss.Center)
		loadingIndicator = spinnerStyle.Render(m.spinner.View())
	}

	// Actions - render as simple styled blocks without borders
	var actionsView string
	if len(m.Actions) > 0 {
		actionParts := make([]string, len(m.Actions))

		// Calculate selected action index based on modal type
		selectedIdx := m.SelectedAction
		if m.Type == ModalForm {
			// For forms, use focusedField to determine selection
			actionsStartIdx := 1 + len(m.textinputs) + len(m.checkboxes)
			if m.focusedField >= actionsStartIdx {
				selectedIdx = m.focusedField - actionsStartIdx
			} else {
				selectedIdx = -1 // No action selected
			}
		}

		for i, action := range m.Actions {
			var actionStyle lipgloss.Style
			if i == selectedIdx {
				// Selected action - highlighted with background
				if action.IsPrimary {
					actionStyle = lipgloss.NewStyle().
						Foreground(style.GhostWhite).
						Background(style.OceanTide).
						Bold(true).
						Padding(0, 3)
				} else {
					actionStyle = lipgloss.NewStyle().
						Foreground(style.GhostWhite).
						Background(style.DimGray).
						Bold(true).
						Padding(0, 3)
				}
			} else {
				// Unselected action - subtle background
				if action.IsPrimary {
					actionStyle = lipgloss.NewStyle().
						Foreground(style.OceanTide).
						Background(lipgloss.Color("237")).
						Padding(0, 3)
				} else {
					actionStyle = lipgloss.NewStyle().
						Foreground(style.SilverMist).
						Background(lipgloss.Color("237")).
						Padding(0, 3)
				}
			}
			// Wrap button with zone marker for mouse click detection
			actionParts[i] = zone.Mark(fmt.Sprintf("modal-action-%d", i), actionStyle.Render(action.Label))
		}
		// Join with spacing between buttons
		actionsView = lipgloss.JoinHorizontal(lipgloss.Left, actionParts...)
		// Center the actions within the modal width
		actionsStyle := lipgloss.NewStyle().
			Background(modalBg).
			Width(modalWidth - 4).
			Align(lipgloss.Center)
		actionsView = actionsStyle.Render(actionsView)
	}

	// Create styled spacer line for consistent backgrounds
	spacer := lipgloss.NewStyle().
		Background(modalBg).
		Width(modalWidth - 4).
		Render("")

	// Combine everything (help is now rendered at bottom of screen)
	var parts []string
	parts = append(parts, spacer, title, spacer, content)

	// Add scroll indicators if present
	if scrollIndicators != "" {
		parts = append(parts, spacer, scrollIndicators)
	}

	// Add loading indicator if present
	if loadingIndicator != "" {
		parts = append(parts, spacer, loadingIndicator)
	}

	if actionsView != "" {
		parts = append(parts, spacer, actionsView)
	}
	parts = append(parts, spacer)

	modalContent := lipgloss.JoinVertical(lipgloss.Center, parts...)

	// Modal box - just return the box, not placed
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(style.PurpleHaze).
		Background(lipgloss.Color("235")).
		Padding(1, 2).
		Width(modalWidth)

	return modalStyle.Render(modalContent)
}

// RenderWithBackground renders modal centered on dimmed background
func (m *Modal) RenderWithBackground(background string, screenWidth, screenHeight int) string {
	if m == nil {
		return background
	}

	// Dim the background
	dimStyle := lipgloss.NewStyle().Foreground(style.DimGray)
	dimmedBg := dimStyle.Render(background)

	// Get the modal box (unplaced)
	modalBox := m.View(screenWidth, screenHeight)
	modalBoxLines := strings.Split(modalBox, "\n")

	// Calculate modal position
	modalHeight := len(modalBoxLines)
	startY := (screenHeight - modalHeight) / 2

	// Split background
	bgLines := strings.Split(dimmedBg, "\n")
	for len(bgLines) < screenHeight {
		bgLines = append(bgLines, strings.Repeat(" ", screenWidth))
	}

	// Build result by overlaying modal on background with ANSI-aware compositing
	result := make([]string, screenHeight)
	for i := 0; i < screenHeight; i++ {
		modalLineIdx := i - startY
		if modalLineIdx >= 0 && modalLineIdx < len(modalBoxLines) {
			// This line has modal content - composite it onto background
			modalLine := modalBoxLines[modalLineIdx]
			bgLine := bgLines[i]

			// Use ANSI-aware compositing to center modal on background
			result[i] = compositeModalLine(modalLine, bgLine, screenWidth)
		} else {
			// No modal on this line, just background
			result[i] = bgLines[i]
		}
	}

	return strings.Join(result, "\n")
}

// compositeModalLine centers a modal line on a background line using ANSI-aware operations
func compositeModalLine(modalLine, bgLine string, screenWidth int) string {
	// Calculate the visual width of the modal line (ignoring ANSI codes)
	modalWidth := ansi.StringWidth(modalLine)

	// If modal is wider than screen, truncate it
	if modalWidth > screenWidth {
		return ansi.Truncate(modalLine, screenWidth, "...")
	}

	// Calculate left padding for centering
	leftPad := (screenWidth - modalWidth) / 2

	// Get the visual width of the background (may have ANSI codes)
	bgWidth := ansi.StringWidth(bgLine)

	// If background is too short, pad it with spaces
	if bgWidth < screenWidth {
		bgLine += strings.Repeat(" ", screenWidth-bgWidth)
	} else if bgWidth > screenWidth {
		// Truncate background if it's too long
		bgLine = ansi.Truncate(bgLine, screenWidth, "")
	}

	// Build the composite line by extracting background segments and inserting modal
	// Left segment: background from 0 to leftPad
	leftSegment := ""
	if leftPad > 0 {
		leftSegment = ansi.Truncate(bgLine, leftPad, "")
	}

	// Right segment: background after modal
	rightSegment := ""
	rightStart := leftPad + modalWidth
	if rightStart < screenWidth {
		// We need to skip the first 'rightStart' visual characters of bgLine
		// Since ansi.Truncate gives us the first N chars, we need a different approach
		// Extract the full background, then skip characters
		bgRunes := []rune(ansi.Strip(bgLine))
		if rightStart < len(bgRunes) {
			rightSegment = string(bgRunes[rightStart:])
			// Truncate to remaining width
			remainingWidth := screenWidth - rightStart
			if ansi.StringWidth(rightSegment) > remainingWidth {
				rightSegment = ansi.Truncate(rightSegment, remainingWidth, "")
			}
		}
	}

	// Composite: left background + modal + right background
	return leftSegment + modalLine + rightSegment
}
