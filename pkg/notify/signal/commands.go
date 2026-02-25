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
	"context"
	"strings"
)

// SignalCommandType identifies the kind of Signal command.
type SignalCommandType int

const (
	CmdList SignalCommandType = iota
	CmdSend
	CmdBroadcast
	CmdNew
)

// SignalCommand is a parsed Signal text command.
type SignalCommand struct {
	Type    SignalCommandType
	Target  string // name/nick for send, project for broadcast
	Message string
}

// ParseSignalCommand parses a Signal message into a command, or returns nil
// if the message is not a recognized command.
func ParseSignalCommand(text string) *SignalCommand {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	lower := strings.ToLower(text)

	// list / ls / status
	if lower == "list" || lower == "ls" || lower == "status" {
		return &SignalCommand{Type: CmdList}
	}

	// new [project:] <task>
	if strings.HasPrefix(lower, "new ") {
		rest := strings.TrimSpace(text[4:])
		var project string
		if idx := strings.Index(rest, ":"); idx > 0 && idx < 30 && !strings.Contains(rest[:idx], " ") {
			project = rest[:idx]
			rest = strings.TrimSpace(rest[idx+1:])
		}
		if rest != "" {
			return &SignalCommand{Type: CmdNew, Target: project, Message: rest}
		}
		return nil
	}

	// @all <msg> → broadcast
	if strings.HasPrefix(lower, "@all ") {
		msg := strings.TrimSpace(text[5:])
		if msg != "" {
			return &SignalCommand{Type: CmdBroadcast, Message: msg}
		}
		return nil
	}

	// @<name> <msg> → send (or broadcast if name matches a project)
	if strings.HasPrefix(text, "@") {
		rest := text[1:]
		parts := strings.SplitN(rest, " ", 2)
		if len(parts) == 2 && parts[1] != "" {
			return &SignalCommand{
				Type:    CmdSend,
				Target:  parts[0],
				Message: strings.TrimSpace(parts[1]),
			}
		}
		return nil
	}

	return nil
}

// executeCommand runs a parsed command against the CommandHandler and sends
// the result back via Signal. Results are filtered to containers visible to
// this provider's recipient.
func (s *SignalProvider) executeCommand(cmd *SignalCommand) {
	ctx := context.Background()
	var reply string

	switch cmd.Type {
	case CmdList:
		containers, err := s.commands.ListContainers(ctx, cmd.Target)
		if err != nil {
			reply = FormatCommandError(err)
		} else {
			visible := s.filterByContacts(containers)
			reply = FormatContainerList(visible)
		}

	case CmdSend:
		resolved, err := s.commands.ResolveName(ctx, cmd.Target)
		if err != nil {
			reply = FormatCommandError(err)
		} else {
			if err := s.commands.SendToContainer(ctx, resolved.Name, cmd.Message); err != nil {
				reply = FormatCommandError(err)
			} else {
				reply = FormatSendConfirmation(resolved.ShortName)
			}
		}

	case CmdBroadcast:
		sent, err := s.commands.Broadcast(ctx, cmd.Target, cmd.Message)
		if err != nil {
			reply = FormatCommandError(err)
		} else {
			reply = FormatBroadcastConfirmation(sent)
		}

	case CmdNew:
		containerName, err := s.commands.CreateContainer(ctx, cmd.Target, cmd.Message)
		if err != nil {
			reply = FormatCommandError(err)
		} else {
			reply = FormatNewConfirmation(containerName)
		}
	}

	if reply != "" {
		if _, err := s.api.SendMessage(s.recipient, reply); err != nil {
			s.logger("signal: failed to send command reply: %v", err)
		}
	}
}
