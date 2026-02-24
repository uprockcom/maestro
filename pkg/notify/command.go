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

package notify

import "context"

// ContainerSummary provides a snapshot of a container's state for command responses.
type ContainerSummary struct {
	Name        string
	ShortName   string
	Nickname    string
	Project     string
	Branch      string
	Status      string // "working", "idle", "dormant", "question"
	Task        string
	HasQuestion bool
	Contacts    map[string]map[string]string // provider → key → value (from maestro.contacts label)
}

// ResolveResult holds the result of resolving a container name or nickname.
type ResolveResult struct {
	Name      string
	ShortName string
	Nickname  string
	Exact     bool
}

// CommandHandler defines provider-agnostic operations for managing containers.
// The daemon implements this interface; providers call it to execute commands.
type CommandHandler interface {
	ListContainers(ctx context.Context, project string) ([]ContainerSummary, error)
	ResolveName(ctx context.Context, input string) (*ResolveResult, error)
	SendToContainer(ctx context.Context, containerName string, message string) error
	Broadcast(ctx context.Context, project string, message string) (sent []string, err error)
	CreateContainer(ctx context.Context, project string, task string) (containerName string, err error)
}
