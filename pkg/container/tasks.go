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

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// TaskStatus represents the status of a Claude Code task
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusInProgress TaskStatus = "in_progress"
	TaskStatusCompleted  TaskStatus = "completed"
)

// Task represents a Claude Code task item
// Supports both old TodoWrite format (content) and new Task* format (subject/description)
type Task struct {
	ID          string     `json:"id"`
	Content     string     `json:"content,omitempty"`     // TodoWrite format: task description
	Subject     string     `json:"subject,omitempty"`     // Task* format: task title
	Description string     `json:"description,omitempty"` // Task* format: detailed description
	Status      TaskStatus `json:"status"`                // pending, in_progress, completed
	ActiveForm  string     `json:"activeForm,omitempty"`  // Present tense form shown during execution
	BlockedBy   []string   `json:"blockedBy,omitempty"`   // Task* format: dependency IDs
	Blocks      []string   `json:"blocks,omitempty"`      // Task* format: tasks this blocks
}

// GetDisplayName returns the best available name for display
func (t *Task) GetDisplayName() string {
	if t.ActiveForm != "" {
		return t.ActiveForm
	}
	if t.Subject != "" {
		return t.Subject
	}
	if t.Content != "" {
		return t.Content
	}
	if t.Description != "" {
		// Truncate long descriptions
		if len(t.Description) > 60 {
			return t.Description[:57] + "..."
		}
		return t.Description
	}
	return fmt.Sprintf("Task %s", t.ID)
}

// Session represents a Claude Code session containing tasks
type Session struct {
	ID         string    // Session UUID (directory name)
	Tasks      []Task    // Tasks in this session
	LastUpdate time.Time // Last modification time
}

// SessionSummary provides a quick overview of session state
type SessionSummary struct {
	ID              string
	TotalTasks      int
	PendingTasks    int
	InProgressTasks int
	CompletedTasks  int
	CurrentTask     string // The activeForm of the in_progress task
	LastUpdate      time.Time
}

// ContainerTasks holds all task data for a container
type ContainerTasks struct {
	ContainerName string
	ShortName     string
	Sessions      []Session
	Error         error // If there was an error reading tasks
}

// GetSummary returns a summary of the session's task state
func (s *Session) GetSummary() SessionSummary {
	summary := SessionSummary{
		ID:         s.ID,
		TotalTasks: len(s.Tasks),
		LastUpdate: s.LastUpdate,
	}

	for _, task := range s.Tasks {
		switch task.Status {
		case TaskStatusPending:
			summary.PendingTasks++
		case TaskStatusInProgress:
			summary.InProgressTasks++
			summary.CurrentTask = task.GetDisplayName()
		case TaskStatusCompleted:
			summary.CompletedTasks++
		}
	}

	return summary
}

// GetContainerTasks reads all task data from a container
// Supports two formats:
// - Old TodoWrite: /home/node/.claude/todos/{session-uuid}-agent-{agent-uuid}.json (array format)
// - New Task*: /home/node/.claude/tasks/{session-uuid}/{id}.json (individual files)
func GetContainerTasks(containerName string) (*ContainerTasks, error) {
	result := &ContainerTasks{
		ContainerName: containerName,
		ShortName:     getShortName(containerName),
		Sessions:      []Session{},
	}

	// Check if container is running
	checkCmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", containerName)
	output, err := checkCmd.Output()
	if err != nil {
		result.Error = fmt.Errorf("container not found or not accessible")
		return result, nil
	}

	if strings.TrimSpace(string(output)) != "true" {
		result.Error = fmt.Errorf("container is not running")
		return result, nil
	}

	// Read from OLD format: /home/node/.claude/todos/
	// Files are named: {session-uuid}-agent-{agent-uuid}.json and contain an array of tasks
	listCmd := exec.Command("docker", "exec", containerName,
		"find", "/home/node/.claude/todos", "-maxdepth", "1", "-name", "*.json", "-type", "f")
	if output, err := listCmd.Output(); err == nil {
		todoFiles := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, todoFile := range todoFiles {
			if todoFile == "" {
				continue
			}
			if session, err := readTodoFile(containerName, todoFile); err == nil && len(session.Tasks) > 0 {
				result.Sessions = append(result.Sessions, *session)
			}
		}
	}

	// Read from NEW format: /home/node/.claude/tasks/
	// Structure: tasks/{session-uuid}/{id}.json where each file is a single task
	listCmd = exec.Command("docker", "exec", containerName,
		"find", "/home/node/.claude/tasks", "-mindepth", "1", "-maxdepth", "1", "-type", "d")
	if output, err := listCmd.Output(); err == nil {
		sessionDirs := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, sessionDir := range sessionDirs {
			if sessionDir == "" {
				continue
			}
			sessionID := filepath.Base(sessionDir)
			if session, err := readTasksDir(containerName, sessionDir, sessionID); err == nil && len(session.Tasks) > 0 {
				result.Sessions = append(result.Sessions, *session)
			}
		}
	}

	// Sort sessions by last update (most recent first)
	sort.Slice(result.Sessions, func(i, j int) bool {
		return result.Sessions[i].LastUpdate.After(result.Sessions[j].LastUpdate)
	})

	return result, nil
}

