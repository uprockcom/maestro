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

var stopCmd = &cobra.Command{
	Use:   "stop [name]",
	Short: "Stop a running container",
	Long: `Stop a running maestro container. The container can be restarted later.

If no name is provided, will prompt to stop all dormant containers (where Claude is not running).`,
	Args: cobra.MaximumNArgs(1),
	RunE: runStop,
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

func runStop(cmd *cobra.Command, args []string) error {
	// If no arguments, prompt to stop dormant containers
	if len(args) == 0 {
		return stopDormantContainers()
	}

	// Stop specific container
	shortName := args[0]
	containerName := resolveContainerName(shortName)

	fmt.Printf("Stopping %s...\n", containerName)

	stopCmd := exec.Command("docker", "stop", containerName)
	if err := stopCmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	fmt.Printf("✅ Container %s stopped\n", containerName)
	fmt.Printf("To remove it completely, run: maestro cleanup\n")
	fmt.Printf("To restart it, run: docker start %s && maestro connect %s\n", containerName, shortName)

	return nil
}

func stopDormantContainers() error {
	// Get all running containers
	containers, err := container.GetRunningContainers(config.Containers.Prefix)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Filter for dormant containers
	var dormantContainers []container.Info
	for _, c := range containers {
		if c.IsDormant {
			dormantContainers = append(dormantContainers, c)
		}
	}

	if len(dormantContainers) == 0 {
		fmt.Println("No dormant containers found.")
		fmt.Println("(Dormant = containers where Claude is not running)")
		return nil
	}

	// Display dormant containers
	fmt.Printf("Found %d dormant container(s):\n", len(dormantContainers))
	for _, c := range dormantContainers {
		fmt.Printf("  - %s (branch: %s)\n", c.ShortName, c.Branch)
	}

	// Prompt for confirmation
	fmt.Print("\nStop all dormant containers? (y/N): ")
	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	response = strings.TrimSpace(strings.ToLower(response))
	if response != "y" && response != "yes" {
		fmt.Println("Cancelled.")
		return nil
	}

	// Stop all dormant containers
	fmt.Println("\nStopping dormant containers...")
	successCount := 0
	for _, c := range dormantContainers {
		fmt.Printf("  Stopping %s... ", c.ShortName)
		stopCmd := exec.Command("docker", "stop", c.Name)
		if err := stopCmd.Run(); err != nil {
			fmt.Printf("FAILED: %v\n", err)
			continue
		}
		fmt.Println("✓")
		successCount++
	}

	if successCount == len(dormantContainers) {
		fmt.Printf("\n✅ Successfully stopped %d container(s)\n", successCount)
	} else {
		fmt.Printf("\n⚠️  Stopped %d/%d container(s)\n", successCount, len(dormantContainers))
	}

	fmt.Println("\nTo remove stopped containers, run: maestro cleanup")

	return nil
}
