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
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/uprockcom/maestro/pkg/container"
	"github.com/uprockcom/maestro/pkg/notify"
)

// ContainerOps abstracts Docker container operations for testability.
type ContainerOps interface {
	GetRunningContainers(prefix string) ([]container.Info, error)
	IsRunning(containerName string) bool
	GetLabel(containerName, label string) string
	QueueMessage(containerName, message string) error
}

// dockerContainerOps is the real implementation that calls Docker.
type dockerContainerOps struct{}

func (d *dockerContainerOps) GetRunningContainers(prefix string) ([]container.Info, error) {
	return container.GetRunningContainers(prefix)
}

func (d *dockerContainerOps) IsRunning(containerName string) bool {
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Status}}", containerName)
	output, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(output)) != ""
}

func (d *dockerContainerOps) GetLabel(containerName, label string) string {
	return getContainerLabel(containerName, label)
}

func (d *dockerContainerOps) QueueMessage(containerName, message string) error {
	return container.QueueMessage(containerName, message)
}

// ListContainers returns summaries of all running containers, optionally filtered by project.
func (d *Daemon) ListContainers(ctx context.Context, project string) ([]notify.ContainerSummary, error) {
	prefix := d.config.ContainerPrefix
	if prefix == "" {
		prefix = "maestro-"
	}

	containers, err := d.containerOps.GetRunningContainers(prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var result []notify.ContainerSummary
	for _, c := range containers {
		// Get project label from Docker
		projLabel := d.containerOps.GetLabel(c.Name, "maestro.project")

		// Filter by project if specified
		if project != "" && projLabel != project {
			continue
		}

		// Determine status from agent state
		status := "working"
		if c.IsDormant {
			status = "dormant"
		} else {
			switch c.AgentState {
			case "question":
				status = "question"
			case "idle", "waiting":
				status = "idle"
			default:
				status = "working"
			}
		}

		// Look up nickname
		nickname := ""
		if d.nicknames != nil {
			if nick, ok := d.nicknames.GetByContainer(c.Name); ok {
				nickname = nick
			}
		}

		result = append(result, notify.ContainerSummary{
			Name:        c.Name,
			ShortName:   c.ShortName,
			Nickname:    nickname,
			Project:     projLabel,
			Branch:      c.Branch,
			Status:      status,
			Task:        c.CurrentTask,
			HasQuestion: status == "question",
			Contacts:    c.Contacts,
		})
	}

	return result, nil
}

// ResolveName resolves a container name or nickname to a full container name.
func (d *Daemon) ResolveName(ctx context.Context, input string) (*notify.ResolveResult, error) {
	prefix := d.config.ContainerPrefix
	if prefix == "" {
		prefix = "maestro-"
	}

	// Check nicknames first (exact match)
	if d.nicknames != nil {
		if containerName, ok := d.nicknames.Get(input); ok {
			nick := input
			return &notify.ResolveResult{
				Name:      containerName,
				ShortName: container.GetShortName(containerName, prefix),
				Nickname:  nick,
				Exact:     true,
			}, nil
		}
	}

	// Check exact short name
	fullName := prefix + input
	if d.containerOps.IsRunning(fullName) {
		nick := ""
		if d.nicknames != nil {
			nick, _ = d.nicknames.GetByContainer(fullName)
		}
		return &notify.ResolveResult{
			Name:      fullName,
			ShortName: input,
			Nickname:  nick,
			Exact:     true,
		}, nil
	}

	// Substring match against running containers
	containers, err := d.containerOps.GetRunningContainers(prefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var matches []container.Info
	lowerInput := strings.ToLower(input)
	for _, c := range containers {
		if strings.Contains(strings.ToLower(c.ShortName), lowerInput) || strings.Contains(strings.ToLower(c.Name), lowerInput) {
			matches = append(matches, c)
		}
		// Also check nicknames for substring
		if d.nicknames != nil {
			if nick, ok := d.nicknames.GetByContainer(c.Name); ok {
				if strings.Contains(strings.ToLower(nick), lowerInput) {
					// Avoid duplicates
					found := false
					for _, m := range matches {
						if m.Name == c.Name {
							found = true
							break
						}
					}
					if !found {
						matches = append(matches, c)
					}
				}
			}
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("no container matching %q", input)
	}
	if len(matches) > 1 {
		var names []string
		for _, m := range matches {
			names = append(names, m.ShortName)
		}
		return nil, fmt.Errorf("ambiguous name %q matches: %s", input, strings.Join(names, ", "))
	}

	nick := ""
	if d.nicknames != nil {
		nick, _ = d.nicknames.GetByContainer(matches[0].Name)
	}
	return &notify.ResolveResult{
		Name:      matches[0].Name,
		ShortName: matches[0].ShortName,
		Nickname:  nick,
		Exact:     false,
	}, nil
}

// SendToContainer queues a message for delivery to a container's Claude session.
func (d *Daemon) SendToContainer(ctx context.Context, containerName string, message string) error {
	return d.containerOps.QueueMessage(containerName, message)
}

// Broadcast sends a message to all running containers, optionally filtered by project.
func (d *Daemon) Broadcast(ctx context.Context, project string, message string) ([]string, error) {
	containers, err := d.ListContainers(ctx, project)
	if err != nil {
		return nil, err
	}

	var sent []string
	for _, c := range containers {
		if err := d.containerOps.QueueMessage(c.Name, message); err != nil {
			d.logInfo("broadcast: failed to queue message for %s: %v", c.ShortName, err)
			continue
		}
		sent = append(sent, c.ShortName)
	}

	return sent, nil
}

// CreateContainer creates a new container, optionally within a named project.
func (d *Daemon) CreateContainer(ctx context.Context, project string, task string) (string, error) {
	if d.config.CreateContainer == nil {
		return "", fmt.Errorf("container creation not available")
	}
	containerName, err := d.config.CreateContainer(CreateContainerOpts{Task: task})
	if err != nil {
		return "", err
	}
	return containerName, nil
}

// getContainerLabel reads a Docker label from a container.
func getContainerLabel(containerName, label string) string {
	return container.GetLabel(containerName, label)
}
