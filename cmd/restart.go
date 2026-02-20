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
	"time"

	"github.com/spf13/cobra"
	"github.com/uprockcom/maestro/pkg/container"
)

var (
	fullRestart bool
)

var restartCmd = &cobra.Command{
	Use:   "restart [name]",
	Short: "Restart a container or its Claude process",
	Long: `Restart a container's Claude process without stopping the container.

This is useful when Claude has crashed (zombie process) or is unresponsive.
The restart preserves the container, git state, and shell window.

If no name is provided, you'll be prompted to select from a list.

Examples:
  maestro restart                    # Show list to select from
  maestro restart feat-auth-1        # Restart Claude process only
  maestro restart feat-auth-1 --full # Full container restart`,
	Args: cobra.MaximumNArgs(1),
	RunE: runRestart,
}

func init() {
	rootCmd.AddCommand(restartCmd)
	restartCmd.Flags().BoolVar(&fullRestart, "full", false, "Perform full container restart instead of just Claude")
}

func runRestart(cmd *cobra.Command, args []string) error {
	// Check if Docker is running
	if err := checkDockerRunning(); err != nil {
		return err
	}

	var shortName string
	var containerName string

	// If no args provided, show interactive selection
	if len(args) == 0 {
		containers, err := container.GetRunningContainers(config.Containers.Prefix)
		if err != nil {
			return fmt.Errorf("failed to get containers: %w", err)
		}

		if len(containers) == 0 {
			fmt.Println("No containers found to restart.")
			fmt.Println("\nCreate a new container with: maestro new <description>")
			return nil
		}

		selected, err := selectContainerForRestart(containers)
		if err != nil {
			return err
		}

		shortName = selected.ShortName
		containerName = selected.Name
	} else {
		shortName = args[0]
		// Check nickname first
		store := getNicknameStore()
		if resolved, ok := store.Get(shortName); ok {
			containerName = resolved
		} else {
			containerName = resolveContainerName(shortName)
		}
	}

	if fullRestart {
		return performFullRestart(containerName, shortName)
	}

	return performClaudeRestart(containerName, shortName)
}

// checkDockerRunning verifies that Docker is running
func checkDockerRunning() error {
	cmd := exec.Command("docker", "info")
	err := cmd.Run()
	if err != nil {
		// Check if it's a connection error (Docker not running)
		if strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "Cannot connect") ||
			strings.Contains(err.Error(), "Is the docker daemon running") {
			return fmt.Errorf("Docker is not running.\n\nPlease start Docker Desktop and try again.")
		}
		return fmt.Errorf("failed to check Docker status: %w", err)
	}
	return nil
}

// selectContainerForRestart shows an interactive menu to select a container for restart
func selectContainerForRestart(containers []container.Info) (container.Info, error) {
	// Display containers with numbers using unified display
	fmt.Println("\nSelect a container to restart:")
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

func performClaudeRestart(containerName, shortName string) error {
	fmt.Printf("Restarting Claude process in %s...\n", shortName)

	// Step 1: Kill any existing Claude processes (including zombies)
	fmt.Println("  Stopping Claude process...")
	killCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		"pkill -9 claude || true")
	if err := killCmd.Run(); err != nil {
		fmt.Printf("  Warning: Failed to kill Claude: %v\n", err)
	}

	// Wait a moment for cleanup
	time.Sleep(500 * time.Millisecond)

	// Step 2: Kill the tmux window 0 (Claude window)
	fmt.Println("  Recreating Claude window...")
	killWindowCmd := exec.Command("docker", "exec", containerName,
		"tmux", "kill-window", "-t", "main:0")
	if err := killWindowCmd.Run(); err != nil {
		// Window might already be dead, that's OK
		fmt.Printf("  Window already closed\n")
	}

	// Step 3: Create new window 0 with Claude
	createWindowCmd := exec.Command("docker", "exec", "-u", "node", containerName, "sh", "-c",
		"cd /workspace && HOME=/home/node tmux new-window -t main:0 -n claude 'claude --dangerously-skip-permissions'")
	if err := createWindowCmd.Run(); err != nil {
		return fmt.Errorf("failed to create new Claude window: %w", err)
	}

	// Step 4: Make window 0 active
	time.Sleep(500 * time.Millisecond)

	selectCmd := exec.Command("docker", "exec", containerName,
		"tmux", "select-window", "-t", "main:0")
	if err := selectCmd.Run(); err != nil {
		fmt.Printf("  Warning: Failed to select window: %v\n", err)
	}

	fmt.Printf("\n✅ Claude restarted successfully in %s\n", shortName)
	fmt.Printf("Connect with: maestro connect %s\n", shortName)

	return nil
}