// readTasksDir reads individual task JSON files from a session directory (new Task* format)
// Structure: tasks/{session-uuid}/{id}.json where each file is a single task object
func readTasksDir(containerName, sessionDir, sessionID string) (*Session, error) {
	session := &Session{
		ID:    sessionID,
		Tasks: []Task{},
	}

	// List JSON files in the session directory (exclude .lock files)
	listCmd := exec.Command("docker", "exec", containerName,
		"find", sessionDir, "-maxdepth", "1", "-name", "*.json", "-type", "f")
	output, err := listCmd.Output()
	if err != nil {
		return nil, err
	}

	taskFiles := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, taskFile := range taskFiles {
		if taskFile == "" {
			continue
		}

		// Get file modification time
		var mtime time.Time
		statCmd := exec.Command("docker", "exec", containerName, "stat", "-c", "%Y", taskFile)
		if statOutput, err := statCmd.Output(); err == nil {
			if ts, err := strconv.ParseInt(strings.TrimSpace(string(statOutput)), 10, 64); err == nil {
				mtime = time.Unix(ts, 0)
			}
		}

		// Read file contents
		catCmd := exec.Command("docker", "exec", containerName, "cat", taskFile)
		content, err := catCmd.Output()
		if err != nil {
			continue
		}

		// Parse as single task object
		var task Task
		if err := json.Unmarshal(content, &task); err != nil {
			continue
		}

		session.Tasks = append(session.Tasks, task)
		if mtime.After(session.LastUpdate) {
			session.LastUpdate = mtime
		}
	}

	// Sort tasks by ID numerically
	sort.Slice(session.Tasks, func(i, j int) bool {
		idI, _ := strconv.Atoi(session.Tasks[i].ID)
		idJ, _ := strconv.Atoi(session.Tasks[j].ID)
		return idI < idJ
	})

	return session, nil
}

// readTodoFile reads a todo JSON file which contains an array of tasks (old TodoWrite format)
// File naming: {session-uuid}-agent-{agent-uuid}.json
func readTodoFile(containerName, filePath string) (*Session, error) {
	// Extract session ID from filename (first UUID before -agent-)
	filename := filepath.Base(filePath)
	sessionID := filename
	if idx := strings.Index(filename, "-agent-"); idx > 0 {
		sessionID = filename[:idx]
	}

	session := &Session{
		ID:    sessionID,
		Tasks: []Task{},
	}

	// Get file modification time
	statCmd := exec.Command("docker", "exec", containerName,
		"stat", "-c", "%Y", filePath)
	output, err := statCmd.Output()
	if err == nil {
		if ts, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64); err == nil {
			session.LastUpdate = time.Unix(ts, 0)
		}
	}

	// Read file contents
	catCmd := exec.Command("docker", "exec", containerName, "cat", filePath)
	output, err = catCmd.Output()
	if err != nil {
		return nil, err
	}

	// Parse as array of tasks
	var tasks []Task
	if err := json.Unmarshal(output, &tasks); err != nil {
		return nil, err
	}

	session.Tasks = tasks

	// Sort tasks by ID numerically
	sort.Slice(session.Tasks, func(i, j int) bool {
		idI, _ := strconv.Atoi(session.Tasks[i].ID)
		idJ, _ := strconv.Atoi(session.Tasks[j].ID)
		return idI < idJ
	})

	return session, nil
}

// GetAllContainerTasks reads task data from all running containers with the given prefix
func GetAllContainerTasks(containerPrefix string) ([]ContainerTasks, error) {
	containers, err := GetRunningContainers(containerPrefix)
	if err != nil {
		return nil, err
	}

	var results []ContainerTasks
	for _, c := range containers {
		tasks, err := GetContainerTasks(c.Name)
		if err != nil {
			results = append(results, ContainerTasks{
				ContainerName: c.Name,
				ShortName:     c.ShortName,
				Error:         err,
			})
			continue
		}
		results = append(results, *tasks)
	}

	return results, nil
}

// getShortName extracts the short name from a container name
func getShortName(containerName string) string {
	// Try common prefixes
	for _, prefix := range []string{"mcl-", "maestro-"} {
		if strings.HasPrefix(containerName, prefix) {
			return containerName[len(prefix):]
		}
	}
	return containerName
}

// GetActiveTask returns the currently in-progress task for a container, if any
func GetActiveTask(containerName string) (*Task, error) {
	tasks, err := GetContainerTasks(containerName)
	if err != nil {
		return nil, err
	}
	if tasks.Error != nil {
		return nil, tasks.Error
	}

	// Find the most recent session with an in-progress task
	for _, session := range tasks.Sessions {
		for _, task := range session.Tasks {
			if task.Status == TaskStatusInProgress {
				return &task, nil
			}
		}
	}

	return nil, nil
}

// TaskSummary holds a brief summary of task state for display
type TaskSummary struct {
	CurrentTask string // Display name of current in-progress task
	Progress    string // e.g., "2/5" (completed/total)
	HasTasks    bool   // Whether there are any tasks
}

// GetTaskSummary returns a brief task summary for a container (for list display)
func GetTaskSummary(containerName string) TaskSummary {
	summary := TaskSummary{}

	tasks, err := GetContainerTasks(containerName)
	if err != nil || tasks.Error != nil || len(tasks.Sessions) == 0 {
		return summary
	}

	// Use the most recent session
	session := tasks.Sessions[0]
	if len(session.Tasks) == 0 {
		return summary
	}

	summary.HasTasks = true

	// Count tasks by status
	var completed, total int
	for _, task := range session.Tasks {
		total++
		switch task.Status {
		case TaskStatusCompleted:
			completed++
		case TaskStatusInProgress:
			summary.CurrentTask = task.GetDisplayName()
		}
	}

	summary.Progress = fmt.Sprintf("%d/%d", completed, total)
	return summary
}
