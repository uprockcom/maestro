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

package container

import (
	"fmt"
	"os"
	"text/tabwriter"
)

// formatTaskForDisplay returns a formatted task string for display in the list
func formatTaskForDisplay(c Info) string {
	if c.Status != "running" {
		return "-"
	}
	if c.CurrentTask != "" {
		// Show current task with progress if available
		task := c.CurrentTask
		if len(task) > 20 {
			task = task[:17] + "..."
		}
		if c.TaskProgress != "" {
			return "▶ " + task + " (" + c.TaskProgress + ")"
		}
		return "▶ " + task
	}
	if c.TaskProgress != "" {
		return "✓ " + c.TaskProgress + " done"
	}
	return "-"
}

// SortByPriority sorts containers by logical priority groups, then by creation date within each group
// Priority order:
// 1. Needs Attention (running with idle flag from Claude Code hooks)
// 2. Running (normal)
// 3. Dormant (running but Claude not active)
// 4. Stopped
// Within each group, sorts by creation date (newest first)
func SortByPriority(containers []Info) []Info {
	// Create a copy to avoid modifying the original
	sorted := make([]Info, len(containers))
	copy(sorted, containers)

	// Define priority function
	getPriority := func(c Info) int {
		if c.IsDormant {
			return 2 // Dormant trumps stale idle flag
		}
		if c.NeedsAttention {
			return 0 // Highest priority
		}
		if c.Status == "running" {
			return 1
		}
		return 3 // Stopped containers lowest priority
	}

	// Sort by priority, then by creation date
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			iPriority := getPriority(sorted[i])
			jPriority := getPriority(sorted[j])

			// First sort by priority
			if iPriority > jPriority {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			} else if iPriority == jPriority {
				// Within same priority, sort by creation date (newest first)
				if sorted[i].CreatedAt.Before(sorted[j].CreatedAt) {
					sorted[i], sorted[j] = sorted[j], sorted[i]
				}
			}
		}
	}

	return sorted
}

// Display shows containers in a consistent format
// Returns the sorted list for use in selection
func Display(containers []Info, opts DisplayOptions) []Info {
	// Sort containers
	sorted := SortByPriority(containers)

	if opts.ShowTable {
		// Table format with tabwriter for proper alignment
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

		// Add number column header if showing numbers
		if opts.ShowNumbers {
			fmt.Fprintln(w, "#\tNAME\tSTATUS\tBRANCH\tTASK\tGIT\tAUTH\tATTENTION")
			fmt.Fprintln(w, "-\t----\t------\t------\t----\t---\t----\t---------")
		} else {
			fmt.Fprintln(w, "NAME\tSTATUS\tBRANCH\tTASK\tGIT\tAUTH\tATTENTION")
			fmt.Fprintln(w, "----\t------\t------\t----\t---\t----\t---------")
		}

		for i, c := range sorted {
			attention := ""
			// Dormant takes priority: if Claude isn't running, the idle flag
			// file is stale and shouldn't be treated as "needs attention"
			if c.IsDormant {
				attention = "💤"
			} else if c.NeedsAttention {
				attention = "🔔"
			}

			// Derive display status: show "dormant" for containers where Claude exited
			displayStatus := c.Status
			if c.Status == "running" && c.IsDormant {
				displayStatus = "dormant"
			}

			// Use default values for stopped containers
			gitStatus := c.GitStatus
			if gitStatus == "" {
				gitStatus = "-"
			}
			authStatus := c.AuthStatus
			if authStatus == "" {
				authStatus = "-"
			}

			// Format task info
			taskInfo := formatTaskForDisplay(c)

			// Include number column if showing numbers
			if opts.ShowNumbers {
				fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					i+1, c.ShortName, displayStatus, c.Branch, taskInfo, gitStatus, authStatus, attention)
			} else {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					c.ShortName, displayStatus, c.Branch, taskInfo, gitStatus, authStatus, attention)
			}
		}
		w.Flush()
	} else if opts.ShowNumbers {
		// Numbered list format (for selection)
		fmt.Println("\nContainers:")
		fmt.Println()

		for i, c := range sorted {
			status := ""
			if c.IsDormant {
				status = " 💤 DORMANT"
			} else if c.NeedsAttention {
				status = " 🔔 NEEDS ATTENTION"
			} else if c.Status != "running" {
				status = " (stopped)"
			}
			fmt.Printf("  %d) %s (branch: %s)%s\n", i+1, c.ShortName, c.Branch, status)
		}
		fmt.Println()
	} else {
		// Simple list format (no numbers)
		for _, c := range sorted {
			status := ""
			if c.IsDormant {
				status = " 💤"
			} else if c.NeedsAttention {
				status = " 🔔"
			}
			fmt.Printf("  %s (branch: %s)%s\n", c.ShortName, c.Branch, status)
		}
	}

	return sorted
}
