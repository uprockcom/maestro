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
	"path/filepath"
	"testing"

	"github.com/uprockcom/maestro/pkg/container"
)

// mockContainerOps is a test double for ContainerOps.
type mockContainerOps struct {
	containers []container.Info
	labels     map[string]map[string]string // containerName → label → value
	running    map[string]bool              // containerName → isRunning
	queued     []queuedMessage
	queueErr   error
}

type queuedMessage struct {
	container string
	message   string
}

func (m *mockContainerOps) GetRunningContainers(prefix string) ([]container.Info, error) {
	return m.containers, nil
}

func (m *mockContainerOps) IsRunning(containerName string) bool {
	return m.running[containerName]
}

func (m *mockContainerOps) GetLabel(containerName, label string) string {
	if m.labels == nil {
		return ""
	}
	return m.labels[containerName][label]
}

func (m *mockContainerOps) QueueMessage(containerName, message string) error {
	if m.queueErr != nil {
		return m.queueErr
	}
	m.queued = append(m.queued, queuedMessage{containerName, message})
	return nil
}

// newTestDaemon creates a minimal Daemon suitable for testing commands.
func newTestDaemon(ops ContainerOps, nicknames *NicknameStore) *Daemon {
	return &Daemon{
		config:          Config{ContainerPrefix: "maestro-"},
		containerStates: make(map[string]*ContainerState),
		containerOps:    ops,
		nicknames:       nicknames,
	}
}

// --- ListContainers tests ---

func TestListContainers_Empty(t *testing.T) {
	ops := &mockContainerOps{}
	d := newTestDaemon(ops, nil)

	result, err := d.ListContainers(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 containers, got %d", len(result))
	}
}

func TestListContainers_Basic(t *testing.T) {
	ops := &mockContainerOps{
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1", Branch: "feat/auth"},
			{Name: "maestro-feat-db-1", ShortName: "feat-db-1", Branch: "feat/db"},
		},
	}
	d := newTestDaemon(ops, nil)

	result, err := d.ListContainers(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(result))
	}
	if result[0].ShortName != "feat-auth-1" {
		t.Errorf("expected feat-auth-1, got %q", result[0].ShortName)
	}
	if result[0].Status != "working" {
		t.Errorf("expected working status, got %q", result[0].Status)
	}
}

func TestListContainers_FilterByProject(t *testing.T) {
	ops := &mockContainerOps{
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1"},
			{Name: "maestro-feat-db-1", ShortName: "feat-db-1"},
		},
		labels: map[string]map[string]string{
			"maestro-feat-auth-1": {"maestro.project": "webapp"},
			"maestro-feat-db-1":   {"maestro.project": "backend"},
		},
	}
	d := newTestDaemon(ops, nil)

	result, err := d.ListContainers(context.Background(), "webapp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 container, got %d", len(result))
	}
	if result[0].ShortName != "feat-auth-1" {
		t.Errorf("expected feat-auth-1, got %q", result[0].ShortName)
	}
}

func TestListContainers_StatusDormant(t *testing.T) {
	ops := &mockContainerOps{
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1", IsDormant: true},
		},
	}
	d := newTestDaemon(ops, nil)

	result, err := d.ListContainers(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Status != "dormant" {
		t.Errorf("expected dormant, got %q", result[0].Status)
	}
}

func TestListContainers_StatusIdle(t *testing.T) {
	ops := &mockContainerOps{
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1", AgentState: "idle"},
		},
	}
	d := newTestDaemon(ops, nil)

	result, err := d.ListContainers(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Status != "idle" {
		t.Errorf("expected idle, got %q", result[0].Status)
	}
}

func TestListContainers_StatusQuestion(t *testing.T) {
	ops := &mockContainerOps{
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1", AgentState: "question"},
		},
	}
	d := newTestDaemon(ops, nil)

	result, err := d.ListContainers(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Status != "question" {
		t.Errorf("expected question, got %q", result[0].Status)
	}
	if !result[0].HasQuestion {
		t.Error("expected HasQuestion=true")
	}
}

