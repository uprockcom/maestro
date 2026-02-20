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
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

func hookSessionStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "session-start",
		Short: "Handle Claude's SessionStart hook — readiness signal and context injection",
		RunE:  runHookSessionStart,
	}
}

// sessionStartInput is the JSON received on stdin from Claude Code
type sessionStartInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Source         string `json:"source"` // startup | resume | clear | compact
	Model          string `json:"model"`
}

func runHookSessionStart(cmd *cobra.Command, args []string) error {
	suppressStderr = true
	EnsureStateDirs()
	InitLog()

	// Parse stdin JSON
	var input sessionStartInput
	stdinData, err := io.ReadAll(os.Stdin)
	if err == nil && len(stdinData) > 0 {
		json.Unmarshal(stdinData, &input) // best-effort parse
	}

	LogInfo("SessionStart hook fired",
		"hook", "session-start",
		"trigger", input.Source)

	// Touch readiness flag
	f, err := os.Create(sessionReadyFile)
	if err == nil {
		f.Close()
	}

	// Update state
	WriteState(StateActive)

	// Remove idle flag (Claude is starting up, not idle)
	os.Remove(claudeIdleFile)

	// Handle compaction — inject light context if configured
	if input.Source == "compact" {
		manifest, err := LoadManifest()
		if err == nil && manifest.OnSessionStart.CompactContext {
			context := buildCompactContext(manifest)
			if context != "" {
				fmt.Print(context) // stdout → added as context Claude sees
			}
		}
	}

	return nil // exit 0
}

// buildCompactContext builds a lightweight context refresh for post-compaction
func buildCompactContext(manifest *Manifest) string {
	if manifest.Bootstrap.Context.Assembly != "" {
		// Run the assembly script for compact context
		// For compaction, we could use a lighter version, but for now use the same script
		return runAssemblyScript(manifest.Bootstrap.Context.Assembly)
	}

	// Build from file list — just the state file (lighter than full context)
	if len(manifest.Bootstrap.Context.Files) > 0 {
		// Only include the first file (typically the state file) for compaction
		content := readContextFile(manifest.Bootstrap.Context.Files[0])
		if content != "" {
			return fmt.Sprintf("=== CONTEXT REFRESH (post-compaction) ===\n%s\n=== END CONTEXT ===\n", content)
		}
	}

	return ""
}
