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

package container

import "time"

// Credentials represents the Claude OAuth credentials structure
type Credentials struct {
	ClaudeAiOauth struct {
		AccessToken      string   `json:"accessToken"`
		RefreshToken     string   `json:"refreshToken"`
		ExpiresAt        int64    `json:"expiresAt"` // milliseconds
		Scopes           []string `json:"scopes"`
		SubscriptionType string   `json:"subscriptionType"`
	} `json:"claudeAiOauth"`
}

// Info holds information about a container
type Info struct {
	Name           string
	ShortName      string
	Status         string
	StatusDetails  string
	Branch         string
	AgentState     string    // maestro-agent state (starting, active, waiting, idle, question, clearing, connected)
	IsDormant      bool      // Claude process not running
	HasWeb         bool      // Container has web/browser support (Playwright)
	AuthStatus     string    // Token expiration status
	LastActivity   string    // Time since last activity
	GitStatus      string    // Git status indicators
	CreatedAt      time.Time // Container creation time
	CurrentTask    string    // Current task being worked on (from Claude Code task management)
	TaskProgress   string    // Task progress (e.g., "2/5")
	Contacts       map[string]map[string]string // Contact overrides from maestro.contacts label
}

// DisplayOptions configures how containers are displayed
type DisplayOptions struct {
	ShowNumbers bool // Show selection numbers (for interactive selection)
	ShowTable   bool // Show full table format with all columns
}

// ContainerDetails holds comprehensive information about a container for the details view
type ContainerDetails struct {
	Name          string
	ShortName     string
	Status        string
	StatusDetails string
	Branch        string
	GitStatus     string
	AuthStatus    string
	LastActivity  string
	Uptime        string
	CPUs          string
	Memory        string
	IPAddress     string
	Ports         []string
	Volumes       []string
	Environment   []string
	RecentLogs    string
}
