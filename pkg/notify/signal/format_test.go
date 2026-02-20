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

package signal

import (
	"strings"
	"testing"

	"github.com/uprockcom/maestro/pkg/notify"
)

func TestFormatContainerList_Empty(t *testing.T) {
	result := FormatContainerList(nil)
	if !strings.Contains(result, "No running containers") {
		t.Errorf("expected 'No running containers', got %q", result)
	}
}

func TestFormatContainerList_Multiple(t *testing.T) {
	containers := []notify.ContainerSummary{
		{ShortName: "feat-auth-1", Status: "working", Task: "implementing login"},
		{ShortName: "feat-db-1", Nickname: "db", Status: "idle"},
		{ShortName: "fix-bug-1", Status: "dormant"},
		{ShortName: "feat-ui-1", Status: "question", HasQuestion: true},
	}
	result := FormatContainerList(containers)

	if !strings.Contains(result, "4 container(s)") {
		t.Errorf("expected container count, got %q", result)
	}
	if !strings.Contains(result, "feat-auth-1") {
		t.Error("missing feat-auth-1")
	}
	if !strings.Contains(result, "db (feat-db-1)") {
		t.Error("expected nickname format 'db (feat-db-1)'")
	}
	if !strings.Contains(result, "implementing login") {
		t.Error("missing task text")
	}
	// Check status icons
	if !strings.Contains(result, "⚡") {
		t.Error("missing working icon")
	}
	if !strings.Contains(result, "⏸") {
		t.Error("missing idle icon")
	}
	if !strings.Contains(result, "💤") {
		t.Error("missing dormant icon")
	}
	if !strings.Contains(result, "❓") {
		t.Error("missing question icon")
	}
}

func TestFormatSendConfirmation(t *testing.T) {
	result := FormatSendConfirmation("feat-auth-1")
	if !strings.Contains(result, "feat-auth-1") {
		t.Errorf("expected container name, got %q", result)
	}
	if !strings.Contains(result, "queued") {
		t.Errorf("expected 'queued', got %q", result)
	}
}

func TestFormatBroadcastConfirmation(t *testing.T) {
	result := FormatBroadcastConfirmation([]string{"auth", "db"})
	if !strings.Contains(result, "2 container(s)") {
		t.Errorf("expected count, got %q", result)
	}
	if !strings.Contains(result, "auth") || !strings.Contains(result, "db") {
		t.Errorf("expected container names, got %q", result)
	}

	empty := FormatBroadcastConfirmation(nil)
	if !strings.Contains(empty, "no containers") {
		t.Errorf("expected 'no containers', got %q", empty)
	}
}

func TestFormatNewConfirmation(t *testing.T) {
	result := FormatNewConfirmation("maestro-feat-auth-1")
	if !strings.Contains(result, "maestro-feat-auth-1") {
		t.Errorf("expected container name, got %q", result)
	}
}

func TestFormatCommandError(t *testing.T) {
	err := &testError{"something went wrong"}
	result := FormatCommandError(err)
	if !strings.Contains(result, "something went wrong") {
		t.Errorf("expected error message, got %q", result)
	}
	if !strings.Contains(result, "[maestro]") {
		t.Errorf("expected [maestro] prefix, got %q", result)
	}
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
