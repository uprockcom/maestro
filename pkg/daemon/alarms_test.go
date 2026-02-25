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
	"testing"
	"time"
)

func TestAlarmStore_AddAndList(t *testing.T) {
	store := NewAlarmStore()

	alarm := &Alarm{
		ID:            "test-1",
		ContainerName: "maestro-test-1",
		Name:          "my-alarm",
		Message:       "hello",
		FireAt:        time.Now().Add(1 * time.Hour),
		CreatedAt:     time.Now(),
	}

	store.Add(alarm)

	alarms := store.ListForContainer("maestro-test-1")
	if len(alarms) != 1 {
		t.Fatalf("expected 1 alarm, got %d", len(alarms))
	}
	if alarms[0].Name != "my-alarm" {
		t.Errorf("expected alarm name 'my-alarm', got %q", alarms[0].Name)
	}

	// Different container should have no alarms
	alarms = store.ListForContainer("maestro-other-1")
	if len(alarms) != 0 {
		t.Fatalf("expected 0 alarms for other container, got %d", len(alarms))
	}
}

func TestAlarmStore_CancelByID(t *testing.T) {
	store := NewAlarmStore()

	store.Add(&Alarm{
		ID:            "test-1",
		ContainerName: "maestro-test-1",
		Name:          "my-alarm",
		FireAt:        time.Now().Add(1 * time.Hour),
		CreatedAt:     time.Now(),
	})

	if !store.Cancel("test-1") {
		t.Error("expected Cancel to return true for existing alarm")
	}
	if store.Cancel("test-1") {
		t.Error("expected Cancel to return false for already-cancelled alarm")
	}

	alarms := store.ListForContainer("maestro-test-1")
	if len(alarms) != 0 {
		t.Fatalf("expected 0 alarms after cancel, got %d", len(alarms))
	}
}

func TestAlarmStore_CancelByName(t *testing.T) {
	store := NewAlarmStore()

	store.Add(&Alarm{
		ID:            "test-1",
		ContainerName: "maestro-test-1",
		Name:          "my-alarm",
		FireAt:        time.Now().Add(1 * time.Hour),
		CreatedAt:     time.Now(),
	})
	store.Add(&Alarm{
		ID:            "test-2",
		ContainerName: "maestro-test-1",
		Name:          "other-alarm",
		FireAt:        time.Now().Add(2 * time.Hour),
		CreatedAt:     time.Now(),
	})

	if !store.CancelByName("maestro-test-1", "my-alarm") {
		t.Error("expected CancelByName to return true")
	}

	alarms := store.ListForContainer("maestro-test-1")
	if len(alarms) != 1 {
		t.Fatalf("expected 1 alarm remaining, got %d", len(alarms))
	}
	if alarms[0].Name != "other-alarm" {
		t.Errorf("expected remaining alarm 'other-alarm', got %q", alarms[0].Name)
	}
}

func TestAlarmStore_CheckAndFire(t *testing.T) {
	store := NewAlarmStore()

	past := time.Now().Add(-1 * time.Minute)
	future := time.Now().Add(1 * time.Hour)

	store.Add(&Alarm{
		ID:            "past-1",
		ContainerName: "maestro-test-1",
		Name:          "past-alarm",
		FireAt:        past,
		CreatedAt:     time.Now(),
	})
	store.Add(&Alarm{
		ID:            "future-1",
		ContainerName: "maestro-test-1",
		Name:          "future-alarm",
		FireAt:        future,
		CreatedAt:     time.Now(),
	})

	fired := store.CheckAndFire(time.Now())
	if len(fired) != 1 {
		t.Fatalf("expected 1 fired alarm, got %d", len(fired))
	}
	if fired[0].ID != "past-1" {
		t.Errorf("expected fired alarm 'past-1', got %q", fired[0].ID)
	}

	// Check again — should not fire twice
	fired = store.CheckAndFire(time.Now())
	if len(fired) != 0 {
		t.Fatalf("expected 0 re-fired alarms, got %d", len(fired))
	}

	// Future alarm should still be listed
	alarms := store.ListForContainer("maestro-test-1")
	if len(alarms) != 1 {
		t.Fatalf("expected 1 pending alarm, got %d", len(alarms))
	}
}

