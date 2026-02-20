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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePath_Absolute(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.txt")
	os.WriteFile(f, []byte("content"), 0644)

	if got := resolvePath(f); got != f {
		t.Errorf("resolvePath(%q) = %q, want %q", f, got, f)
	}
}

func TestResolvePath_AbsoluteNotFound(t *testing.T) {
	if got := resolvePath("/nonexistent/file.txt"); got != "" {
		t.Errorf("resolvePath(nonexistent) = %q, want empty", got)
	}
}

func TestResolvePath_RelativeDirectlyUnderWorkspace(t *testing.T) {
	// resolvePath checks /workspace/<path> — we can't easily mock /workspace
	// without overriding the function, but we can test that it returns empty
	// for a relative path when /workspace doesn't have it
	got := resolvePath("definitely-not-a-real-file-xyz.txt")
	if got != "" {
		t.Errorf("resolvePath for missing relative file = %q, want empty", got)
	}
}

func TestReadContextFile_Simple(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "ctx.txt")
	os.WriteFile(f, []byte("  context content  \n"), 0644)

	got := readContextFile(f)
	if got != "context content" {
		t.Errorf("readContextFile() = %q, want %q", got, "context content")
	}
}

func TestReadContextFile_NotFound(t *testing.T) {
	got := readContextFile("/nonexistent/file.txt")
	if got != "" {
		t.Errorf("readContextFile(nonexistent) = %q, want empty", got)
	}
}

func TestReadContextFile_TailSuffix(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "log.txt")

	// Write 10 lines
	var lines []string
	for i := 1; i <= 10; i++ {
		lines = append(lines, "line "+string(rune('0'+i)))
	}
	os.WriteFile(f, []byte(strings.Join(lines, "\n")+"\n"), 0644)

	got := readContextFile(f + ":tail:3")
	// Should contain the last 3 lines
	resultLines := strings.Split(strings.TrimSpace(got), "\n")
	if len(resultLines) != 3 {
		t.Errorf("tail:3 returned %d lines, want 3: %q", len(resultLines), got)
	}
}

func TestBuildContext_FromFiles(t *testing.T) {
	tmp := t.TempDir()

	f1 := filepath.Join(tmp, "state.md")
	f2 := filepath.Join(tmp, "notes.txt")
	os.WriteFile(f1, []byte("current state"), 0644)
	os.WriteFile(f2, []byte("some notes"), 0644)

	manifest := &Manifest{
		Bootstrap: BootstrapConfig{
			Context: ContextConfig{
				Files: []string{f1, f2},
			},
		},
	}

	got := buildContext(manifest)

	if !strings.Contains(got, "current state") {
		t.Error("context should contain state.md content")
	}
	if !strings.Contains(got, "some notes") {
		t.Error("context should contain notes.txt content")
	}
	if !strings.Contains(got, "--- "+f1+" ---") {
		t.Error("context should contain file header for state.md")
	}
}

func TestBuildContext_TailInHeader(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "log.txt")
	os.WriteFile(f, []byte("line1\nline2\nline3\n"), 0644)

	manifest := &Manifest{
		Bootstrap: BootstrapConfig{
			Context: ContextConfig{
				Files: []string{f + ":tail:2"},
			},
		},
	}

	got := buildContext(manifest)

	// Header should show "last 2 lines"
	if !strings.Contains(got, "last 2 lines") {
		t.Errorf("header should show tail info, got:\n%s", got)
	}
}

func TestBuildBootstrapPrompt_SkillPrefix(t *testing.T) {
	setupTestDirs(t)

	manifest := &Manifest{
		Bootstrap: BootstrapConfig{
			Skill: "manager",
		},
	}

	got := BuildBootstrapPrompt(manifest)

	if !strings.HasPrefix(got, "/manager\n") {
		t.Errorf("bootstrap should start with /manager, got: %q", got[:min(len(got), 30)])
	}
}

func TestBuildBootstrapPrompt_NoSkill(t *testing.T) {
	setupTestDirs(t)

	manifest := &Manifest{}

	got := BuildBootstrapPrompt(manifest)

	if strings.HasPrefix(got, "/") {
		t.Errorf("bootstrap without skill should not start with /, got: %q", got[:min(len(got), 30)])
	}
}

func TestBuildBootstrapPrompt_WithContextAndMessages(t *testing.T) {
	setupTestDirs(t)

	tmp := t.TempDir()
	f := filepath.Join(tmp, "state.md")
	os.WriteFile(f, []byte("state content"), 0644)

	// Queue a message
	os.WriteFile(filepath.Join(pendingMsgDir, "1000.txt"), []byte("pending msg"), 0644)

	manifest := &Manifest{
		Bootstrap: BootstrapConfig{
			Skill: "test",
			Context: ContextConfig{
				Files: []string{f},
			},
		},
	}

	got := BuildBootstrapPrompt(manifest)

	if !strings.Contains(got, "/test") {
		t.Error("missing skill invocation")
	}
	if !strings.Contains(got, "=== MANAGER CONTEXT ===") {
		t.Error("missing context block")
	}
	if !strings.Contains(got, "state content") {
		t.Error("missing context content")
	}
	if !strings.Contains(got, "=== MESSAGE SOURCE:") {
		t.Error("missing pending messages")
	}
	if !strings.Contains(got, "pending msg") {
		t.Error("missing message content")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
