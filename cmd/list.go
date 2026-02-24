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

	"github.com/spf13/cobra"
	"github.com/uprockcom/maestro/pkg/container"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls", "ps"},
	Short:   "List all maestro containers",
	Long:    `List all maestro containers with their status and attention indicators.`,
	RunE:    runList,
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	svc := newContainerService()
	defer svc.Close()

	containers, err := svc.ListAll(cmd.Context())
	if err != nil {
		// Fall back to Docker responsive check for better error messages
		if !container.IsDockerResponsive() {
			fmt.Println("No maestro containers found.")
			fmt.Println("\nHint: Is Docker running?")
			return nil
		}
		return fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		fmt.Println("No maestro containers found.")
		fmt.Println("Create one with: maestro new \"your task description\"")
		return nil
	}

	// Display using unified display function
	container.Display(containers, container.DisplayOptions{
		ShowNumbers: false,
		ShowTable:   true,
	})

	// Show quick help
	fmt.Println("\nCommands:")
	fmt.Println("  maestro connect <name>    - Connect to container")
	fmt.Println("  maestro stop <name>       - Stop container")
	fmt.Println("  maestro cleanup           - Remove stopped containers")

	// Show daemon nag if not running
	showDaemonNag()

	return nil
}
