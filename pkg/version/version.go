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
	"fmt"
	"runtime"
	"strings"
)

var (
	// Version is the current version of maestro.
	// Set via -ldflags at build time: -X github.com/uprockcom/maestro/pkg/version.Version=1.2.3
	Version = "dev"

	// Commit is the git commit hash.
	// Set via -ldflags at build time: -X github.com/uprockcom/maestro/pkg/version.Commit=abc123
	Commit = "none"

	// Date is the build date.
	// Set via -ldflags at build time: -X github.com/uprockcom/maestro/pkg/version.Date=2025-01-15
	Date = "unknown"

	// BuiltBy indicates who/what built the binary.
	// Set via -ldflags at build time: -X github.com/uprockcom/maestro/pkg/version.BuiltBy=goreleaser
	BuiltBy = "unknown"
)

// Info returns formatted version information for display.
func Info() string {
	var builder strings.Builder

	builder.WriteString(fmt.Sprintf("maestro version %s\n", Version))
	builder.WriteString(fmt.Sprintf("  commit: %s\n", Commit))
	builder.WriteString(fmt.Sprintf("  built: %s\n", Date))
	builder.WriteString(fmt.Sprintf("  go: %s\n", runtime.Version()))

	// Show container image that will be used
	containerImage := GetContainerImage()
	builder.WriteString(fmt.Sprintf("  container: %s\n", containerImage))

	// Add development build warning
	if IsDevelopment() {
		builder.WriteString("\n⚠️  Development build")
	}

	return builder.String()
}

// GetContainerImage returns the appropriate container image for this version.
// Production builds use version-tagged images for reproducibility.
// Development builds use :latest for rapid iteration.
func GetContainerImage() string {
	// Development builds use latest
	if IsDevelopment() {
		return "ghcr.io/uprockcom/maestro:latest"
	}

	// Production builds use version-tagged images
	// Strip 'v' prefix if present (v1.2.3 -> 1.2.3)
	version := strings.TrimPrefix(Version, "v")
	return fmt.Sprintf("ghcr.io/uprockcom/maestro:%s", version)
}

// GetContainerWebImage returns the appropriate web-enabled container image for this version.
func GetContainerWebImage() string {
	if IsDevelopment() {
		return "ghcr.io/uprockcom/maestro-web:latest"
	}
	version := strings.TrimPrefix(Version, "v")
	return fmt.Sprintf("ghcr.io/uprockcom/maestro-web:%s", version)
}

// IsDevelopment returns true if this is a development build.
func IsDevelopment() bool {
	return Version == "dev" || strings.Contains(Version, "next") || strings.Contains(Version, "snapshot")
}

// Short returns a short version string suitable for display in limited space.
func Short() string {
	if IsDevelopment() {
		// Show commit for dev builds
		if Commit != "none" && len(Commit) > 7 {
			return fmt.Sprintf("dev-%s", Commit[:7])
		}
		return "dev"
	}
	return Version
}