func TestListContainers_WithNicknames(t *testing.T) {
	ops := &mockContainerOps{
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1"},
		},
	}
	tmp := filepath.Join(t.TempDir(), "nicks.yml")
	nicks := NewNicknameStore(tmp)
	nicks.Set("auth", "maestro-feat-auth-1")

	d := newTestDaemon(ops, nicks)

	result, err := d.ListContainers(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result[0].Nickname != "auth" {
		t.Errorf("expected nickname 'auth', got %q", result[0].Nickname)
	}
}

// --- ResolveName tests ---

func TestResolveName_ByNickname(t *testing.T) {
	ops := &mockContainerOps{}
	tmp := filepath.Join(t.TempDir(), "nicks.yml")
	nicks := NewNicknameStore(tmp)
	nicks.Set("auth", "maestro-feat-auth-1")

	d := newTestDaemon(ops, nicks)

	result, err := d.ResolveName(context.Background(), "auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "maestro-feat-auth-1" {
		t.Errorf("expected maestro-feat-auth-1, got %q", result.Name)
	}
	if result.Nickname != "auth" {
		t.Errorf("expected nickname 'auth', got %q", result.Nickname)
	}
	if !result.Exact {
		t.Error("expected Exact=true for nickname match")
	}
}

func TestResolveName_ByExactShortName(t *testing.T) {
	ops := &mockContainerOps{
		running: map[string]bool{
			"maestro-feat-auth-1": true,
		},
	}
	d := newTestDaemon(ops, nil)

	result, err := d.ResolveName(context.Background(), "feat-auth-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "maestro-feat-auth-1" {
		t.Errorf("expected maestro-feat-auth-1, got %q", result.Name)
	}
	if result.ShortName != "feat-auth-1" {
		t.Errorf("expected short name feat-auth-1, got %q", result.ShortName)
	}
	if !result.Exact {
		t.Error("expected Exact=true for exact short name")
	}
}

func TestResolveName_BySubstring(t *testing.T) {
	ops := &mockContainerOps{
		running: map[string]bool{},
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1"},
			{Name: "maestro-feat-db-1", ShortName: "feat-db-1"},
		},
	}
	d := newTestDaemon(ops, nil)

	result, err := d.ResolveName(context.Background(), "auth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "maestro-feat-auth-1" {
		t.Errorf("expected maestro-feat-auth-1, got %q", result.Name)
	}
	if result.Exact {
		t.Error("expected Exact=false for substring match")
	}
}

func TestResolveName_SubstringByNickname(t *testing.T) {
	ops := &mockContainerOps{
		running: map[string]bool{},
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1"},
		},
	}
	tmp := filepath.Join(t.TempDir(), "nicks.yml")
	nicks := NewNicknameStore(tmp)
	nicks.Set("authentication", "maestro-feat-auth-1")

	d := newTestDaemon(ops, nicks)

	result, err := d.ResolveName(context.Background(), "authen")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Name != "maestro-feat-auth-1" {
		t.Errorf("expected maestro-feat-auth-1, got %q", result.Name)
	}
}

func TestResolveName_Ambiguous(t *testing.T) {
	ops := &mockContainerOps{
		running: map[string]bool{},
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1"},
			{Name: "maestro-feat-auth-2", ShortName: "feat-auth-2"},
		},
	}
	d := newTestDaemon(ops, nil)

	_, err := d.ResolveName(context.Background(), "auth")
	if err == nil {
		t.Fatal("expected error for ambiguous match")
	}
	if got := err.Error(); !contains(got, "ambiguous") {
		t.Errorf("expected 'ambiguous' in error, got: %s", got)
	}
}

func TestResolveName_NotFound(t *testing.T) {
	ops := &mockContainerOps{
		running:    map[string]bool{},
		containers: []container.Info{},
	}
	d := newTestDaemon(ops, nil)

	_, err := d.ResolveName(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for no match")
	}
	if got := err.Error(); !contains(got, "no container matching") {
		t.Errorf("expected 'no container matching' in error, got: %s", got)
	}
}

