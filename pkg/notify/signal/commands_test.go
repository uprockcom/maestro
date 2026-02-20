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
	"testing"
)

func TestParseSignalCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    *SignalCommand
		wantNil bool
	}{
		// List commands
		{name: "list", input: "list", want: &SignalCommand{Type: CmdList}},
		{name: "ls", input: "ls", want: &SignalCommand{Type: CmdList}},
		{name: "status", input: "status", want: &SignalCommand{Type: CmdList}},
		{name: "LIST uppercase", input: "LIST", want: &SignalCommand{Type: CmdList}},
		{name: "Status mixed case", input: "Status", want: &SignalCommand{Type: CmdList}},

		// Broadcast
		{name: "broadcast @all", input: "@all fix the tests", want: &SignalCommand{Type: CmdBroadcast, Message: "fix the tests"}},
		{name: "broadcast @all empty", input: "@all ", wantNil: true},
		{name: "broadcast @ALL uppercase", input: "@ALL hello", want: &SignalCommand{Type: CmdBroadcast, Message: "hello"}},

		// Send
		{name: "send @name msg", input: "@auth fix login bug", want: &SignalCommand{Type: CmdSend, Target: "auth", Message: "fix login bug"}},
		{name: "send @name no msg", input: "@auth", wantNil: true},
		{name: "send @name empty msg", input: "@auth ", wantNil: true},

		// New
		{name: "new simple", input: "new add user auth", want: &SignalCommand{Type: CmdNew, Message: "add user auth"}},
		{name: "new with project", input: "new insight: add user auth", want: &SignalCommand{Type: CmdNew, Target: "insight", Message: "add user auth"}},
		{name: "new empty", input: "new ", wantNil: true},
		{name: "new only project", input: "new insight:", wantNil: true},

		// Not a command
		{name: "empty", input: "", wantNil: true},
		{name: "random text", input: "hello world", wantNil: true},
		{name: "number reply", input: "1", wantNil: true},
		{name: "freetext reply", input: "yes, go ahead", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSignalCommand(tt.input)
			if tt.wantNil {
				if got != nil {
					t.Errorf("ParseSignalCommand(%q) = %+v, want nil", tt.input, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("ParseSignalCommand(%q) = nil, want %+v", tt.input, tt.want)
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %d, want %d", got.Type, tt.want.Type)
			}
			if got.Target != tt.want.Target {
				t.Errorf("Target = %q, want %q", got.Target, tt.want.Target)
			}
			if got.Message != tt.want.Message {
				t.Errorf("Message = %q, want %q", got.Message, tt.want.Message)
			}
		})
	}
}
