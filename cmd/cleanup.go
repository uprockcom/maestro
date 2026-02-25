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
	"time"

	"github.com/spf13/cobra"
	"github.com/uprockcom/maestro/pkg/container"
	"github.com/uprockcom/maestro/pkg/containerservice"
)

var (
	forceCleanup   bool
	cleanupAll     bool
	cleanupTimeout int
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
	cleanupCmd.Flags().IntVar(&cleanupTimeout, "timeout", 0, "Per-container timeout in seconds (0 = no timeout)")
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

	if cleanupTimeout < 0 {
		return fmt.Errorf("--timeout must be non-negative")
	}

	// Validate state hash once before starting (optimistic concurrency check).
	// The first call uses the real hash; subsequent calls use empty string
	// (empty hash skips validation, safe because the user already confirmed).
	stateHash := svc.StateHash()

	total := len(toRemove)
	fmt.Printf("\nRemoving %d container(s)...\n", total)

	var removed []string
	var errors []string
	totalVolumes := 0

	for i, name := range toRemove {
		fmt.Printf("  [%d/%d] Removing %s...", i+1, total, name)

		// Use state hash only for the first container to detect stale state,
		// then skip validation for the rest (we've already confirmed the list).
		hash := ""
		if i == 0 {
			hash = stateHash
		}

		ctx := cmd.Context()
		var cancel context.CancelFunc
		if cleanupTimeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, time.Duration(cleanupTimeout)*time.Second)
		}

		result, err := svc.CleanupContainers(ctx, []string{name}, hash, &containerservice.CleanupOptions{
			SkipRefresh: true, // refresh once at end, not per container
		})

		if cancel != nil {
			cancel()
		}

		if err != nil {
			if i == 0 && isStateHashMismatch(err) {
				fmt.Println(" failed")
				return fmt.Errorf("container state changed since listing — please re-run 'maestro cleanup'")
			}
			fmt.Printf(" error: %v\n", err)
			errors = append(errors, fmt.Sprintf("failed to remove %s: %v", name, err))
			continue
		}

		if len(result.Removed) > 0 {
			removed = append(removed, result.Removed...)
			totalVolumes += result.VolumesRemoved
			fmt.Println(" done")
		} else if len(result.Errors) > 0 {
			fmt.Println(" failed")
		} else {
			fmt.Println(" skipped (already removed)")
		}

		// Always report errors (e.g. volume removal failures) even if container was removed
		for _, e := range result.Errors {
			fmt.Printf("    warning: %s\n", e)
			errors = append(errors, e)
		}
	}

	// Remove any expose sidecars associated with the cleaned-up containers
	if len(removed) > 0 {
		removeExposeSidecarsForContainers(removed)
	}

	// Refresh daemon cache once after all mutations
	if svc.IsDaemonConnected() {
		if err := svc.RefreshCache(cmd.Context()); err != nil {
			fmt.Printf("\n  Warning: failed to refresh container cache: %v\n", err)
		}
	}

	fmt.Printf("\nCleaned up %d container(s) and %d volume(s)\n", len(removed), totalVolumes)

	if len(errors) > 0 {
		return fmt.Errorf("cleanup completed with %d error(s)", len(errors))
	}
	return nil
}