// --- SendToContainer tests ---

func TestSendToContainer(t *testing.T) {
	ops := &mockContainerOps{}
	d := newTestDaemon(ops, nil)

	err := d.SendToContainer(context.Background(), "maestro-feat-auth-1", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ops.queued) != 1 {
		t.Fatalf("expected 1 queued message, got %d", len(ops.queued))
	}
	if ops.queued[0].container != "maestro-feat-auth-1" || ops.queued[0].message != "hello" {
		t.Errorf("unexpected queued message: %+v", ops.queued[0])
	}
}

func TestSendToContainer_Error(t *testing.T) {
	ops := &mockContainerOps{queueErr: fmt.Errorf("queue failed")}
	d := newTestDaemon(ops, nil)

	err := d.SendToContainer(context.Background(), "maestro-feat-auth-1", "hello")
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Broadcast tests ---

func TestBroadcast(t *testing.T) {
	ops := &mockContainerOps{
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1"},
			{Name: "maestro-feat-db-1", ShortName: "feat-db-1"},
		},
	}
	d := newTestDaemon(ops, nil)

	sent, err := d.Broadcast(context.Background(), "", "update all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(sent) != 2 {
		t.Errorf("expected 2 sent, got %d", len(sent))
	}
	if len(ops.queued) != 2 {
		t.Errorf("expected 2 queued messages, got %d", len(ops.queued))
	}
}

func TestBroadcast_PartialFailure(t *testing.T) {
	callCount := 0
	ops := &mockContainerOps{
		containers: []container.Info{
			{Name: "maestro-feat-auth-1", ShortName: "feat-auth-1"},
			{Name: "maestro-feat-db-1", ShortName: "feat-db-1"},
		},
	}
	// Use a custom daemon with logInfo to suppress log output
	d := newTestDaemon(ops, nil)

	// Override QueueMessage to fail on first call
	failOps := &failOnceOps{inner: ops, failOn: 0, callCount: &callCount}
	d.containerOps = failOps

	sent, err := d.Broadcast(context.Background(), "", "update all")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// First container fails, second succeeds
	if len(sent) != 1 {
		t.Errorf("expected 1 sent (partial), got %d", len(sent))
	}
}

// --- CreateContainer tests ---

func TestCreateContainer(t *testing.T) {
	ops := &mockContainerOps{}
	d := newTestDaemon(ops, nil)
	d.config.CreateContainer = func(opts CreateContainerOpts) (string, error) {
		return "maestro-new-container-1", nil
	}

	name, err := d.CreateContainer(context.Background(), "", "build feature X")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if name != "maestro-new-container-1" {
		t.Errorf("expected maestro-new-container-1, got %q", name)
	}
}

func TestCreateContainer_NotAvailable(t *testing.T) {
	ops := &mockContainerOps{}
	d := newTestDaemon(ops, nil)

	_, err := d.CreateContainer(context.Background(), "", "task")
	if err == nil {
		t.Fatal("expected error when CreateContainer callback is nil")
	}
}

// --- helpers ---

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// failOnceOps wraps mockContainerOps but fails QueueMessage on a specific call.
type failOnceOps struct {
	inner     *mockContainerOps
	failOn    int
	callCount *int
}

func (f *failOnceOps) GetRunningContainers(prefix string) ([]container.Info, error) {
	return f.inner.GetRunningContainers(prefix)
}

func (f *failOnceOps) IsRunning(containerName string) bool {
	return f.inner.IsRunning(containerName)
}

func (f *failOnceOps) GetLabel(containerName, label string) string {
	return f.inner.GetLabel(containerName, label)
}

func (f *failOnceOps) QueueMessage(containerName, message string) error {
	n := *f.callCount
	*f.callCount++
	if n == f.failOn {
		return fmt.Errorf("simulated failure")
	}
	return f.inner.QueueMessage(containerName, message)
}
