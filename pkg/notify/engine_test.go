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

import (
	"testing"
)

func TestShouldSendTo_NoOverride(t *testing.T) {
	engine := NewEngine(nil, nil, func(string, ...interface{}) {}, nil)

	// No per-provider override → always true
	if !engine.shouldSendTo("desktop", "question") {
		t.Error("expected true for desktop/question with no override")
	}
	if !engine.shouldSendTo("signal", "attention_needed") {
		t.Error("expected true for signal/attention_needed with no override")
	}
}

func TestShouldSendTo_WithOverride(t *testing.T) {
	providerNotifyOn := map[string][]string{
		"signal": {"question", "blocker"},
	}
	engine := NewEngine(nil, nil, func(string, ...interface{}) {}, providerNotifyOn)

	// Signal: only question and blocker allowed
	if !engine.shouldSendTo("signal", "question") {
		t.Error("expected true for signal/question")
	}
	if !engine.shouldSendTo("signal", "blocker") {
		t.Error("expected true for signal/blocker")
	}
	if engine.shouldSendTo("signal", "attention_needed") {
		t.Error("expected false for signal/attention_needed")
	}
	if engine.shouldSendTo("signal", "tasks_completed") {
		t.Error("expected false for signal/tasks_completed")
	}

	// Desktop: no override → inherits global → true for everything
	if !engine.shouldSendTo("desktop", "attention_needed") {
		t.Error("expected true for desktop/attention_needed (no override)")
	}
}

func TestShouldSendTo_EmptyOverride(t *testing.T) {
	providerNotifyOn := map[string][]string{
		"signal": {},
	}
	engine := NewEngine(nil, nil, func(string, ...interface{}) {}, providerNotifyOn)

	// Empty list means nothing is allowed
	if engine.shouldSendTo("signal", "question") {
		t.Error("expected false for signal/question with empty override")
	}
}
