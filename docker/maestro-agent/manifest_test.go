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
	"testing"
)

func TestLoadManifest_NotFound(t *testing.T) {
	setupTestDirs(t)

	_, err := LoadManifest()
	if err == nil {
		t.Error("LoadManifest() should error when file doesn't exist")
	}
}

func TestLoadManifest_Defaults(t *testing.T) {
	setupTestDirs(t)

	os.WriteFile(manifestFile, []byte("type: manager\n"), 0644)

	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if m.Type != "manager" {
		t.Errorf("Type = %q, want %q", m.Type, "manager")
	}
	if m.OnMessage.Delivery != "batch" {
		t.Errorf("OnMessage.Delivery = %q, want %q", m.OnMessage.Delivery, "batch")
	}
	if !m.Heartbeat.SuppressWhileActive {
		t.Error("Heartbeat.SuppressWhileActive should default to true")
	}
}

func TestLoadManifest_DefaultType(t *testing.T) {
	setupTestDirs(t)

	// Empty manifest — should get default type
	os.WriteFile(manifestFile, []byte("{}\n"), 0644)

	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if m.Type != "interactive" {
		t.Errorf("default Type = %q, want %q", m.Type, "interactive")
	}
}

func TestLoadManifest_FullConfig(t *testing.T) {
	setupTestDirs(t)

	yml := `
type: manager
bootstrap:
  strategy: pipe
  skill: manager
  context:
    files:
      - state.md
      - conversation/:tail:50
    assembly: hooks/build-context.sh
on_stop:
  watchers:
    - queue
    - connected
    - script: hooks/watch-repo.sh
on_idle:
  clear_after: 3600
  pre_clear: hooks/save-state.sh
heartbeat:
  interval: 300
  script: hooks/heartbeat.sh
  suppress_while_active: true
on_session_start:
  compact_context: true
`
	os.WriteFile(manifestFile, []byte(yml), 0644)

	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}

	if m.Type != "manager" {
		t.Errorf("Type = %q, want %q", m.Type, "manager")
	}
	if m.Bootstrap.Strategy != "pipe" {
		t.Errorf("Bootstrap.Strategy = %q, want %q", m.Bootstrap.Strategy, "pipe")
	}
	if m.Bootstrap.Skill != "manager" {
		t.Errorf("Bootstrap.Skill = %q, want %q", m.Bootstrap.Skill, "manager")
	}
	if len(m.Bootstrap.Context.Files) != 2 {
		t.Fatalf("Context.Files len = %d, want 2", len(m.Bootstrap.Context.Files))
	}
	if m.Bootstrap.Context.Files[1] != "conversation/:tail:50" {
		t.Errorf("Context.Files[1] = %q, want %q", m.Bootstrap.Context.Files[1], "conversation/:tail:50")
	}
	if m.Bootstrap.Context.Assembly != "hooks/build-context.sh" {
		t.Errorf("Context.Assembly = %q", m.Bootstrap.Context.Assembly)
	}

	// Watchers
	if len(m.OnStop.Watchers) != 3 {
		t.Fatalf("Watchers len = %d, want 3", len(m.OnStop.Watchers))
	}
	if m.OnStop.Watchers[0].Builtin != "queue" {
		t.Errorf("Watcher[0].Builtin = %q, want %q", m.OnStop.Watchers[0].Builtin, "queue")
	}
	if m.OnStop.Watchers[1].Builtin != "connected" {
		t.Errorf("Watcher[1].Builtin = %q, want %q", m.OnStop.Watchers[1].Builtin, "connected")
	}
	if m.OnStop.Watchers[2].Script != "hooks/watch-repo.sh" {
		t.Errorf("Watcher[2].Script = %q, want %q", m.OnStop.Watchers[2].Script, "hooks/watch-repo.sh")
	}

	// OnIdle
	if m.OnIdle.ClearAfter != 3600 {
		t.Errorf("ClearAfter = %d, want 3600", m.OnIdle.ClearAfter)
	}
	if m.OnIdle.PreClear != "hooks/save-state.sh" {
		t.Errorf("PreClear = %q", m.OnIdle.PreClear)
	}

	// Heartbeat
	if m.Heartbeat.Interval != 300 {
		t.Errorf("Heartbeat.Interval = %d, want 300", m.Heartbeat.Interval)
	}
	if m.Heartbeat.Script != "hooks/heartbeat.sh" {
		t.Errorf("Heartbeat.Script = %q", m.Heartbeat.Script)
	}

	// SessionStart
	if !m.OnSessionStart.CompactContext {
		t.Error("OnSessionStart.CompactContext should be true")
	}
}

