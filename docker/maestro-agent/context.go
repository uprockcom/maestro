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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// BuildBootstrapPrompt assembles the full bootstrap prompt from manifest config
func BuildBootstrapPrompt(manifest *Manifest) string {
	var sb strings.Builder

	// 1. Skill invocation (if configured)
	if manifest.Bootstrap.Skill != "" {
		sb.WriteString("/" + manifest.Bootstrap.Skill + "\n\n")
	}

	// 2. Context block
	context := buildContext(manifest)
	if context != "" {
		sb.WriteString("=== MANAGER CONTEXT ===\n\n")
		sb.WriteString(context)
		sb.WriteString("\n=== END CONTEXT ===\n")
	}

	// 3. Pending messages (if any at bootstrap time)
	messages, _ := DrainQueue()
	if len(messages) > 0 {
		sb.WriteString("\n")
		sb.WriteString(FormatMessages(messages))
	}

	return sb.String()
}

// buildContext assembles context from manifest config
func buildContext(manifest *Manifest) string {
	// If custom assembly script is specified, use it
	if manifest.Bootstrap.Context.Assembly != "" {
		result := runAssemblyScript(manifest.Bootstrap.Context.Assembly)
		if result != "" {
			return result
		}
	}

	// Otherwise, concatenate files with headers
	var sb strings.Builder
	for _, fileSpec := range manifest.Bootstrap.Context.Files {
		content := readContextFile(fileSpec)
		if content != "" {
			// Extract display name (strip :tail:N suffix)
			displayName := fileSpec
			if idx := strings.Index(fileSpec, ":tail:"); idx != -1 {
				displayName = fileSpec[:idx] + fmt.Sprintf(" (last %s lines)", fileSpec[idx+6:])
			}
			sb.WriteString(fmt.Sprintf("--- %s ---\n", displayName))
			sb.WriteString(content)
			sb.WriteString("\n\n")
		}
	}

	return sb.String()
}

// readContextFile reads a file, supporting :tail:N suffix for last N lines
func readContextFile(fileSpec string) string {
	var tailLines int
	path := fileSpec

	// Parse :tail:N suffix
	if idx := strings.Index(fileSpec, ":tail:"); idx != -1 {
		path = fileSpec[:idx]
		fmt.Sscanf(fileSpec[idx+6:], "%d", &tailLines)
	}

	// Resolve path relative to workspace project directories
	fullPath := resolvePath(path)
	if fullPath == "" {
		return ""
	}

	if tailLines > 0 {
		// Use tail command
		cmd := exec.Command("tail", "-n", fmt.Sprintf("%d", tailLines), fullPath)
		output, err := cmd.Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(output))
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// runAssemblyScript runs a custom context assembly script and returns its stdout
func runAssemblyScript(scriptPath string) string {
	fullPath := resolvePath(scriptPath)
	if fullPath == "" {
		LogError("Assembly script not found", "error", scriptPath)
		return ""
	}

	cmd := exec.Command("bash", fullPath)
	cmd.Dir = "/workspace"
	output, err := cmd.Output()
	if err != nil {
		LogError("Assembly script failed", "error", err.Error())
		return ""
	}

	return strings.TrimSpace(string(output))
}

// resolvePath resolves a relative path by checking workspace subdirectories
func resolvePath(path string) string {
	if filepath.IsAbs(path) {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		return ""
	}

	// Check directly under /workspace
	candidate := filepath.Join("/workspace", path)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}

	// Check under workspace project subdirectories
	entries, err := os.ReadDir("/workspace")
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			candidate := filepath.Join("/workspace", entry.Name(), path)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}

	return ""
}
