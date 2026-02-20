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

package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// User represents an authenticated API user.
type User struct {
	Name      string `json:"name"`
	KeyHash   string `json:"key_hash"`
	Recipient string `json:"recipient"` // user's phone number for message routing
}

type keysFile struct {
	Users []User `json:"users"`
}

type contextKey string

const userContextKey contextKey = "user"

// KeyStore loads and validates API keys from keys.json.
type KeyStore struct {
	users []User
}

// LoadKeyStore reads and parses the keys file.
func LoadKeyStore(path string) (*KeyStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read keys file: %w", err)
	}
	var kf keysFile
	if err := json.Unmarshal(data, &kf); err != nil {
		return nil, fmt.Errorf("failed to parse keys file: %w", err)
	}
	if len(kf.Users) == 0 {
		return nil, fmt.Errorf("keys file contains no users")
	}
	return &KeyStore{users: kf.Users}, nil
}

// Validate checks a raw API key against stored hashes using constant-time
// comparison. Returns the matching user or nil.
func (ks *KeyStore) Validate(rawKey string) *User {
	h := sha256.Sum256([]byte(rawKey))
	keyHash := hex.EncodeToString(h[:])

	for i := range ks.users {
		storedHash, err := hex.DecodeString(ks.users[i].KeyHash)
		if err != nil {
			continue
		}
		computedHash, err := hex.DecodeString(keyHash)
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare(storedHash, computedHash) == 1 {
			return &ks.users[i]
		}
	}
	return nil
}

// AuthMiddleware returns HTTP middleware that validates the X-API-Key header.
func AuthMiddleware(ks *KeyStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				http.Error(w, `{"error":"missing API key"}`, http.StatusUnauthorized)
				return
			}
			user := ks.Validate(apiKey)
			if user == nil {
				http.Error(w, `{"error":"invalid API key"}`, http.StatusForbidden)
				return
			}
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UserFromContext extracts the authenticated user from the request context.
func UserFromContext(ctx context.Context) *User {
	u, _ := ctx.Value(userContextKey).(*User)
	return u
}
