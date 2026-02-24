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
	svc := newContainerService()
	defer svc.Close()

	// Get all containers via ContainerService (daemon cache or Docker fallback)
	containers, err := svc.ListAll(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	var toRemove []string

	for _, c := range containers {
		if container.IsInfraContainer(c.Name) {
			continue
		}

		if c.Status == "running" {
			if cleanupAll {
				toRemove = append(toRemove, c.Name)
			}
		} else {
			toRemove = append(toRemove, c.Name)
		}
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

	// Remove containers via ContainerService (handles stop, remove, and volumes)
	// Uses state hash from the list call for optimistic concurrency
	fmt.Printf("Removing %d container(s)...\n", len(toRemove))
	result, err := svc.CleanupContainers(cmd.Context(), toRemove, svc.StateHash())
	if err != nil {
		if isStateHashMismatch(err) {
			return fmt.Errorf("container state changed since listing — please re-run 'maestro cleanup'")
		}
		return fmt.Errorf("cleanup failed: %w", err)
	}

	// Report errors
	for _, e := range result.Errors {
		fmt.Printf("  Warning: %s\n", e)
	}

	// Remove any expose sidecars associated with the cleaned-up containers
	if len(result.Removed) > 0 {
		removeExposeSidecarsForContainers(result.Removed)
	}

	fmt.Printf("\nCleaned up %d container(s) and %d volume(s)\n", len(result.Removed), result.VolumesRemoved)
	return nil
}
