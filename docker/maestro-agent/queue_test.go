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

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDrainQueue_Empty(t *testing.T) {
	setupTestDirs(t)

	msgs, err := DrainQueue()
	if err != nil {
		t.Fatalf("DrainQueue() error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("DrainQueue() returned %d messages, want 0", len(msgs))
	}
}

func TestDrainQueue_NoDirectory(t *testing.T) {
	setupTestDirs(t)
	os.RemoveAll(pendingMsgDir)

	msgs, err := DrainQueue()
	if err != nil {
		t.Fatalf("DrainQueue() error: %v", err)
	}
	if len(msgs) != 0 {
		t.Errorf("DrainQueue() returned %d messages, want 0", len(msgs))
	}
}

func TestDrainQueue_SingleMessage(t *testing.T) {
	setupTestDirs(t)

	ts := time.Now().UnixNano()
	filename := fmt.Sprintf("%d.txt", ts)
	os.WriteFile(filepath.Join(pendingMsgDir, filename), []byte("hello world"), 0644)

	msgs, err := DrainQueue()
	if err != nil {
		t.Fatalf("DrainQueue() error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("DrainQueue() returned %d messages, want 1", len(msgs))
	}
	if msgs[0].Content != "hello world" {
		t.Errorf("message content = %q, want %q", msgs[0].Content, "hello world")
	}

	// File should be removed
	if _, err := os.Stat(filepath.Join(pendingMsgDir, filename)); !os.IsNotExist(err) {
		t.Error("message file should be removed after drain")
	}
}

func TestDrainQueue_OrderedByTimestamp(t *testing.T) {
	setupTestDirs(t)

	// Write messages out of order
	ts1 := int64(1000000000)
	ts2 := int64(2000000000)
	ts3 := int64(3000000000)

	os.WriteFile(filepath.Join(pendingMsgDir, fmt.Sprintf("%d.txt", ts3)), []byte("third"), 0644)
	os.WriteFile(filepath.Join(pendingMsgDir, fmt.Sprintf("%d.txt", ts1)), []byte("first"), 0644)
	os.WriteFile(filepath.Join(pendingMsgDir, fmt.Sprintf("%d.txt", ts2)), []byte("second"), 0644)

	msgs, err := DrainQueue()
	if err != nil {
		t.Fatalf("DrainQueue() error: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("got %d messages, want 3", len(msgs))
	}
	if msgs[0].Content != "first" || msgs[1].Content != "second" || msgs[2].Content != "third" {
		t.Errorf("messages not in order: %q, %q, %q", msgs[0].Content, msgs[1].Content, msgs[2].Content)
	}
}

func TestDrainQueue_IgnoresNonTxtFiles(t *testing.T) {
	setupTestDirs(t)

	os.WriteFile(filepath.Join(pendingMsgDir, "1000.txt"), []byte("real message"), 0644)
	os.WriteFile(filepath.Join(pendingMsgDir, "1000.json"), []byte("not a message"), 0644)
	os.Mkdir(filepath.Join(pendingMsgDir, "subdir"), 0755)

	msgs, err := DrainQueue()
	if err != nil {
		t.Fatalf("DrainQueue() error: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("got %d messages, want 1", len(msgs))
	}
}

func TestHasQueuedMessages_Empty(t *testing.T) {
	setupTestDirs(t)
	if HasQueuedMessages() {
		t.Error("HasQueuedMessages() = true, want false")
	}
}

func TestHasQueuedMessages_WithMessages(t *testing.T) {
	setupTestDirs(t)
	os.WriteFile(filepath.Join(pendingMsgDir, "1000.txt"), []byte("msg"), 0644)

	if !HasQueuedMessages() {
		t.Error("HasQueuedMessages() = false, want true")
	}
}

func TestFormatMessages_Empty(t *testing.T) {
	if got := FormatMessages(nil); got != "" {
		t.Errorf("FormatMessages(nil) = %q, want empty", got)
	}
}

func TestFormatMessages_Single(t *testing.T) {
	ts := time.Date(2026, 2, 17, 12, 0, 0, 0, time.UTC)
	msgs := []QueuedMessage{{Content: "hello", Timestamp: ts}}

	got := FormatMessages(msgs)

	if !strings.Contains(got, "=== MESSAGE SOURCE: queue (1 message) ===") {
		t.Errorf("missing singular header in:\n%s", got)
	}
	if !strings.Contains(got, "--- Message 1") {
		t.Error("missing message delimiter")
	}
	if !strings.Contains(got, "hello") {
		t.Error("missing message content")
	}
	if !strings.Contains(got, "=== END MESSAGES ===") {
		t.Error("missing end marker")
	}
}

func TestFormatMessages_Plural(t *testing.T) {
	ts := time.Now()
	msgs := []QueuedMessage{
		{Content: "first", Timestamp: ts},
		{Content: "second", Timestamp: ts},
	}

	got := FormatMessages(msgs)

	if !strings.Contains(got, "(2 messages)") {
		t.Errorf("missing plural header in:\n%s", got)
	}
	if !strings.Contains(got, "--- Message 1") || !strings.Contains(got, "--- Message 2") {
		t.Error("missing message delimiters")
	}
}

func TestFormatTrigger(t *testing.T) {
	got := FormatTrigger("watch-repo.sh", "new commit abc123")

	if !strings.Contains(got, "=== TRIGGER: watch-repo.sh ===") {
		t.Error("missing trigger header")
	}
	if !strings.Contains(got, "new commit abc123") {
		t.Error("missing trigger content")
	}
	if !strings.Contains(got, "=== END TRIGGER ===") {
		t.Error("missing end marker")
	}
}

func TestParseTimestampFilename(t *testing.T) {
	ts, err := parseTimestampFilename("1700000000000000000")
	if err != nil {
		t.Fatalf("parseTimestampFilename() error: %v", err)
	}
	if ts.Year() != 2023 {
		t.Errorf("parsed year = %d, want 2023", ts.Year())
	}
}

func TestParseTimestampFilename_Invalid(t *testing.T) {
	_, err := parseTimestampFilename("not-a-number")
	if err == nil {
		t.Error("parseTimestampFilename(\"not-a-number\") should error")
	}
}
