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
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/uprockcom/maestro/pkg/container"
)

// Alarm represents a scheduled alarm for a container
type Alarm struct {
	ID            string     `json:"id"`
	ContainerName string     `json:"container"`
	Name          string     `json:"name"`
	Message       string     `json:"message"`
	FireAt        time.Time  `json:"fire_at"`
	CreatedAt     time.Time  `json:"created_at"`
	Fired         bool       `json:"fired,omitempty"`
	FiredAt       *time.Time `json:"fired_at,omitempty"`
}

// AlarmStore tracks alarms across all containers
type AlarmStore struct {
	mu     sync.Mutex
	alarms map[string]*Alarm // keyed by alarm ID
}

// NewAlarmStore creates a new alarm store
func NewAlarmStore() *AlarmStore {
	return &AlarmStore{
		alarms: make(map[string]*Alarm),
	}
}

// Has returns whether an alarm with the given ID exists (fired or not)
func (s *AlarmStore) Has(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.alarms[id]
	return ok
}

// Add registers a new alarm. If an alarm with the same ID already exists it is
// silently overwritten — this is expected during daemon restart recovery where
// the same alarm may be loaded from both the container filesystem and a pending
// IPC request file.
func (s *AlarmStore) Add(alarm *Alarm) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alarms[alarm.ID] = alarm
}

// Cancel removes an alarm by ID, returning whether it existed
func (s *AlarmStore) Cancel(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.alarms[id]; ok {
		delete(s.alarms, id)
		return true
	}
	return false
}

// CancelByName removes an alarm by container + name, returning whether it existed
func (s *AlarmStore) CancelByName(containerName, name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, a := range s.alarms {
		if a.ContainerName == containerName && a.Name == name && !a.Fired {
			delete(s.alarms, id)
			return true
		}
	}
	return false
}

// ListForContainer returns copies of all pending alarms for a container.
// The returned alarms are safe to read without holding the store's lock.
func (s *AlarmStore) ListForContainer(containerName string) []Alarm {
	s.mu.Lock()
	defer s.mu.Unlock()
	var result []Alarm
	for _, a := range s.alarms {
		if a.ContainerName == containerName && !a.Fired {
			result = append(result, *a)
		}
	}
	return result
}

// CheckAndFire returns all alarms that should fire now (past their fire time)
// and marks them as fired
func (s *AlarmStore) CheckAndFire(now time.Time) []*Alarm {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ready []*Alarm
	for _, a := range s.alarms {
		if !a.Fired && !now.Before(a.FireAt) {
			a.Fired = true
			firedAt := now
			a.FiredAt = &firedAt
			ready = append(ready, a)
		}
	}
	return ready
}

// CleanupContainer removes all alarms for a container (when it stops)
func (s *AlarmStore) CleanupContainer(containerName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, a := range s.alarms {
		if a.ContainerName == containerName {
			delete(s.alarms, id)
		}
	}
}

// CleanupFired removes all fired alarms older than the given duration
func (s *AlarmStore) CleanupFired(olderThan time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := time.Now().Add(-olderThan)
	for id, a := range s.alarms {
		if a.Fired && a.FiredAt != nil && a.FiredAt.Before(cutoff) {
			delete(s.alarms, id)
		}
	}
}

// Count returns the number of pending (unfired) alarms
func (s *AlarmStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, a := range s.alarms {
		if !a.Fired {
			count++
		}
	}
	return count
}

// fireAlarms checks for due alarms and delivers them via the pending-messages queue
func (d *Daemon) fireAlarms() {
	if d.alarms == nil {
		return
	}

	ready := d.alarms.CheckAndFire(time.Now())
	for _, alarm := range ready {
		// Format the alarm message as a trigger
		msg := formatAlarmMessage(alarm)

		// Queue the message in the container
		if err := container.QueueMessage(alarm.ContainerName, msg); err != nil {
			d.logError("Failed to deliver alarm %s to %s: %v", alarm.ID, alarm.ContainerName, err)
			continue
		}
		d.logInfo("Alarm fired: %s (%s) in %s", alarm.Name, alarm.ID, d.getShortName(alarm.ContainerName))

		// Clean up the persisted alarm file from the container
		removeAlarmFromContainer(alarm.ContainerName, alarm.ID)
	}

	// Periodically clean up old fired alarms (keep for 1 hour for debugging).
	// This runs inside fireAlarms which is called every check cycle, but
	// CleanupFired is cheap (single lock + map scan) so no throttling needed.
	d.alarms.CleanupFired(1 * time.Hour)
}