func TestAlarmStore_CleanupContainer(t *testing.T) {
	store := NewAlarmStore()

	store.Add(&Alarm{
		ID:            "test-1",
		ContainerName: "maestro-test-1",
		Name:          "alarm-1",
		FireAt:        time.Now().Add(1 * time.Hour),
		CreatedAt:     time.Now(),
	})
	store.Add(&Alarm{
		ID:            "test-2",
		ContainerName: "maestro-test-1",
		Name:          "alarm-2",
		FireAt:        time.Now().Add(2 * time.Hour),
		CreatedAt:     time.Now(),
	})
	store.Add(&Alarm{
		ID:            "test-3",
		ContainerName: "maestro-other-1",
		Name:          "alarm-3",
		FireAt:        time.Now().Add(1 * time.Hour),
		CreatedAt:     time.Now(),
	})

	store.CleanupContainer("maestro-test-1")

	if store.Count() != 1 {
		t.Fatalf("expected 1 remaining alarm, got %d", store.Count())
	}

	alarms := store.ListForContainer("maestro-other-1")
	if len(alarms) != 1 {
		t.Fatalf("expected 1 alarm for other container, got %d", len(alarms))
	}
}

func TestAlarmStore_Count(t *testing.T) {
	store := NewAlarmStore()

	if store.Count() != 0 {
		t.Fatalf("expected 0 count for empty store, got %d", store.Count())
	}

	store.Add(&Alarm{
		ID:            "test-1",
		ContainerName: "maestro-test-1",
		Name:          "alarm-1",
		FireAt:        time.Now().Add(1 * time.Hour),
		CreatedAt:     time.Now(),
	})
	store.Add(&Alarm{
		ID:            "test-2",
		ContainerName: "maestro-test-1",
		Name:          "alarm-2",
		FireAt:        time.Now().Add(2 * time.Hour),
		CreatedAt:     time.Now(),
	})

	if store.Count() != 2 {
		t.Fatalf("expected 2 count, got %d", store.Count())
	}
}

func TestFormatAlarmMessage(t *testing.T) {
	alarm := &Alarm{
		ID:      "test-1",
		Name:    "check-build",
		Message: "Check if the build finished",
		FireAt:  time.Date(2026, 2, 25, 15, 30, 0, 0, time.UTC),
	}

	msg := formatAlarmMessage(alarm)

	if msg == "" {
		t.Fatal("expected non-empty message")
	}

	// Should contain trigger markers
	if !contains(msg, "=== TRIGGER: alarm ===") {
		t.Error("expected TRIGGER header")
	}
	if !contains(msg, "=== END TRIGGER ===") {
		t.Error("expected END TRIGGER footer")
	}
	if !contains(msg, "check-build") {
		t.Error("expected alarm name in message")
	}
	if !contains(msg, "Check if the build finished") {
		t.Error("expected alarm message in output")
	}
}

