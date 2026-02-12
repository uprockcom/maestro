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

package style

import "github.com/charmbracelet/lipgloss"

// Primary Colors
var (
	PurpleHaze   = lipgloss.Color("#703898")
	CrimsonPulse = lipgloss.Color("#C52735")
	SunsetGlow   = lipgloss.Color("#FCC451")
)

// Accent Colors (Maestro-Specific)
var (
	OceanTide  = lipgloss.Color("#00BCD4") // Primary accent
	OceanSurge = lipgloss.Color("#00E5FF") // Focused elements
	OceanDepth = lipgloss.Color("#008CA3") // Muted
	OceanAbyss = lipgloss.Color("#006978") // Subtle
	HotPink    = lipgloss.Color("#FF10F0") // Titles
	NeonGreen  = lipgloss.Color("#00FF41") // Success
)

// Grayscale
var (
	GhostWhite = lipgloss.Color("#F0F0F0")
	SilverMist = lipgloss.Color("#A0A0A0")
	DimGray    = lipgloss.Color("#4A4A4A")
	DeepSpace  = lipgloss.Color("#0A0E27")
)

// Focus
var (
	FocusedBorder   = OceanSurge
	UnfocusedBorder = PurpleHaze
)

// Animation shades for ping-pong effect
var OceanTideAnimShades = []string{
	"#00E5FF", "#00D4E8", "#00BCD4", "#00A3BB", "#008CA3",
}

// DaemonAnimShades uses pure greens from the xterm-256 palette for a subtle pulse
// xterm-256 color cube: 16 + 36*r + 6*g + b where r,g,b ∈ [0,5]
// Selected for pure green appearance (r=0, low blue component)
var DaemonAnimShades = []string{
	"48", // r=0, g=5, b=0 - brightest green
	"47", // r=0, g=4, b=5
	"43", // r=0, g=4, b=3
	"42", // r=0, g=4, b=2
	"41", // r=0, g=4, b=1
	"40", // r=0, g=4, b=0
	"37", // r=0, g=3, b=3
	"36", // r=0, g=3, b=2
	"35", // r=0, g=3, b=1
	"34", // r=0, g=3, b=0
	"30", // r=0, g=2, b=2
	"29", // r=0, g=2, b=1
	"28", // r=0, g=2, b=0
	"24", // r=0, g=1, b=2
	"23", // r=0, g=1, b=1
	"22", // r=0, g=1, b=0
}

// GetOceanTideShade returns the Ocean Tide color for the given animation state (0-4)
func GetOceanTideShade(state int) lipgloss.Color {
	if state < 0 || state >= len(OceanTideAnimShades) {
		state = 2 // Default to middle
	}
	return lipgloss.Color(OceanTideAnimShades[state])
}

// GetDaemonShade returns the daemon indicator color for the given animation state (0-15)
func GetDaemonShade(state int) lipgloss.Color {
	if state < 0 || state >= len(DaemonAnimShades) {
		state = len(DaemonAnimShades) / 2 // Default to middle
	}
	return lipgloss.Color(DaemonAnimShades[state])
}
