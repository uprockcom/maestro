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

package notify

// History operations are embedded in Engine (engine.go).
// This file documents the data model for future persistence.
//
// The in-memory []LogEntry in Engine is append-only during the daemon's
// lifetime. Each entry records:
//
//   - Direction: "outbound" (notification sent) or "inbound" (unsolicited message)
//   - Event: the notification payload (outbound only)
//   - SentAt: when the notification was dispatched
//   - Providers: which providers successfully delivered
//   - Response: user's answer to a question (if applicable)
//   - RespondedAt: when the answer arrived
//   - IncomingMessage: unsolicited user message (inbound only)
//
// Future: persist to ~/.maestro/.claude/notification-history.json for
// cross-restart history. The current data model supports this without changes.
