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
	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/uprockcom/maestro/pkg/container"
)

// CachedState holds TUI state for seamless return
type CachedState struct {
	Containers []container.Info
	CursorPos  int
}

// Run launches the TUI and returns the result and final state
// Pass cached state from previous run for instant rendering
func Run(containerPrefix string, cachedState *CachedState) (*TUIResult, *CachedState, error) {
	// Initialize bubblezone for mouse click tracking
	zone.NewGlobal()

	model := NewWithCache(containerPrefix, cachedState)

	// tea.WithAltScreen() enables fullscreen mode
	// tea.WithMouseCellMotion() enables mouse support for clicks, wheel, drag
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	finalModel, err := p.Run()
	if err != nil {
		return nil, nil, err
	}

	// Extract result and state from final model
	if m, ok := finalModel.(Model); ok {
		return m.GetResult(), m.GetState(), nil
	}

	return &TUIResult{Action: ActionQuit}, nil, nil
}
