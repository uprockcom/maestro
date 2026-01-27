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
	"strconv"
	"strings"

	"github.com/uprockcom/maestro/pkg/container"
	"github.com/spf13/cobra"
)

var connectCmd = &cobra.Command{
	Use:   "connect [name]",
	Short: "Connect to a running container",
	Long: `Connect to the tmux session in a running mcl container.

If no name is provided:
  - Auto-connects if only one container is running
  - Shows interactive selection if multiple containers are running`,
	Args: cobra.MaximumNArgs(1),
	RunE: runConnect,
}

func init() {
	rootCmd.AddCommand(connectCmd)
}

func runConnect(cmd *cobra.Command, args []string) error {
	var containerName string

	// If no argument provided, show interactive selection
	if len(args) == 0 {
		// Check both configured prefix and legacy "mcl-" prefix for backward compatibility
		containers, err := container.GetRunningContainers(config.Containers.Prefix)
		if err != nil {
			return fmt.Errorf("failed to get running containers: %w", err)
		}

		// Also check legacy prefix if different from configured
		if config.Containers.Prefix != "mcl-" {
			legacyContainers, _ := container.GetRunningContainers("mcl-")
			containers = append(containers, legacyContainers...)
		}

		if len(containers) == 0 {
			return fmt.Errorf("no running containers found. Create one with: maestro new \"task description\"")
		}

		if len(containers) == 1 {
			// Auto-connect to the only container
			containerName = containers[0].Name
			fmt.Printf("Auto-connecting to %s\n", containers[0].ShortName)
		} else {
			// Multiple containers - show selection
			selected, err := selectContainer(containers)
			if err != nil {
				return err
			}
			containerName = selected.Name
		}
	} else {
		// Argument provided - resolve container name
		shortName := args[0]
		containerName = resolveContainerName(shortName)

		// Check if container exists (include stopped containers with -a)
		checkCmd := exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("name=^%s$", containerName), "--format", "{{.State}}")
		output, err := checkCmd.Output()
		if err != nil {
			return fmt.Errorf("failed to check container status: %w", err)
		}

		state := strings.TrimSpace(string(output))
		if state == "" {
			return fmt.Errorf("container %s not found", shortName)
		}
		if state != "running" {
			// Container exists but is stopped - offer to start it
			fmt.Printf("Container %s is stopped (status: %s)\n", shortName, state)
			fmt.Print("Would you like to start it? (Y/n): ")

			reader := bufio.NewReader(os.Stdin)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))

			if response != "" && response != "y" && response != "yes" {
				return fmt.Errorf("cancelled - container not started")
			}

			fmt.Printf("Starting %s...\n", shortName)
			if err := container.StartContainer(containerName); err != nil {
				return fmt.Errorf("failed to start container: %w", err)
			}
			fmt.Println("Container started successfully")
		}
	}

	// Ensure container has fresh token before connecting
	fmt.Printf("Syncing credentials for %s...\n", containerName)
	if err := container.EnsureFreshToken(containerName, config.Containers.Prefix); err != nil {
		// Warn but don't fail - user might want to connect anyway
		fmt.Printf("⚠️  Token sync warning: %v\n", err)
		fmt.Println("   You may need to run 'maestro auth' if authentication fails.")
	}

	fmt.Printf("Connecting to %s...\n", containerName)
	fmt.Println("Detach with: Ctrl+b d")
	fmt.Println("Switch windows: Ctrl+b 0 (Claude), Ctrl+b 1 (shell)")

	// Connect to tmux session
	connectCmd := exec.Command("docker", "exec", "-it", containerName, "tmux", "attach", "-t", "main")
	connectCmd.Stdin = os.Stdin
	connectCmd.Stdout = os.Stdout
	connectCmd.Stderr = os.Stderr

	return connectCmd.Run()
}

// selectContainer shows an interactive menu to select a container
func selectContainer(containers []container.Info) (container.Info, error) {
	// Display containers with numbers using unified display
	fmt.Println("\nSelect a container to connect:")
	fmt.Println()
	sorted := container.Display(containers, container.DisplayOptions{
		ShowNumbers: true,
		ShowTable:   true,
	})

	fmt.Println()
	fmt.Printf("Enter number (1-%d): ", len(sorted))

	// Read user input
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return container.Info{}, fmt.Errorf("failed to read input: %w", err)
	}

	input = strings.TrimSpace(input)
	choice, err := strconv.Atoi(input)
	if err != nil || choice < 1 || choice > len(sorted) {
		return container.Info{}, fmt.Errorf("invalid selection: %s", input)
	}

	return sorted[choice-1], nil
}