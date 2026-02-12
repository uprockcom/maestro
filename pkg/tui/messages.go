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
	"time"

	"github.com/uprockcom/maestro/pkg/container"
)

// tickMsg is sent on each animation tick (750ms for daemon pulsing)
type tickMsg time.Time

// refreshTickMsg is sent on each refresh interval (30s)
type refreshTickMsg time.Time

// wizardAnimationTickMsg is sent during the opening animation (80ms per column)
type wizardAnimationTickMsg time.Time

// exitWizardMsg is sent when the wizard should exit and transition to normal mode
type exitWizardMsg struct{}

// wizardNextStepMsg is sent to advance to the next wizard step
type wizardNextStepMsg struct{}

// wizardPrevStepMsg is sent to go back to the previous wizard step
type wizardPrevStepMsg struct{}

// saveWizardConfigMsg is sent when wizard completes to save configuration
type saveWizardConfigMsg struct {
	memory     string
	cpus       string
	domains    []string
	runAuthNow bool // If true, exit TUI to run maestro auth
}

// updateWizardConfigMsg is sent to update wizard config fields and advance
type updateWizardConfigMsg struct {
	memory string
	cpus   string
}

// prerequisiteCheckResult contains the results of prerequisite checks
type prerequisiteCheckResult struct {
	claudeAvailable bool
	claudeMessage   string
	dockerAvailable bool
	dockerMessage   string
}

// containersLoadedMsg is sent when container data is loaded
type containersLoadedMsg struct {
	containers       []container.Info
	err              error
	dockerResponsive bool
}

// daemonStatusMsg is sent when daemon status is checked
type daemonStatusMsg struct {
	running bool
	err     error
}

// errorMsg wraps an error for display
type errorMsg struct {
	err error
}

// connectRequestMsg is sent when user wants to connect to a container
type connectRequestMsg struct {
	containerName string
}

// createContainerMsg is sent when user submits the create form
type createContainerMsg struct {
	taskDescription string
	branchName      string
	noConnect       bool
	exact           bool
}

// saveSettingsMsg is sent when user saves settings
type saveSettingsMsg struct {
	memory              string
	cpus                string
	showNag             bool
	autoRefreshTokens   bool
	enableNotifications bool
}

// saveFirewallMsg is sent when user saves firewall configuration
type saveFirewallMsg struct {
	domainsText    string
	applyToRunning bool
}

// Docker operation result messages
type dockerOperationResult struct {
	action        container.OperationType
	containerName string
	success       bool
	err           error
}

// TUIResult is returned when the TUI exits, telling the caller what action to take
type TUIResult struct {
	Action          ActionType
	ContainerName   string
	FilePath        string
	TaskDescription string // For ActionCreate
	BranchName      string // For ActionCreate
	NoConnect       bool   // For ActionCreate
	Exact           bool   // For ActionCreate
}

// ActionType defines what action the TUI wants the caller to perform
type ActionType int

const (
	ActionNone ActionType = iota
	ActionQuit
	ActionConnect    // Connect to a container
	ActionEditConfig // Edit config file
	ActionRunCommand // Run a CLI command
	ActionCreate     // Create a new container
	ActionRunAuth    // Run maestro auth command
)
