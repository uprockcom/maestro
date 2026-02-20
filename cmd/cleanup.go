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
	"github.com/uprockcom/maestro/pkg/container"
)

var (
	forceCleanup bool
	cleanupAll   bool
)

var cleanupCmd = &cobra.Command{
	Use:   "cleanup",
	Short: "Remove stopped containers",
	Long:  `Remove stopped Maestro containers and their associated volumes.`,
	RunE:  runCleanup,
}

func init() {
	rootCmd.AddCommand(cleanupCmd)
	cleanupCmd.Flags().BoolVarP(&forceCleanup, "force", "f", false, "Skip confirmation")
	cleanupCmd.Flags().BoolVarP(&cleanupAll, "all", "a", false, "Remove all containers (including running)")
}

func runCleanup(cmd *cobra.Command, args []string) error {
	// Get containers to remove
	filter := config.Containers.Prefix
	dockerCmd := exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("name=%s", filter), "--format", "{{.Names}}\t{{.State}}")
	output, err := dockerCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	var toRemove []string
	var running []string

	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}

		name := parts[0]
		state := parts[1]

		if container.IsInfraContainer(name) {
			continue
		}

		if state == "running" {
			if cleanupAll {
				running = append(running, name)
			}
		} else {
			toRemove = append(toRemove, name)
		}
	}

	if cleanupAll {
		toRemove = append(toRemove, running...)
	}

	if len(toRemove) == 0 {
		fmt.Println("No containers to clean up.")
		return nil
	}

	// Show what will be removed
	fmt.Println("The following containers will be removed:")
	for _, name := range toRemove {
		fmt.Printf("  - %s\n", name)
	}

	// Confirm unless forced
	if !forceCleanup {
		fmt.Print("\nContinue? [y/N]: ")
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.ToLower(strings.TrimSpace(response))

		if response != "y" && response != "yes" {
			fmt.Println("Cleanup cancelled.")
			return nil
		}
	}

	// Stop running containers if needed
	for _, name := range running {
		fmt.Printf("Stopping %s...\n", name)
		stopCmd := exec.Command("docker", "stop", name)
		if err := stopCmd.Run(); err != nil {
			fmt.Printf("Warning: failed to stop %s: %v\n", name, err)
		}
	}

	// Remove containers and volumes
	totalVolumes := 0
	for _, name := range toRemove {
		fmt.Printf("Removing %s...\n", name)

		// Remove container
		rmCmd := exec.Command("docker", "rm", "-f", "-v", name)
		if err := rmCmd.Run(); err != nil {
			fmt.Printf("Warning: failed to remove %s: %v\n", name, err)
			continue
		}

		// Remove associated named volumes
		volumes := []string{
			fmt.Sprintf("%s-npm", name),
			fmt.Sprintf("%s-uv", name),
			fmt.Sprintf("%s-history", name),
			fmt.Sprintf("%s-claude-debug", name), // Also remove debug volume if it exists
		}

		for _, vol := range volumes {
			volCmd := exec.Command("docker", "volume", "rm", vol)
			output, err := volCmd.CombinedOutput()
			if err != nil {
				// Only warn if it's not a "volume not found" error
				if !strings.Contains(string(output), "no such volume") {
					fmt.Printf("  Warning: failed to remove volume %s: %v\n", vol, err)
				}
			} else {
				totalVolumes++
				fmt.Printf("  Removed volume %s\n", vol)
			}
		}
	}

	fmt.Printf("\n✅ Cleaned up %d container(s) and %d volume(s)\n", len(toRemove), totalVolumes)
	return nil
}