func TestLoadManifest_InvalidYAML(t *testing.T) {
	setupTestDirs(t)

	os.WriteFile(manifestFile, []byte("{{invalid yaml"), 0644)

	_, err := LoadManifest()
	if err == nil {
		t.Error("LoadManifest() should error on invalid YAML")
	}
}

func TestHasManifest(t *testing.T) {
	setupTestDirs(t)

	if HasManifest() {
		t.Error("HasManifest() = true before creating file")
	}

	os.WriteFile(manifestFile, []byte("type: interactive\n"), 0644)

	if !HasManifest() {
		t.Error("HasManifest() = false after creating file")
	}
}

func TestWatcherConfig_BuiltinString(t *testing.T) {
	setupTestDirs(t)

	yml := `
on_stop:
  watchers:
    - queue
`
	os.WriteFile(manifestFile, []byte(yml), 0644)

	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if len(m.OnStop.Watchers) != 1 {
		t.Fatalf("got %d watchers, want 1", len(m.OnStop.Watchers))
	}
	if m.OnStop.Watchers[0].Builtin != "queue" {
		t.Errorf("Builtin = %q, want %q", m.OnStop.Watchers[0].Builtin, "queue")
	}
	if m.OnStop.Watchers[0].Script != "" {
		t.Errorf("Script should be empty, got %q", m.OnStop.Watchers[0].Script)
	}
}

func TestWatcherConfig_ScriptMap(t *testing.T) {
	setupTestDirs(t)

	yml := `
on_stop:
  watchers:
    - script: hooks/watch.sh
`
	os.WriteFile(manifestFile, []byte(yml), 0644)

	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if len(m.OnStop.Watchers) != 1 {
		t.Fatalf("got %d watchers, want 1", len(m.OnStop.Watchers))
	}
	if m.OnStop.Watchers[0].Script != "hooks/watch.sh" {
		t.Errorf("Script = %q, want %q", m.OnStop.Watchers[0].Script, "hooks/watch.sh")
	}
	if m.OnStop.Watchers[0].Builtin != "" {
		t.Errorf("Builtin should be empty, got %q", m.OnStop.Watchers[0].Builtin)
	}
}

func TestWatcherConfig_Mixed(t *testing.T) {
	setupTestDirs(t)

	yml := `
on_stop:
  watchers:
    - queue
    - connected
    - script: hooks/a.sh
    - script: hooks/b.sh
`
	os.WriteFile(manifestFile, []byte(yml), 0644)

	m, err := LoadManifest()
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if len(m.OnStop.Watchers) != 4 {
		t.Fatalf("got %d watchers, want 4", len(m.OnStop.Watchers))
	}
	if m.OnStop.Watchers[0].Builtin != "queue" {
		t.Errorf("[0] Builtin = %q", m.OnStop.Watchers[0].Builtin)
	}
	if m.OnStop.Watchers[1].Builtin != "connected" {
		t.Errorf("[1] Builtin = %q", m.OnStop.Watchers[1].Builtin)
	}
	if m.OnStop.Watchers[2].Script != "hooks/a.sh" {
		t.Errorf("[2] Script = %q", m.OnStop.Watchers[2].Script)
	}
	if m.OnStop.Watchers[3].Script != "hooks/b.sh" {
		t.Errorf("[3] Script = %q", m.OnStop.Watchers[3].Script)
	}
}
