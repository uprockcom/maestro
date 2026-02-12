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

package version

import (
	"strings"
	"testing"
)

func TestGetContainerImage(t *testing.T) {
	tests := []struct {
		name           string
		version        string
		expectedSuffix string
		shouldBeLatest bool
	}{
		{
			name:           "dev build",
			version:        "dev",
			shouldBeLatest: true,
		},
		{
			name:           "next snapshot",
			version:        "1.0.1-next",
			shouldBeLatest: true,
		},
		{
			name:           "snapshot build",
			version:        "1.0.0-snapshot",
			shouldBeLatest: true,
		},
		{
			name:           "release with v prefix",
			version:        "v1.2.3",
			expectedSuffix: ":1.2.3",
			shouldBeLatest: false,
		},
		{
			name:           "release without v prefix",
			version:        "1.2.3",
			expectedSuffix: ":1.2.3",
			shouldBeLatest: false,
		},
		{
			name:           "pre-release",
			version:        "2.0.0-rc1",
			expectedSuffix: ":2.0.0-rc1",
			shouldBeLatest: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original version
			origVersion := Version
			defer func() { Version = origVersion }()

			// Set test version
			Version = tt.version

			image := GetContainerImage()

			// Check base image
			if !strings.HasPrefix(image, "ghcr.io/uprockcom/maestro:") {
				t.Errorf("GetContainerImage() = %q, should start with 'ghcr.io/uprockcom/maestro:'", image)
			}

			// Check suffix
			if tt.shouldBeLatest {
				if !strings.HasSuffix(image, ":latest") {
					t.Errorf("GetContainerImage() = %q, should end with ':latest' for %s", image, tt.version)
				}
			} else if tt.expectedSuffix != "" {
				if !strings.HasSuffix(image, tt.expectedSuffix) {
					t.Errorf("GetContainerImage() = %q, should end with %q", image, tt.expectedSuffix)
				}
			}
		})
	}
}

func TestIsDevelopment(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		{"dev", "dev", true},
		{"next snapshot", "1.0.1-next", true},
		{"snapshot", "1.0.0-snapshot", true},
		{"release", "1.2.3", false},
		{"release with v", "v1.2.3", false},
		{"pre-release", "2.0.0-rc1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original version
			origVersion := Version
			defer func() { Version = origVersion }()

			Version = tt.version
			result := IsDevelopment()

			if result != tt.expected {
				t.Errorf("IsDevelopment() = %v, want %v for version %q", result, tt.expected, tt.version)
			}
		})
	}
}

func TestShort(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		commit   string
		expected string
	}{
		{
			name:     "dev with commit",
			version:  "dev",
			commit:   "abc123def456",
			expected: "dev-abc123d",
		},
		{
			name:     "dev without commit",
			version:  "dev",
			commit:   "none",
			expected: "dev",
		},
		{
			name:     "release version",
			version:  "1.2.3",
			commit:   "abc123",
			expected: "1.2.3",
		},
		{
			name:     "release with v prefix",
			version:  "v2.0.0",
			commit:   "xyz789",
			expected: "v2.0.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save originals
			origVersion := Version
			origCommit := Commit
			defer func() {
				Version = origVersion
				Commit = origCommit
			}()

			Version = tt.version
			Commit = tt.commit

			result := Short()
			if result != tt.expected {
				t.Errorf("Short() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestInfo(t *testing.T) {
	// Save originals
	origVersion := Version
	origCommit := Commit
	origDate := Date
	defer func() {
		Version = origVersion
		Commit = origCommit
		Date = origDate
	}()

	// Set test values
	Version = "1.2.3"
	Commit = "abc123"
	Date = "2025-01-15"

	info := Info()

	// Check that info contains expected components
	expectedParts := []string{
		"maestro version 1.2.3",
		"commit: abc123",
		"built: 2025-01-15",
		"go:",
		"container: ghcr.io/uprockcom/maestro:1.2.3",
	}

	for _, part := range expectedParts {
		if !strings.Contains(info, part) {
			t.Errorf("Info() should contain %q, got:\n%s", part, info)
		}
	}

	// Development build should have warning
	Version = "dev"
	devInfo := Info()
	if !strings.Contains(devInfo, "Development build") {
		t.Error("Info() for dev build should contain 'Development build' warning")
	}
}
