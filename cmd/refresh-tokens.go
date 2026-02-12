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

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/uprockcom/maestro/pkg/container"
	"github.com/uprockcom/maestro/pkg/paths"
)

var refreshTokensCmd = &cobra.Command{
	Use:   "refresh-tokens",
	Short: "Find and propagate the freshest authentication token",
	Long: `Scans all running containers and the host for credentials,
finds the one with the latest expiration time, and syncs it
to all containers and the host.

This is useful when tokens have been automatically refreshed
in one container but not synchronized to others.`,
	RunE: runRefreshTokens,
}

func init() {
	rootCmd.AddCommand(refreshTokensCmd)
}

type tokenSource struct {
	location  string // "host" or container name
	path      string // file path (for reading)
	creds     *container.Credentials
	expiresAt time.Time
}

func runRefreshTokens(cmd *cobra.Command, args []string) error {
	fmt.Println("Scanning for credentials...")

	var sources []tokenSource

	// 1. Check host credentials
	hostCredPath := filepath.Join(paths.AuthDir(), ".credentials.json")
	if hostCreds, err := container.ReadCredentials(hostCredPath); err == nil {
		sources = append(sources, tokenSource{
			location:  "host",
			path:      hostCredPath,
			creds:     hostCreds,
			expiresAt: time.UnixMilli(hostCreds.ClaudeAiOauth.ExpiresAt),
		})
		fmt.Printf("  ✓ Host: %s\n", container.FormatExpiration(hostCreds))
	} else {
		fmt.Printf("  ✗ Host: Could not read credentials (%v)\n", err)
	}

	// 2. Check all running containers (including legacy prefix for backward compatibility)
	containers, err := container.GetRunningContainers(config.Containers.Prefix)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Also check legacy prefix if different from configured
	if config.Containers.Prefix != "mcl-" {
		legacyContainers, _ := container.GetRunningContainers("mcl-")
		containers = append(containers, legacyContainers...)
	}

	for _, c := range containers {
		// Extract credentials from container to temp file
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("maestro-creds-%s.json", c.Name))
		copyCmd := exec.Command("docker", "cp",
			fmt.Sprintf("%s:/home/node/.claude/.credentials.json", c.Name),
			tmpFile)
		if err := copyCmd.Run(); err != nil {
			fmt.Printf("  ✗ %s: Could not read credentials\n", c.Name)
			continue
		}
		defer os.Remove(tmpFile)

		if creds, err := container.ReadCredentials(tmpFile); err == nil {
			sources = append(sources, tokenSource{
				location:  c.Name,
				path:      tmpFile,
				creds:     creds,
				expiresAt: time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt),
			})
			fmt.Printf("  ✓ %s: %s\n", c.Name, container.FormatExpiration(creds))
		}
	}

	if len(sources) == 0 {
		return fmt.Errorf("no valid credentials found in host or containers")
	}

	// 3. Find freshest token
	var freshest tokenSource
	for _, src := range sources {
		if src.expiresAt.After(freshest.expiresAt) {
			freshest = src
		}
	}

	// 4. Check if freshest is still valid
	if container.IsTokenExpired(freshest.creds) {
		fmt.Println("\n❌ All tokens are expired!")
		fmt.Printf("   Latest token: %s\n", container.FormatExpiration(freshest.creds))
		fmt.Println("\nPlease run 'maestro auth' to re-authenticate.")
		return fmt.Errorf("all tokens expired")
	}

	fmt.Printf("\n✓ Found fresh token in %s\n", freshest.location)
	fmt.Printf("  Expires: %s\n", freshest.expiresAt.Format(time.RFC1123))
	fmt.Printf("  Status: %s\n", container.FormatExpiration(freshest.creds))

	// 5. Warn if expiring soon
	timeUntilExp := container.TimeUntilExpiration(freshest.creds)
	if timeUntilExp < 24*time.Hour {
		fmt.Printf("\n⚠️  Token expires in less than 24 hours!\n")
		fmt.Printf("   Consider running 'maestro auth' soon.\n")
	}

	// 6. Sync to all locations
	fmt.Println("\nSyncing credentials...")

	syncCount := 0

	// Sync to host (if not already source)
	if freshest.location != "host" {
		if err := copyCredentials(freshest.path, hostCredPath); err != nil {
			fmt.Printf("  ✗ Failed to sync to host: %v\n", err)
		} else {
			fmt.Println("  ✓ Synced to host")
			syncCount++
		}
	}

	// Sync to containers (skip source container)
	for _, container := range containers {
		if container.Name == freshest.location {
			continue
		}

		// Copy to container
		tmpFile := freshest.path
		if freshest.location == "host" {
			tmpFile = hostCredPath
		}

		copyCmd := exec.Command("docker", "cp", tmpFile,
			fmt.Sprintf("%s:/home/node/.claude/.credentials.json", container.Name))
		if err := copyCmd.Run(); err != nil {
			fmt.Printf("  ✗ Failed to sync to %s: %v\n", container.Name, err)
			continue
		}

		// Fix ownership
		chownCmd := exec.Command("docker", "exec", "-u", "root", container.Name,
			"chown", "node:node", "/home/node/.claude/.credentials.json")
		if err := chownCmd.Run(); err != nil {
			fmt.Printf("  ⚠  Synced to %s but failed to fix ownership\n", container.Name)
		} else {
			fmt.Printf("  ✓ Synced to %s\n", container.Name)
		}
		syncCount++
	}

	fmt.Printf("\n✅ Refresh complete! Synced to %d location(s).\n", syncCount)
	return nil
}

func copyCredentials(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0600)
}
