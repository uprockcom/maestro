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
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
)

var forceVolumeCleanup bool

var cleanupVolumesCmd = &cobra.Command{
	Use:   "cleanup-volumes",
	Short: "Remove orphaned volumes",
	Long:  `Remove volumes for containers that no longer exist.`,
	RunE:  runCleanupVolumes,
}

func init() {
	rootCmd.AddCommand(cleanupVolumesCmd)
	cleanupVolumesCmd.Flags().BoolVarP(&forceVolumeCleanup, "force", "f", false, "Skip confirmation")
}

func runCleanupVolumes(cmd *cobra.Command, args []string) error {
	// Get all Maestro volumes
	volumeCmd := exec.Command("docker", "volume", "ls", "--format", "{{.Name}}")
	volumeOutput, err := volumeCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list volumes: %w", err)
	}

	var matchingVolumes []string
	prefix := config.Containers.Prefix
	for _, line := range strings.Split(string(volumeOutput), "\n") {
		if strings.HasPrefix(line, prefix) {
			matchingVolumes = append(matchingVolumes, line)
		}
	}

	if len(matchingVolumes) == 0 {
		fmt.Println("No Maestro volumes found.")
		return nil
	}

	// Get all Maestro containers (including stopped)
	containerCmd := exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("name=%s", prefix), "--format", "{{.Names}}")
	containerOutput, err := containerCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	containers := make(map[string]bool)
	for _, line := range strings.Split(string(containerOutput), "\n") {
		if line != "" {
			containers[line] = true
		}
	}

	// Find orphaned volumes
	var orphaned []string
	for _, vol := range matchingVolumes {
		// Extract container name from volume name
		// Volume format: <prefix><name>-<number>-<type>
		// Container format: <prefix><name>-<number>
		parts := strings.Split(vol, "-")
		if len(parts) < 2 {
			continue
		}

		// Remove the last part (npm, uv, history, claude-debug) to get container name
		containerName := strings.Join(parts[:len(parts)-1], "-")

		if !containers[containerName] {
			orphaned = append(orphaned, vol)
		}
	}

	if len(orphaned) == 0 {
		fmt.Println("No orphaned volumes found.")
		return nil
	}

	// Show what will be removed
	fmt.Printf("Found %d orphaned volume(s):\n", len(orphaned))
	for _, vol := range orphaned {
		fmt.Printf("  - %s\n", vol)
	}

	// Confirm unless forced
	if !forceVolumeCleanup {
		fmt.Print("\nRemove these volumes? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))

		if response != "y" && response != "yes" {
			fmt.Println("Cleanup cancelled.")
			return nil
		}
	}

	// Remove orphaned volumes
	removed := 0
	for _, vol := range orphaned {
		volCmd := exec.Command("docker", "volume", "rm", vol)
		if err := volCmd.Run(); err != nil {
			fmt.Printf("Warning: failed to remove %s: %v\n", vol, err)
		} else {
			removed++
		}
	}

	fmt.Printf("\n✅ Removed %d orphaned volume(s)\n", removed)
	return nil
}
