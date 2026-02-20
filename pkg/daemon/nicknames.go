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

package daemon

import (
	"fmt"
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

// NicknameStore manages a persistent mapping of nicknames to container names.
type NicknameStore struct {
	path string
	data map[string]string // nickname → container name
	mu   sync.RWMutex
}

// NewNicknameStore creates a new NicknameStore backed by a YAML file.
func NewNicknameStore(path string) *NicknameStore {
	ns := &NicknameStore{
		path: path,
		data: make(map[string]string),
	}
	ns.load()
	return ns
}

// Set assigns a nickname to a container name, persisting to disk.
func (ns *NicknameStore) Set(nickname, containerName string) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	ns.data[nickname] = containerName
	return ns.save()
}

// Get returns the container name for a nickname.
func (ns *NicknameStore) Get(nickname string) (string, bool) {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	name, ok := ns.data[nickname]
	return name, ok
}

// GetByContainer returns the nickname for a container name.
func (ns *NicknameStore) GetByContainer(containerName string) (string, bool) {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	for nick, name := range ns.data {
		if name == containerName {
			return nick, true
		}
	}
	return "", false
}

// Delete removes a nickname mapping.
func (ns *NicknameStore) Delete(nickname string) error {
	ns.mu.Lock()
	defer ns.mu.Unlock()
	delete(ns.data, nickname)
	return ns.save()
}

// All returns a copy of all nickname mappings.
func (ns *NicknameStore) All() map[string]string {
	ns.mu.RLock()
	defer ns.mu.RUnlock()
	result := make(map[string]string, len(ns.data))
	for k, v := range ns.data {
		result[k] = v
	}
	return result
}

func (ns *NicknameStore) load() {
	data, err := os.ReadFile(ns.path)
	if err != nil {
		return // File doesn't exist yet, start empty
	}
	var m map[string]string
	if err := yaml.Unmarshal(data, &m); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to parse nicknames file %s: %v\n", ns.path, err)
		return
	}
	if m != nil {
		ns.data = m
	}
}

func (ns *NicknameStore) save() error {
	data, err := yaml.Marshal(ns.data)
	if err != nil {
		return fmt.Errorf("failed to marshal nicknames: %w", err)
	}
	return os.WriteFile(ns.path, data, 0644)
}
