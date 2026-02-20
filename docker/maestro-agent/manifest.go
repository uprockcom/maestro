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

	"gopkg.in/yaml.v3"
)

// Manifest represents the agent.yml configuration
type Manifest struct {
	Type string `yaml:"type"` // interactive | manager | worker

	Bootstrap BootstrapConfig `yaml:"bootstrap"`
	OnStop    OnStopConfig    `yaml:"on_stop"`
	OnIdle    OnIdleConfig    `yaml:"on_idle"`
	OnMessage OnMessageConfig `yaml:"on_message"`

	Heartbeat      HeartbeatConfig      `yaml:"heartbeat"`
	OnSessionStart OnSessionStartConfig `yaml:"on_session_start"`
}

type BootstrapConfig struct {
	Strategy string         `yaml:"strategy"` // pipe | cli_arg | skill
	Skill    string         `yaml:"skill"`    // skill name (e.g., "manager")
	Context  ContextConfig  `yaml:"context"`
}

type ContextConfig struct {
	Files    []string `yaml:"files"`    // relative to workspace
	Assembly string   `yaml:"assembly"` // optional custom script
}

type OnStopConfig struct {
	Watchers      []WatcherConfig `yaml:"watchers"`
	Compress      bool            `yaml:"compress"`
	CompressModel string          `yaml:"compress_model"`
	Notify        bool            `yaml:"notify"`
}

type WatcherConfig struct {
	// For built-in watchers, this is just "queue" or "connected"
	// For script watchers, the YAML is: script: path/to/script.sh
	// We handle both via custom unmarshaling
	Builtin string `yaml:"-"`
	Script  string `yaml:"-"`
}

func (w *WatcherConfig) UnmarshalYAML(value *yaml.Node) error {
	// Try as a simple string first (built-in watcher)
	if value.Kind == yaml.ScalarNode {
		w.Builtin = value.Value
		return nil
	}
	// Try as a map with "script" key
	if value.Kind == yaml.MappingNode {
		var m map[string]string
		if err := value.Decode(&m); err != nil {
			return err
		}
		if s, ok := m["script"]; ok {
			w.Script = s
		}
		return nil
	}
	return nil
}

type OnIdleConfig struct {
	ClearAfter int    `yaml:"clear_after"` // seconds, 0 = never
	PreClear   string `yaml:"pre_clear"`   // script to run before kill-restart
}

type OnMessageConfig struct {
	Delivery string `yaml:"delivery"` // batch | immediate
}

type HeartbeatConfig struct {
	Interval             int    `yaml:"interval"`               // seconds, 0 = disabled
	Script               string `yaml:"script"`                 // generates heartbeat message
	SuppressWhileActive  bool   `yaml:"suppress_while_active"`  // default true
}

type OnSessionStartConfig struct {
	CompactContext bool `yaml:"compact_context"` // re-inject light context on compaction
}

// LoadManifest reads and parses agent.yml
func LoadManifest() (*Manifest, error) {
	data, err := os.ReadFile(manifestFile)
	if err != nil {
		return nil, err
	}

	m := &Manifest{
		Type: "interactive", // default
		OnMessage: OnMessageConfig{
			Delivery: "batch",
		},
		Heartbeat: HeartbeatConfig{
			SuppressWhileActive: true,
		},
	}

	if err := yaml.Unmarshal(data, m); err != nil {
		return nil, err
	}

	return m, nil
}

// HasManifest returns true if agent.yml exists
func HasManifest() bool {
	_, err := os.Stat(manifestFile)
	return err == nil
}
