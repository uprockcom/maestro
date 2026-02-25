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

package paths

import (
	"os"
	"path/filepath"
	"runtime"
)

// GetConfigDir returns the platform-appropriate configuration directory.
// On Unix/macOS: ~/.maestro
// On Windows: %APPDATA%\maestro
func GetConfigDir() string {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" {
			// Fallback if APPDATA not set
			appData = filepath.Join(os.Getenv("USERPROFILE"), "AppData", "Roaming")
		}
		return filepath.Join(appData, "maestro")
	}

	// macOS and Linux
	home, err := os.UserHomeDir()
	if err != nil {
		// This should rarely happen, but provide a fallback
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".maestro")
}

// ConfigFile returns the path to the main configuration file.
// Unix/macOS: ~/.maestro/config.yml
// Windows: %APPDATA%\maestro\config.yml
func ConfigFile() string {
	return filepath.Join(GetConfigDir(), "config.yml")
}

// AuthDir returns the path to the Claude authentication directory.
// Unix/macOS: ~/.maestro/.claude
// Windows: %APPDATA%\maestro\.claude
func AuthDir() string {
	return filepath.Join(GetConfigDir(), ".claude")
}

// GitHubAuthDir returns the path to the GitHub CLI authentication directory.
// Unix/macOS: ~/.maestro/gh
// Windows: %APPDATA%\maestro\gh
func GitHubAuthDir() string {
	return filepath.Join(GetConfigDir(), "gh")
}

// CertificatesDir returns the path to the SSL certificates directory.
// Unix/macOS: ~/.maestro/certificates
// Windows: %APPDATA%\maestro\certificates
func CertificatesDir() string {
	return filepath.Join(GetConfigDir(), "certificates")
}

// LegacyConfigFile returns the old config file path for migration detection.
// Returns empty string on Windows (no legacy path on Windows).
func LegacyConfigFile() string {
	if runtime.GOOS == "windows" {
		return ""
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".mcl.yml")
}

// LegacyConfigDir returns the old config directory path for migration detection.
// Returns empty string on Windows (no legacy path on Windows).
func LegacyConfigDir() string {
	if runtime.GOOS == "windows" {
		return ""
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".mcl")
}

// HasLegacyConfig checks if old configuration exists (pre-1.0 paths).
// Only relevant on Unix/macOS.
func HasLegacyConfig() bool {
	if runtime.GOOS == "windows" {
		return false
	}

	legacyFile := LegacyConfigFile()
	legacyDir := LegacyConfigDir()

	if legacyFile != "" {
		if _, err := os.Stat(legacyFile); err == nil {
			return true
		}
	}

	if legacyDir != "" {
		if _, err := os.Stat(legacyDir); err == nil {
			return true
		}
	}

	return false
}

// EnsureConfigDir creates the configuration directory if it doesn't exist.
func EnsureConfigDir() error {
	return os.MkdirAll(GetConfigDir(), 0755)
}

// EnsureAuthDir creates the authentication directory if it doesn't exist.
func EnsureAuthDir() error {
	return os.MkdirAll(AuthDir(), 0755)
}
