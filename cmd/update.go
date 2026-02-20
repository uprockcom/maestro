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
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/uprockcom/maestro/pkg/container"
)

var updateCmd = &cobra.Command{
	Use:   "update <container> [flags]",
	Short: "Update resource limits on a running container",
	Long: `Update memory and/or CPU limits on a running container using docker update.

Examples:
  maestro update my-container --memory 14g
  maestro update my-container --cpus 12
  maestro update my-container --memory 8g --cpus 4`,
	Args: cobra.ExactArgs(1),
	RunE: runUpdate,
}

func init() {
	updateCmd.Flags().String("memory", "", "Memory limit (e.g., 14g)")
	updateCmd.Flags().String("cpus", "", "CPU limit (e.g., 12)")
	rootCmd.AddCommand(updateCmd)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	containerName := resolveContainerName(args[0])

	memory, _ := cmd.Flags().GetString("memory")
	cpus, _ := cmd.Flags().GetString("cpus")

	if memory == "" && cpus == "" {
		return fmt.Errorf("at least one of --memory or --cpus must be provided")
	}

	// Check if container is running
	checkCmd := exec.Command("docker", "ps", "--filter", fmt.Sprintf("name=%s", containerName), "--format", "{{.State}}")
	output, err := checkCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}

	state := strings.TrimSpace(string(output))
	if state != "running" {
		return fmt.Errorf("container %s is not running", args[0])
	}

	if err := container.UpdateContainerResources(containerName, memory, cpus); err != nil {
		return err
	}

	fmt.Printf("Updated resources for %s:", containerName)
	if memory != "" {
		fmt.Printf(" memory=%s", memory)
	}
	if cpus != "" {
		fmt.Printf(" cpus=%s", cpus)
	}
	fmt.Println()

	return nil
}
