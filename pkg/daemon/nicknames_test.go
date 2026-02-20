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
	"os"
	"path/filepath"
	"testing"
)

func TestNicknameStore_SetAndGet(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "nicknames.yml")
	store := NewNicknameStore(tmp)

	// Set
	if err := store.Set("auth", "maestro-feat-auth-1"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get
	name, ok := store.Get("auth")
	if !ok || name != "maestro-feat-auth-1" {
		t.Errorf("Get(auth) = %q, %v; want maestro-feat-auth-1, true", name, ok)
	}

	// Get missing
	_, ok = store.Get("missing")
	if ok {
		t.Error("Get(missing) should return false")
	}
}

func TestNicknameStore_GetByContainer(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "nicknames.yml")
	store := NewNicknameStore(tmp)

	store.Set("auth", "maestro-feat-auth-1")
	store.Set("db", "maestro-feat-db-1")

	nick, ok := store.GetByContainer("maestro-feat-auth-1")
	if !ok || nick != "auth" {
		t.Errorf("GetByContainer = %q, %v; want auth, true", nick, ok)
	}

	_, ok = store.GetByContainer("maestro-feat-unknown-1")
	if ok {
		t.Error("GetByContainer for unknown should return false")
	}
}

func TestNicknameStore_Delete(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "nicknames.yml")
	store := NewNicknameStore(tmp)

	store.Set("auth", "maestro-feat-auth-1")
	store.Delete("auth")

	_, ok := store.Get("auth")
	if ok {
		t.Error("Get after Delete should return false")
	}
}

func TestNicknameStore_All(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "nicknames.yml")
	store := NewNicknameStore(tmp)

	store.Set("auth", "maestro-feat-auth-1")
	store.Set("db", "maestro-feat-db-1")

	all := store.All()
	if len(all) != 2 {
		t.Errorf("All() returned %d entries, want 2", len(all))
	}
	if all["auth"] != "maestro-feat-auth-1" {
		t.Errorf("auth = %q", all["auth"])
	}
	if all["db"] != "maestro-feat-db-1" {
		t.Errorf("db = %q", all["db"])
	}

	// Verify returned map is a copy (not mutable reference)
	all["extra"] = "should-not-persist"
	if _, ok := store.Get("extra"); ok {
		t.Error("All() should return a copy, not the internal map")
	}
}

func TestNicknameStore_Persistence(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "nicknames.yml")

	// Write with one store
	store1 := NewNicknameStore(tmp)
	store1.Set("auth", "maestro-feat-auth-1")

	// Read with a new store
	store2 := NewNicknameStore(tmp)
	name, ok := store2.Get("auth")
	if !ok || name != "maestro-feat-auth-1" {
		t.Errorf("Persisted Get(auth) = %q, %v; want maestro-feat-auth-1, true", name, ok)
	}
}

func TestNicknameStore_LoadNonexistent(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "does-not-exist.yml")
	store := NewNicknameStore(tmp)

	// Should be empty, not error
	all := store.All()
	if len(all) != 0 {
		t.Errorf("expected empty store, got %d entries", len(all))
	}
}

func TestNicknameStore_LoadCorrupt(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "corrupt.yml")
	os.WriteFile(tmp, []byte("{{not valid yaml"), 0644)

	store := NewNicknameStore(tmp)
	all := store.All()
	if len(all) != 0 {
		t.Errorf("expected empty store from corrupt file, got %d entries", len(all))
	}
}