// loadAlarmsFromContainer reads persisted alarms from a container's filesystem
func (d *Daemon) loadAlarmsFromContainer(containerName string) {
	if d.alarms == nil {
		return
	}

	// List alarm files in the container
	listCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		"ls /home/node/.maestro/alarms/*.json 2>/dev/null")
	output, err := listCmd.Output()
	if err != nil {
		return // no alarms directory or no files
	}

	files := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, f := range files {
		if f == "" {
			continue
		}
		readCmd := exec.Command("docker", "exec", containerName, "cat", f)
		data, err := readCmd.Output()
		if err != nil {
			d.logInfo("Alarm recovery: skipping unreadable file %s in %s", f, d.getShortName(containerName))
			continue
		}
		var alarm Alarm
		if err := json.Unmarshal(data, &alarm); err != nil {
			d.logInfo("Alarm recovery: skipping corrupt file %s in %s: %v", f, d.getShortName(containerName), err)
			continue
		}
		// Only load alarms that haven't fired yet (past-due alarms will fire on the next check cycle)
		if alarm.Fired {
			d.logInfo("Alarm recovery: skipping already-fired alarm %s (%s) in %s", alarm.Name, alarm.ID, d.getShortName(containerName))
		} else {
			alarm.ContainerName = containerName
			d.alarms.Add(&alarm)
			d.logInfo("Recovered alarm %s (%s) from %s, fires at %s",
				alarm.Name, alarm.ID, d.getShortName(containerName),
				alarm.FireAt.Format(time.RFC3339))
		}
	}
}

// persistAlarmToContainer writes an alarm file to the container's filesystem
func persistAlarmToContainer(containerName string, alarm *Alarm) error {
	data, err := json.MarshalIndent(alarm, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling alarm: %w", err)
	}

	// Ensure alarms directory exists
	mkdirCmd := exec.Command("docker", "exec", containerName, "mkdir", "-p",
		"/home/node/.maestro/alarms")
	_ = mkdirCmd.Run()

	// Write alarm file
	path := fmt.Sprintf("/home/node/.maestro/alarms/%s.json", alarm.ID)
	writeCmd := exec.Command("docker", "exec", "-i", containerName, "tee", path)
	writeCmd.Stdin = strings.NewReader(string(data))
	writeCmd.Stdout = nil
	if err := writeCmd.Run(); err != nil {
		return fmt.Errorf("writing alarm file: %w", err)
	}

	return nil
}

// removeAlarmFromContainer deletes an alarm file from the container
func removeAlarmFromContainer(containerName, alarmID string) {
	path := fmt.Sprintf("/home/node/.maestro/alarms/%s.json", alarmID)
	rmCmd := exec.Command("docker", "exec", containerName, "rm", "-f", path)
	_ = rmCmd.Run()
}

// formatAlarmMessage formats an alarm as a trigger message for the queue
func formatAlarmMessage(alarm *Alarm) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("=== TRIGGER: alarm ===\n"))
	sb.WriteString(fmt.Sprintf("Alarm: %s\n", alarm.Name))
	if alarm.Message != "" {
		sb.WriteString(fmt.Sprintf("Message: %s\n", alarm.Message))
	}
	sb.WriteString(fmt.Sprintf("Scheduled for: %s\n", alarm.FireAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("Fired at: %s\n", time.Now().UTC().Format(time.RFC3339)))
	sb.WriteString("=== END TRIGGER ===\n")
	return sb.String()
}
