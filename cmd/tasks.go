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
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/uprockcom/maestro/pkg/container"
)

var (
	tasksWatch    bool
	tasksInterval int
	tasksVerbose  bool
)

var tasksCmd = &cobra.Command{
	Use:   "tasks [container]",
	Short: "Show Claude Code task status for containers",
	Long: `Display the Claude Code task list status for running containers.

Shows the current tasks that Claude is working on, including pending,
in-progress, and completed items.

Examples:
  maestro tasks              # Show tasks for all running containers
  maestro tasks my-feature   # Show tasks for specific container
  maestro tasks --watch      # Continuously update task display
  maestro tasks -v           # Show verbose task details`,
	RunE: runTasks,
}

func init() {
	rootCmd.AddCommand(tasksCmd)
	tasksCmd.Flags().BoolVarP(&tasksWatch, "watch", "w", false, "Continuously watch and update task display")
	tasksCmd.Flags().IntVarP(&tasksInterval, "interval", "i", 2, "Update interval in seconds (with --watch)")
	tasksCmd.Flags().BoolVarP(&tasksVerbose, "verbose", "v", false, "Show verbose task details")
}

func runTasks(cmd *cobra.Command, args []string) error {
	// Check if Docker is responsive
	if !container.IsDockerResponsive() {
		fmt.Println("Cannot connect to Docker.")
		fmt.Println("\nHint: Is Docker running?")
		return nil
	}

	var containerName string
	if len(args) > 0 {
		containerName = args[0]
		// Add prefix if not present
		if !strings.HasPrefix(containerName, config.Containers.Prefix) {
			containerName = config.Containers.Prefix + containerName
		}
	}

	if tasksWatch {
		return runTasksWatch(containerName)
	}

	return displayTasks(containerName)
}

func displayTasks(containerName string) error {
	if containerName != "" {
		// Show tasks for specific container
		tasks, err := container.GetContainerTasks(containerName)
		if err != nil {
			return fmt.Errorf("failed to get tasks: %w", err)
		}
		displayContainerTasks(tasks)
	} else {
		// Show tasks for all containers
		allTasks, err := container.GetAllContainerTasks(config.Containers.Prefix)
		if err != nil {
			return fmt.Errorf("failed to get tasks: %w", err)
		}

		if len(allTasks) == 0 {
			fmt.Println("No running containers found.")
			return nil
		}

		for i, ct := range allTasks {
			if i > 0 {
				fmt.Println()
			}
			displayContainerTasks(&ct)
		}
	}

	return nil
}

func displayContainerTasks(ct *container.ContainerTasks) {
	// Header
	fmt.Printf("📦 %s\n", ct.ShortName)

	if ct.Error != nil {
		fmt.Printf("   ⚠️  %v\n", ct.Error)
		return
	}

	if len(ct.Sessions) == 0 {
		fmt.Println("   No active tasks")
		return
	}

	// Show the most recent session (usually the active one)
	for _, session := range ct.Sessions {
		summary := session.GetSummary()

		// Progress bar
		progressBar := renderProgressBar(summary.CompletedTasks, summary.TotalTasks)

		// Status line
		fmt.Printf("   %s %d/%d tasks", progressBar, summary.CompletedTasks, summary.TotalTasks)
		if summary.InProgressTasks > 0 {
			fmt.Printf(" (%d in progress)", summary.InProgressTasks)
		}
		fmt.Println()

		// Current task
		if summary.CurrentTask != "" {
			// Truncate long task names
			currentTask := summary.CurrentTask
			if len(currentTask) > 60 {
				currentTask = currentTask[:57] + "..."
			}
			fmt.Printf("   ▶ %s\n", currentTask)
		}

		// Verbose: show all tasks
		if tasksVerbose {
			fmt.Println()
			for _, task := range session.Tasks {
				displayTask(task, "   ")
			}
		}

		// Only show most recent session unless verbose
		if !tasksVerbose {
			break
		}
	}
}

func displayTask(task container.Task, indent string) {
	var statusIcon string
	switch task.Status {
	case container.TaskStatusCompleted:
		statusIcon = "✓"
	case container.TaskStatusInProgress:
		statusIcon = "▶"
	case container.TaskStatusPending:
		statusIcon = "○"
	default:
		statusIcon = "?"
	}

	name := task.GetDisplayName()

	// Truncate long names
	if len(name) > 70 {
		name = name[:67] + "..."
	}

	fmt.Printf("%s%s %s\n", indent, statusIcon, name)
}

func renderProgressBar(completed, total int) string {
	if total == 0 {
		return "[          ]"
	}

	width := 10
	filled := (completed * width) / total
	if filled > width {
		filled = width
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return "[" + bar + "]"
}

func runTasksWatch(containerName string) error {
	// Set up signal handling for clean exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	ticker := time.NewTicker(time.Duration(tasksInterval) * time.Second)
	defer ticker.Stop()

	// Clear screen and hide cursor
	fmt.Print("\033[2J\033[H\033[?25l")
	defer fmt.Print("\033[?25h") // Show cursor on exit

	// Initial display
	clearAndDisplay(containerName)

	for {
		select {
		case <-ticker.C:
			clearAndDisplay(containerName)
		case <-sigChan:
			fmt.Println("\nStopping watch...")
			return nil
		}
	}
}

func clearAndDisplay(containerName string) {
	// Move cursor to top-left and clear screen
	fmt.Print("\033[H\033[2J")

	// Header with timestamp
	fmt.Printf("Task Monitor - %s (Ctrl+C to exit)\n", time.Now().Format("15:04:05"))
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()

	if err := displayTasks(containerName); err != nil {
		fmt.Printf("Error: %v\n", err)
	}
}
