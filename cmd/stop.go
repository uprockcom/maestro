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
	"context"
	"fmt"
	"os"
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
		return stopDormantContainers(cmd.Context())
	}

	// Stop specific container via ContainerService
	svc := newContainerService()
	defer svc.Close()

	shortName := args[0]
	containerName := resolveContainerName(shortName)

	fmt.Printf("Stopping %s...\n", containerName)

	// Empty state hash = skip validation (direct CLI command, not from a stale list)
	if err := svc.StopContainer(cmd.Context(), containerName, ""); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	fmt.Printf("Container %s stopped\n", containerName)
	fmt.Printf("To remove it completely, run: maestro cleanup\n")
	fmt.Printf("To restart it, run: docker start %s && maestro connect %s\n", containerName, shortName)

	return nil
}

func stopDormantContainers(ctx context.Context) error {
	svc := newContainerService()
	defer svc.Close()

	// Get all running containers
	containers, err := svc.ListRunning(ctx)
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

	// Stop all dormant containers via ContainerService
	// Use state hash from the list call for optimistic concurrency
	stateHash := svc.StateHash()
	fmt.Println("\nStopping dormant containers...")
	successCount := 0
	for _, c := range dormantContainers {
		fmt.Printf("  Stopping %s... ", c.ShortName)
		if err := svc.StopContainer(ctx, c.Name, stateHash); err != nil {
			if isStateHashMismatch(err) {
				fmt.Printf("FAILED: container state changed — re-run 'maestro stop'\n")
				break
			}
			fmt.Printf("FAILED: %v\n", err)
			continue
		}
		fmt.Println("done")
		successCount++
		// Clear state hash after first mutation — subsequent stops go through
		// without validation since the state has already changed
		stateHash = ""
	}

	if successCount == len(dormantContainers) {
		fmt.Printf("\nSuccessfully stopped %d container(s)\n", successCount)
	} else {
		fmt.Printf("\nStopped %d/%d container(s)\n", successCount, len(dormantContainers))
	}

	fmt.Println("\nTo remove stopped containers, run: maestro cleanup")

	return nil
}