func TestAlarmStore_CancelByName_Multiple(t *testing.T) {
	store := NewAlarmStore()

	// Two alarms with the same name in the same container.
	store.Add(&Alarm{
		ID:            "test-1",
		ContainerName: "maestro-test-1",
		Name:          "my-alarm",
		FireAt:        time.Now().Add(30 * time.Minute),
		CreatedAt:     time.Now(),
	})
	store.Add(&Alarm{
		ID:            "test-2",
		ContainerName: "maestro-test-1",
		Name:          "my-alarm",
		FireAt:        time.Now().Add(45 * time.Minute),
		CreatedAt:     time.Now(),
	})
	// A third alarm with a different name should not be affected.
	store.Add(&Alarm{
		ID:            "test-3",
		ContainerName: "maestro-test-1",
		Name:          "other-alarm",
		FireAt:        time.Now().Add(1 * time.Hour),
		CreatedAt:     time.Now(),
	})

	if !store.CancelByName("maestro-test-1", "my-alarm") {
		t.Error("expected CancelByName to return true when multiple alarms share the same name")
	}

	alarms := store.ListForContainer("maestro-test-1")
	if len(alarms) != 2 {
		t.Fatalf("expected 2 alarms remaining after cancelling one by name, got %d", len(alarms))
	}

	foundIDs := make(map[string]bool)
	for _, a := range alarms {
		foundIDs[a.ID] = true
	}

	if foundIDs["test-1"] && foundIDs["test-2"] {
		t.Error("expected one of the matching alarms to be cancelled, but both are still present")
	}
	if !foundIDs["test-3"] {
		t.Error("expected non-matching alarm 'test-3' to remain after cancelling by name")
	}
}

func TestAlarmStore_CleanupFired(t *testing.T) {
	store := NewAlarmStore()

	now := time.Now()
	oldFiredAt := now.Add(-2 * time.Hour)
	recentFiredAt := now.Add(-30 * time.Minute)

	// Alarm that fired long ago and should be cleaned up.
	store.Add(&Alarm{
		ID:            "old-fired",
		ContainerName: "maestro-test-1",
		Name:          "old-fired-alarm",
		FireAt:        now.Add(-3 * time.Hour),
		CreatedAt:     now.Add(-4 * time.Hour),
		Fired:         true,
		FiredAt:       &oldFiredAt,
	})

	// Alarm that fired recently and should be retained.
	store.Add(&Alarm{
		ID:            "recent-fired",
		ContainerName: "maestro-test-1",
		Name:          "recent-fired-alarm",
		FireAt:        now.Add(-45 * time.Minute),
		CreatedAt:     now.Add(-1 * time.Hour),
		Fired:         true,
		FiredAt:       &recentFiredAt,
	})

	// Alarm that has not yet fired and must not be removed.
	store.Add(&Alarm{
		ID:            "future",
		ContainerName: "maestro-test-1",
		Name:          "future-alarm",
		FireAt:        now.Add(30 * time.Minute),
		CreatedAt:     now,
	})

	// Remove fired alarms older than 1 hour.
	store.CleanupFired(1 * time.Hour)

	// old-fired should be removed; recent-fired and future should remain
	alarms := store.ListForContainer("maestro-test-1")
	// ListForContainer only returns unfired alarms, so only future shows
	if len(alarms) != 1 {
		t.Fatalf("expected 1 pending alarm after cleanup, got %d", len(alarms))
	}
	if alarms[0].ID != "future" {
		t.Errorf("expected remaining pending alarm to be 'future', got %q", alarms[0].ID)
	}

	// Check that Has still finds recent-fired (it was retained)
	if !store.Has("recent-fired") {
		t.Error("expected recently-fired alarm to be retained")
	}
	if store.Has("old-fired") {
		t.Error("expected old-fired alarm to be cleaned up")
	}
}

func TestAlarmStore_Has(t *testing.T) {
	store := NewAlarmStore()

	if store.Has("nonexistent") {
		t.Error("expected Has to return false for nonexistent alarm")
	}

	store.Add(&Alarm{
		ID:            "test-1",
		ContainerName: "maestro-test-1",
		Name:          "alarm-1",
		FireAt:        time.Now().Add(1 * time.Hour),
		CreatedAt:     time.Now(),
	})

	if !store.Has("test-1") {
		t.Error("expected Has to return true for existing alarm")
	}

	store.Cancel("test-1")
	if store.Has("test-1") {
		t.Error("expected Has to return false after cancel")
	}
}

// contains and searchString helpers are defined in commands_test.go