func performFullRestart(containerName, shortName string) error {
	fmt.Printf("Performing full restart of %s...\n", shortName)

	// Step 1: Stop container
	fmt.Println("  Stopping container...")
	stopCmd := exec.Command("docker", "stop", containerName)
	if err := stopCmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	// Step 2: Start container
	fmt.Println("  Starting container...")
	startCmd := exec.Command("docker", "start", containerName)
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Step 3: Wait for container to be ready
	fmt.Println("  Waiting for container to be ready...")
	time.Sleep(2 * time.Second)

	// Step 3.5: Fix shell config for better terminal experience
	checkPromptCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		"grep -q 'Custom Maestro prompt' /home/node/.zshrc")
	if err := checkPromptCmd.Run(); err != nil {
		// Prompt not found, apply all shell fixes
		shellFixCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
			`# Remove TERM override
sed -i '/^export TERM=xterm$/d' /home/node/.zshrc

# Disable powerlevel10k theme (causes missing font glyphs)
sed -i 's/^ZSH_THEME=.*/ZSH_THEME=""/' /home/node/.zshrc

# Add custom prompt with readable symbols and colors
cat >> /home/node/.zshrc << 'PROMPT_EOF'

# Custom Maestro prompt with colors and git status
autoload -Uz vcs_info
precmd_vcs_info() { vcs_info }
precmd_functions+=( precmd_vcs_info )
setopt prompt_subst
zstyle ':vcs_info:git:*' formats '%b'
zstyle ':vcs_info:*' enable git

# Git status indicators (matching maestro list command)
git_status_symbols() {
    if [[ -n ${vcs_info_msg_0_} ]]; then
        local git_status=""
        local changes=$(git status --porcelain 2>/dev/null | wc -l | tr -d ' ')
        local ahead=$(git rev-list --count @{u}..HEAD 2>/dev/null || echo "0")
        local behind=$(git rev-list --count HEAD..@{u} 2>/dev/null || echo "0")

        [[ $changes -gt 0 ]] && git_status+="Δ$changes "
        [[ $ahead -gt 0 ]] && git_status+="↑$ahead "
        [[ $behind -gt 0 ]] && git_status+="↓$behind "
        [[ -z $git_status ]] && git_status="✓ "

        echo "$git_status"
    fi
}

PROMPT='%F{green}%n%f  %F{blue}%~%f  %F{magenta}${vcs_info_msg_0_}%f %F{yellow}$(git_status_symbols)%f'
PROMPT_EOF`)
		if err := shellFixCmd.Run(); err != nil {
			fmt.Printf("  Warning: Failed to configure shell: %v\n", err)
		}
	}

	// Step 4: Get branch name for tmux config
	branchCmd := exec.Command("docker", "exec", containerName, "git", "-C", "/workspace", "branch", "--show-current")
	branchOutput, err := branchCmd.Output()
	branchName := "main"
	if err == nil {
		branchName = strings.TrimSpace(string(branchOutput))
	}

	// Step 5: Always write tmux config with true color support
	tmuxConfig := generateTmuxConfig(containerName, branchName)
	writeCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		fmt.Sprintf("cat > /home/node/.tmux.conf << 'EOF'\n%s\nEOF", tmuxConfig))
	if err := writeCmd.Run(); err != nil {
		fmt.Printf("  Warning: Failed to write tmux config: %v\n", err)
	}

	// Step 6: Check if tmux session exists
	checkCmd := exec.Command("docker", "exec", containerName, "tmux", "has-session", "-t", "main")
	if err := checkCmd.Run(); err != nil {
		fmt.Println("  Recreating tmux session...")

		// Start tmux with Claude
		tmuxStartCmd := exec.Command("docker", "exec", "-u", "node", containerName, "sh", "-c",
			"cd /workspace && HOME=/home/node tmux new-session -d -s main 'claude --dangerously-skip-permissions'")
		if err := tmuxStartCmd.Run(); err != nil {
			return fmt.Errorf("failed to start tmux session: %w", err)
		}

		time.Sleep(1 * time.Second)

		// Add shell window
		shellCmd := exec.Command("docker", "exec", containerName,
			"tmux", "new-window", "-t", "main:1", "-n", "shell", "-c", "cd /workspace && exec zsh")
		shellCmd.Run()

		// Rename and configure windows
		exec.Command("docker", "exec", containerName, "tmux", "rename-window", "-t", "main:0", "claude").Run()
		exec.Command("docker", "exec", containerName, "tmux", "select-window", "-t", "main:0").Run()
	}

	fmt.Printf("\n✅ Container %s restarted successfully\n", shortName)
	fmt.Printf("Connect with: maestro connect %s\n", shortName)

	return nil
}
