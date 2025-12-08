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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/uprockcom/maestro/assets"
	"github.com/uprockcom/maestro/pkg/container"
	"github.com/uprockcom/maestro/pkg/version"
	"github.com/spf13/cobra"
)

var (
	specFile    string
	noConnect   bool
	exactPrompt bool
)

var newCmd = &cobra.Command{
	Use:   "new [description]",
	Short: "Create a new development container",
	Long: `Create a new isolated development container with Claude Code.

Examples:
  mcl new "implement user authentication"
  mcl new --file specs/auth-design.md
  mcl new -f requirements.txt
  mcl new "add tests" --no-connect
  mcl new -e "/pr_review 123"     # Use exact prompt (no AI transformation)
  mcl new -en "/help"              # Combine flags: exact + no-connect`,
	RunE: runNew,
}

func init() {
	rootCmd.AddCommand(newCmd)
	newCmd.Flags().StringVarP(&specFile, "file", "f", "", "Read task specification from file")
	newCmd.Flags().BoolVarP(&noConnect, "no-connect", "n", false, "Don't automatically connect after creation")
	newCmd.Flags().BoolVarP(&exactPrompt, "exact", "e", false, "Use exact prompt without AI transformation")
}

func runNew(cmd *cobra.Command, args []string) error {
	// Get task description
	var taskDescription string
	if specFile != "" {
		content, err := os.ReadFile(specFile)
		if err != nil {
			return fmt.Errorf("failed to read spec file: %w", err)
		}
		taskDescription = string(content)
	} else if len(args) > 0 {
		taskDescription = strings.Join(args, " ")
	} else {
		fmt.Print("Enter task description: ")
		reader := bufio.NewReader(os.Stdin)
		desc, _ := reader.ReadString('\n')
		taskDescription = strings.TrimSpace(desc)
	}

	if taskDescription == "" {
		return fmt.Errorf("task description is required")
	}

	fmt.Printf("Creating container for: %s\n", truncateString(taskDescription, 80))

	// Step 1: Generate branch name and planning prompt using Claude
	branchName, planningPrompt, err := generateBranchAndPrompt(taskDescription, exactPrompt)
	if err != nil {
		return fmt.Errorf("failed to generate branch name: %w", err)
	}

	// Step 2: Get next container number
	containerName, err := getNextContainerName(branchName)
	if err != nil {
		return fmt.Errorf("failed to generate container name: %w", err)
	}

	fmt.Printf("Container name: %s\n", containerName)
	fmt.Printf("Branch name: %s\n", branchName)

	// Step 3: Build Docker image if needed
	if err := ensureDockerImage(); err != nil {
		return fmt.Errorf("failed to ensure Docker image: %w", err)
	}

	// Step 4: Start container
	if err := startContainer(containerName); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Step 5: Copy current directory to container
	fmt.Println("Copying project files to container...")
	if err := copyProjectToContainer(containerName); err != nil {
		return fmt.Errorf("failed to copy project: %w", err)
	}

	// Step 6: Copy additional folders
	if err := copyAdditionalFolders(containerName); err != nil {
		return fmt.Errorf("failed to copy additional folders: %w", err)
	}

	// Step 7: Initialize git branch in container
	if err := initializeGitBranch(containerName, branchName); err != nil {
		return fmt.Errorf("failed to initialize git branch: %w", err)
	}

	// Step 7.1: Configure git user if specified
	if err := configureGitUser(containerName); err != nil {
		fmt.Printf("Warning: Failed to configure git user: %v\n", err)
	}

	// Step 7.5: Convert SSH GitHub remotes to HTTPS for gh authentication
	if err := setupGitHubRemote(containerName); err != nil {
		// Don't fail container creation, just warn
		fmt.Printf("Warning: Failed to setup GitHub remote: %v\n", err)
	}

	// Step 8: Start tmux session with Claude
	if err := startTmuxSession(containerName, branchName, planningPrompt, exactPrompt); err != nil {
		return fmt.Errorf("failed to start tmux session: %w", err)
	}

	fmt.Printf("\n✅ Container %s is ready!\n", containerName)

	// Auto-connect unless --no-connect flag is set
	if !noConnect {
		fmt.Println("\nConnecting to container...")
		fmt.Println("Detach with: Ctrl+b d")
		fmt.Println("Switch windows: Ctrl+b 0 (Claude), Ctrl+b 1 (shell)")

		// Connect to tmux session
		connectCmd := exec.Command("docker", "exec", "-it", containerName, "tmux", "attach", "-t", "main")
		connectCmd.Stdin = os.Stdin
		connectCmd.Stdout = os.Stdout
		connectCmd.Stderr = os.Stderr

		if err := connectCmd.Run(); err != nil {
			fmt.Printf("\nWarning: Failed to connect: %v\n", err)
			fmt.Printf("You can connect later with: maestro connect %s\n", container.GetShortName(containerName, config.Containers.Prefix))
		}
	} else {
		fmt.Printf("Connect with: maestro connect %s\n", container.GetShortName(containerName, config.Containers.Prefix))
		fmt.Printf("Detach with: Ctrl+b d\n")
	}

	return nil
}

func generateBranchAndPrompt(taskDescription string, exact bool) (string, string, error) {
	// In exact mode, still generate branch name via AI but use literal prompt
	if exact {
		branchName, err := generateBranchNameOnly(taskDescription)
		if err != nil {
			// Fallback to simple branch name generation
			branchName = generateSimpleBranch(taskDescription)
		}
		// Return the exact task description as the prompt
		return branchName, taskDescription, nil
	}

	// Normal mode: Generate both branch name and planning prompt via AI
	// Create prompt for Claude to generate a concise branch name and planning prompt
	claudePrompt := fmt.Sprintf(`Given this task description:

%s

Generate:
1. A SHORT, concise git branch name (max 40 chars) following git-flow conventions (feat/, fix/, refactor/, etc.)
   - Use abbreviations and remove unnecessary words
   - Examples: "implement user authentication system" -> "feat/user-auth"
   - Examples: "fix bug in payment processor" -> "fix/payment-processor"
   - Examples: "refactor database connection pooling" -> "refactor/db-pooling"

2. A detailed planning prompt for implementing this task

Format your response EXACTLY as:
BRANCH: <branch-name>
PROMPT: <detailed-planning-prompt>`, taskDescription)

	// Call Claude CLI in --print mode to generate branch and prompt (using haiku for speed/cost)
	cmd := exec.Command("claude", "--print", "Generate branch name and prompt", "--model", "haiku", "--dangerously-skip-permissions")
	cmd.Stdin = strings.NewReader(claudePrompt)
	output, err := cmd.Output()
	if err != nil {
		// Fallback to simple branch name generation
		simpleBranch := generateSimpleBranch(taskDescription)
		planningPrompt := fmt.Sprintf(`Please plan the implementation for the following task:

%s

Break down the implementation into clear steps and identify key components that need to be created or modified.`, taskDescription)
		return simpleBranch, planningPrompt, nil
	}

	// Parse output
	outputStr := string(output)
	branchRe := regexp.MustCompile(`BRANCH:\s*(.+)`)
	promptRe := regexp.MustCompile(`PROMPT:\s*(.+)`)

	branchMatch := branchRe.FindStringSubmatch(outputStr)
	promptMatch := promptRe.FindStringSubmatch(outputStr)

	if len(branchMatch) > 1 && len(promptMatch) > 1 {
		branchName := strings.TrimSpace(branchMatch[1])

		// Enforce max length (40 chars) in case AI ignored the instruction
		if len(branchName) > 40 {
			branchName = branchName[:40]
			branchName = strings.TrimRight(branchName, "-/")
		}

		return branchName, strings.TrimSpace(promptMatch[1]), nil
	}

	// Fallback
	return generateSimpleBranch(taskDescription), taskDescription, nil
}

// generateBranchNameOnly generates just a branch name via AI, without a planning prompt
func generateBranchNameOnly(taskDescription string) (string, error) {
	claudePrompt := fmt.Sprintf(`Given this task description:

%s

Generate a SHORT, concise git branch name (max 40 chars) following git-flow conventions (feat/, fix/, refactor/, etc.)
- Use abbreviations and remove unnecessary words
- Examples: "implement user authentication system" -> "feat/user-auth"
- Examples: "fix bug in payment processor" -> "fix/payment-processor"
- Examples: "refactor database connection pooling" -> "refactor/db-pooling"

Respond with ONLY the branch name, nothing else.`, taskDescription)

	// Call Claude CLI in --print mode to generate just the branch name (using haiku for speed/cost)
	cmd := exec.Command("claude", "--print", "Generate branch name", "--model", "haiku", "--dangerously-skip-permissions")
	cmd.Stdin = strings.NewReader(claudePrompt)
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Parse output - just take the first line and trim it
	branchName := strings.TrimSpace(strings.Split(string(output), "\n")[0])
	if branchName == "" {
		return "", fmt.Errorf("empty branch name from AI")
	}

	// Enforce max length (40 chars) in case AI ignored the instruction
	if len(branchName) > 40 {
		branchName = branchName[:40]
		branchName = strings.TrimRight(branchName, "-/")
	}

	return branchName, nil
}

func generateSimpleBranch(description string) string {
	// Simple branch name generation from description
	desc := strings.ToLower(description)

	// Remove common filler words to keep it concise
	fillerWords := []string{"the", "a", "an", "and", "or", "but", "in", "on", "at", "to", "for"}
	words := strings.Fields(desc)
	var filtered []string
	for _, word := range words {
		isFillerWord := false
		for _, filler := range fillerWords {
			if word == filler {
				isFillerWord = true
				break
			}
		}
		if !isFillerWord {
			filtered = append(filtered, word)
		}
	}
	desc = strings.Join(filtered, " ")

	// Convert to branch-safe format
	desc = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(desc, "-")
	desc = strings.Trim(desc, "-")

	// Keep it short (max 35 chars for the description part)
	if len(desc) > 35 {
		desc = desc[:35]
	}
	desc = strings.TrimRight(desc, "-")

	return fmt.Sprintf("feat/%s", desc)
}

func getNextContainerName(branchName string) (string, error) {
	// Convert branch to container-friendly name
	baseName := strings.ReplaceAll(branchName, "/", "-")
	baseName = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(baseName, "-")

	// CRITICAL: Limit total length to avoid hostname errors
	// Linux hostname limit is 64 chars. We need room for prefix + base + suffix
	// Format: {prefix}{basename}-{num}
	// Example: mcl-feat-add-auth-1 (prefix=4, suffix=2, leaves 58 for basename)
	maxBaseLength := 50 // Conservative limit leaving room for prefix/suffix
	if len(baseName) > maxBaseLength {
		baseName = baseName[:maxBaseLength]
		baseName = strings.TrimRight(baseName, "-") // Remove trailing dash if truncated mid-word
	}

	// Check existing containers
	cmd := exec.Command("docker", "ps", "-a", "--format", "{{.Names}}")
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}

	// Find highest number for this base name
	containerPrefix := config.Containers.Prefix + baseName
	maxNum := 0
	for _, name := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(name, containerPrefix+"-") {
			parts := strings.Split(name, "-")
			if len(parts) > 0 {
				var num int
				if _, err := fmt.Sscanf(parts[len(parts)-1], "%d", &num); err == nil {
					if num > maxNum {
						maxNum = num
					}
				}
			}
		}
	}

	return fmt.Sprintf("%s-%d", containerPrefix, maxNum+1), nil
}

// getDockerImage returns the container image to use, prioritizing embedded version.
// Priority:
//  1. Embedded version (from pkg/version) - PRODUCTION PATH
//  2. Config override (if user explicitly set a different image)
func getDockerImage() string {
	// Get the version-synchronized image (primary source of truth)
	versionImage := version.GetContainerImage()

	// If config is empty or matches default, use version-synchronized image
	if config.Containers.Image == "" || config.Containers.Image == "ghcr.io/uprockcom/maestro:latest" {
		return versionImage
	}

	// User has explicitly overridden - respect their choice
	// This allows advanced users to pin to specific versions or use local builds
	return config.Containers.Image
}

func ensureDockerImage() error {
	// Use the image determined by priority logic
	imageName := getDockerImage()
	cmd := exec.Command("docker", "images", "-q", imageName)
	output, err := cmd.Output()
	if err != nil {
		return err
	}

	if len(output) == 0 {
		// Image doesn't exist - try to pull from registry first
		if strings.Contains(imageName, "ghcr.io") || strings.Contains(imageName, "docker.io") {
			fmt.Printf("Pulling Docker image from registry: %s\n", imageName)
			pullCmd := exec.Command("docker", "pull", imageName)
			pullCmd.Stdout = os.Stdout
			pullCmd.Stderr = os.Stderr
			if err := pullCmd.Run(); err == nil {
				fmt.Println("✓ Image pulled successfully")
				return nil
			}
			fmt.Println("Warning: Failed to pull from registry, will try to build locally...")
		}

		// Fall back to building locally (for development)
		fmt.Println("Building Docker image locally...")
		dockerDir := "docker"
		if _, err := os.Stat(dockerDir); os.IsNotExist(err) {
			// Try relative to mcl binary location
			mclDir := filepath.Dir(os.Args[0])
			dockerDir = filepath.Join(mclDir, "docker")
		}

		// Check if docker directory exists
		if _, err := os.Stat(dockerDir); os.IsNotExist(err) {
			return fmt.Errorf("docker image not found and cannot build (no docker/ directory found)\nTry: docker pull %s", imageName)
		}

		buildCmd := exec.Command("docker", "build", "-t", imageName, dockerDir)
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
		return buildCmd.Run()
	}

	return nil
}

func startContainer(containerName string) error {
	// Ensure Claude auth directory exists
	authPath := expandPath(config.Claude.AuthPath)
	if err := os.MkdirAll(authPath, 0755); err != nil {
		return fmt.Errorf("failed to create Claude auth directory: %w", err)
	}

	// Check if credentials and config exist, warn if not
	credPath := filepath.Join(authPath, ".credentials.json")
	configPath := filepath.Join(authPath, ".claude.json")

	credExists := false
	configExists := false

	if _, err := os.Stat(credPath); err == nil {
		credExists = true
	}
	if _, err := os.Stat(configPath); err == nil {
		configExists = true
	}

	// Skip credential checks when using Bedrock (uses AWS auth instead)
	if config.Bedrock.Enabled {
		if !configExists {
			fmt.Println("⚠️  Warning: Missing .claude.json configuration.")
			fmt.Println("Run 'maestro auth' to copy config from ~/.claude")
		}
	} else if !credExists || !configExists {
		fmt.Println("⚠️  Warning: Claude authentication/configuration incomplete.")
		if !credExists {
			fmt.Println("  - Missing .credentials.json")
		}
		if !configExists {
			fmt.Println("  - Missing .claude.json")
		}
		fmt.Println("Run 'maestro auth' to complete setup before creating containers.")
		fmt.Println("Continuing anyway - you'll need to authenticate in the container...")
	} else if credExists {
		// Check token expiration
		if creds, err := container.ReadCredentials(credPath); err == nil {
			if container.IsTokenExpired(creds) {
				fmt.Println("\n⚠️  WARNING: Authentication token is EXPIRED!")
				fmt.Printf("   Status: %s\n", container.FormatExpiration(creds))
				fmt.Println("   Run 'maestro auth' or 'maestro refresh-tokens' to get a fresh token.")
				fmt.Print("\nContinue creating container with expired token? (y/N): ")
				var response string
				fmt.Scanln(&response)
				if response != "y" && response != "Y" {
					return fmt.Errorf("cancelled by user - run 'maestro refresh-tokens' or 'maestro auth' first")
				}
			} else {
				timeLeft := container.TimeUntilExpiration(creds)
				if timeLeft < 24*time.Hour {
					fmt.Printf("\n⚠️  Token expires in %.1f hours. Consider running 'maestro auth' soon.\n\n",
						timeLeft.Hours())
				}
			}
		}
	}

	args := []string{
		"run", "-d",
		"--name", containerName,
		"--hostname", containerName,
		"--cap-add", "NET_ADMIN", // For iptables
		"--memory", config.Containers.Resources.Memory,
		"--cpus", config.Containers.Resources.CPUs,
	}

	// Add cache volumes for persistence
	args = append(args,
		"-v", fmt.Sprintf("%s-npm:/home/node/.npm", containerName),
		"-v", fmt.Sprintf("%s-uv:/home/node/.cache/uv", containerName),
		"-v", fmt.Sprintf("%s-history:/commandhistory", containerName),
	)

	// Mount host SSL certificates for corporate proxies (Zscaler, etc.)
	// This allows the container to use the same CA trust store as the host
	if _, err := os.Stat("/etc/ssl/certs/ca-certificates.crt"); err == nil {
		args = append(args,
			"-v", "/etc/ssl/certs:/etc/ssl/certs:ro",
			"-e", "NODE_EXTRA_CA_CERTS=/etc/ssl/certs/ca-certificates.crt",
			"-e", "NODE_OPTIONS=--use-openssl-ca",
			"-e", "SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt",
			"-e", "CURL_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt",
			"-e", "REQUESTS_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt",
		)
	}

	// Mount AWS config and credentials for Bedrock support
	if config.AWS.Enabled || config.Bedrock.Enabled {
		homeDir, _ := os.UserHomeDir()
		awsDir := filepath.Join(homeDir, ".aws")
		if _, err := os.Stat(awsDir); err == nil {
			// Mount as read-write so SSO token refresh can work
			args = append(args,
				"-v", fmt.Sprintf("%s:/home/node/.aws", awsDir),
			)
		}

		// Set AWS environment variables
		if config.AWS.Profile != "" {
			args = append(args, "-e", fmt.Sprintf("AWS_PROFILE=%s", config.AWS.Profile))
		}
		if config.AWS.Region != "" {
			args = append(args, "-e", fmt.Sprintf("AWS_REGION=%s", config.AWS.Region))
			args = append(args, "-e", fmt.Sprintf("AWS_DEFAULT_REGION=%s", config.AWS.Region))
		}

		// Set Bedrock environment variables
		if config.Bedrock.Enabled {
			args = append(args, "-e", "CLAUDE_CODE_USE_BEDROCK=1")
			if config.Bedrock.Model != "" {
				args = append(args, "-e", fmt.Sprintf("ANTHROPIC_MODEL=%s", config.Bedrock.Model))
			}
		}
	}

	// Mount SSH agent socket for git authentication (more secure than mounting keys)
	// Only the agent socket is exposed - private keys stay on the host
	if config.SSH.Enabled {
		sshAuthSock := os.Getenv("SSH_AUTH_SOCK")
		if sshAuthSock != "" {
			args = append(args,
				"-v", fmt.Sprintf("%s:/ssh-agent", sshAuthSock),
				"-e", "SSH_AUTH_SOCK=/ssh-agent",
			)
		} else {
			fmt.Println("Warning: SSH enabled but SSH_AUTH_SOCK not set. Run 'ssh-add' first.")
		}
	}

	// Use version-synchronized image (or config override if set)
	args = append(args, getDockerImage())

	cmd := exec.Command("docker", args...)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Wait for container startup script to complete
	// The startup script runs npm update and claude --version, which can take several seconds
	fmt.Println("Waiting for container initialization...")
	for i := 0; i < 30; i++ {
		// Check if startup script has finished by looking for the "sleep infinity" process
		checkCmd := exec.Command("docker", "exec", containerName, "pgrep", "-f", "sleep infinity")
		if err := checkCmd.Run(); err == nil {
			// Found sleep infinity - startup is complete
			break
		}
		if i == 29 {
			fmt.Println("Warning: Container startup taking longer than expected, continuing anyway...")
		}
		time.Sleep(1 * time.Second)
	}

	// Fix shell config for better terminal experience
	shellFixCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		`# Remove TERM override
sed -i '/^export TERM=xterm$/d' /home/node/.zshrc

# Disable powerlevel10k theme (causes missing font glyphs)
sed -i 's/^ZSH_THEME=.*/ZSH_THEME=""/' /home/node/.zshrc

# Add custom prompt with readable symbols and colors
cat >> /home/node/.zshrc << 'PROMPT_EOF'

# Custom MCL prompt with colors and git status
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
		fmt.Printf("Warning: Failed to configure shell: %v\n", err)
	}

	// Copy credentials and config files to container if they exist
	// These files are shared across all containers, while other state files (debug/, statsig/) are container-specific
	if credExists || configExists {
		fmt.Println("Copying Claude credentials and configuration to container...")

		// Create .claude directory in container
		mkdirCmd := exec.Command("docker", "exec", containerName, "mkdir", "-p", "/home/node/.claude")
		if err := mkdirCmd.Run(); err != nil {
			fmt.Printf("Warning: Failed to create .claude directory: %v\n", err)
		}

		// Copy credentials file to .claude directory
		if credExists {
			copyCredCmd := exec.Command("docker", "cp", credPath, fmt.Sprintf("%s:/home/node/.claude/.credentials.json", containerName))
			if err := copyCredCmd.Run(); err != nil {
				fmt.Printf("Warning: Failed to copy credentials: %v\n", err)
			}
		}

		// Copy config file to home directory (NOT inside .claude/)
		// .claude.json lives at /home/node/.claude.json, not /home/node/.claude/.claude.json
		if configExists {
			copyConfigCmd := exec.Command("docker", "cp", configPath, fmt.Sprintf("%s:/home/node/.claude.json", containerName))
			if err := copyConfigCmd.Run(); err != nil {
				fmt.Printf("Warning: Failed to copy config: %v\n", err)
			}
		}

		// Fix ownership of .claude directory and .claude.json file
		chownCmd := exec.Command("docker", "exec", "-u", "root", containerName, "chown", "-R", "node:node", "/home/node/.claude")
		if err := chownCmd.Run(); err != nil {
			fmt.Printf("Warning: Failed to fix .claude ownership: %v\n", err)
		}

		if configExists {
			chownConfigCmd := exec.Command("docker", "exec", "-u", "root", containerName, "chown", "node:node", "/home/node/.claude.json")
			if err := chownConfigCmd.Run(); err != nil {
				fmt.Printf("Warning: Failed to fix .claude.json ownership: %v\n", err)
			}
		}
	}

	// Copy GitHub CLI config if enabled
	if config.GitHub.Enabled {
		ghConfigPath := expandPath(config.GitHub.ConfigPath)
		if _, err := os.Stat(ghConfigPath); err == nil {
			fmt.Println("Copying GitHub CLI configuration to container...")

			// Create .config directory in container
			mkdirCmd := exec.Command("docker", "exec", containerName, "mkdir", "-p", "/home/node/.config")
			if err := mkdirCmd.Run(); err != nil {
				fmt.Printf("Warning: Failed to create .config directory: %v\n", err)
			}

			// Copy entire gh config directory
			copyGhCmd := exec.Command("docker", "cp", ghConfigPath, fmt.Sprintf("%s:/home/node/.config/gh", containerName))
			if err := copyGhCmd.Run(); err != nil {
				fmt.Printf("Warning: Failed to copy GitHub config: %v\n", err)
			} else {
				// Fix ownership
				chownGhCmd := exec.Command("docker", "exec", "-u", "root", containerName, "chown", "-R", "node:node", "/home/node/.config")
				if err := chownGhCmd.Run(); err != nil {
					fmt.Printf("Warning: Failed to fix .config ownership: %v\n", err)
				}
			}
		} else {
			fmt.Printf("⚠️  Warning: GitHub integration enabled but config not found at %s\n", ghConfigPath)
			fmt.Println("   Run 'gh auth login' on the host to set up GitHub CLI authentication")
		}
	}

	// Initialize firewall
	fmt.Println("Setting up firewall...")
	if err := initializeFirewall(containerName); err != nil {
		fmt.Printf("Warning: Failed to initialize firewall: %v\n", err)
	}

	return nil
}

func copyProjectToContainer(containerName string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Create tar of current directory (excluding .git if it's huge)
	tarCmd := exec.Command("tar", "-czf", "-", "--exclude=node_modules", "--exclude=.git", ".")
	tarCmd.Dir = cwd

	// Pipe to docker cp
	dockerCmd := exec.Command("docker", "exec", "-i", containerName, "tar", "-xzf", "-", "-C", "/workspace")

	// Connect pipes
	pipe, err := tarCmd.StdoutPipe()
	if err != nil {
		return err
	}
	dockerCmd.Stdin = pipe

	// Start both commands
	if err := tarCmd.Start(); err != nil {
		return err
	}
	if err := dockerCmd.Start(); err != nil {
		return err
	}

	// Wait for completion
	if err := tarCmd.Wait(); err != nil {
		return err
	}
	if err := dockerCmd.Wait(); err != nil {
		return err
	}

	// Copy .git separately if it exists
	if _, err := os.Stat(".git"); err == nil {
		gitCmd := exec.Command("docker", "cp", ".git", fmt.Sprintf("%s:/workspace/", containerName))
		if err := gitCmd.Run(); err != nil {
			fmt.Printf("Warning: Failed to copy .git: %v\n", err)
		}
	}

	// Fix ownership of /workspace to node user
	chownCmd := exec.Command("docker", "exec", containerName, "sh", "-c", "sudo chown -R node:node /workspace")
	if err := chownCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to fix ownership: %v\n", err)
	}

	return nil
}

func copyAdditionalFolders(containerName string) error {
	for _, folder := range config.Sync.AdditionalFolders {
		expandedPath := expandPath(folder)
		if _, err := os.Stat(expandedPath); err != nil {
			fmt.Printf("Skipping %s (not found)\n", folder)
			continue
		}

		baseName := filepath.Base(expandedPath)
		fmt.Printf("Copying %s...\n", baseName)

		cmd := exec.Command("docker", "cp", expandedPath, fmt.Sprintf("%s:/workspace/../%s", containerName, baseName))
		if err := cmd.Run(); err != nil {
			fmt.Printf("Warning: Failed to copy %s: %v\n", folder, err)
		}
	}
	return nil
}

func initializeGitBranch(containerName, branchName string) error {
	// Fix git ownership issue first
	safeCmd := exec.Command("docker", "exec", containerName, "git", "config", "--global", "--add", "safe.directory", "/workspace")
	if err := safeCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to set safe.directory: %v\n", err)
	}

	// Check if git repo exists
	checkCmd := exec.Command("docker", "exec", containerName, "test", "-d", "/workspace/.git")
	if err := checkCmd.Run(); err != nil {
		// Initialize git if not exists
		initCmd := exec.Command("docker", "exec", containerName, "sh", "-c", "cd /workspace && git init")
		if err := initCmd.Run(); err != nil {
			return err
		}
	}

	// Create and checkout new branch
	cmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		fmt.Sprintf("cd /workspace && git checkout -b %s 2>/dev/null || git checkout %s", branchName, branchName))
	return cmd.Run()
}

func configureGitUser(containerName string) error {
	if config.Git.UserName != "" {
		cmd := exec.Command("docker", "exec", containerName, "git", "config", "--global", "user.name", config.Git.UserName)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to set git user.name: %w", err)
		}
	}
	if config.Git.UserEmail != "" {
		cmd := exec.Command("docker", "exec", containerName, "git", "config", "--global", "user.email", config.Git.UserEmail)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to set git user.email: %w", err)
		}
	}
	return nil
}

func setupGitHubRemote(containerName string) error {
	// Check if origin remote exists
	getOriginCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		"cd /workspace && git config --get remote.origin.url")
	originOutput, err := getOriginCmd.Output()
	if err != nil {
		// No origin, nothing to do
		return nil
	}

	originURL := strings.TrimSpace(string(originOutput))
	if originURL == "" {
		return nil
	}

	// Check if it's a GitHub SSH URL
	sshPattern := regexp.MustCompile(`^git@github\.com:(.+/.+?)(?:\.git)?$`)
	matches := sshPattern.FindStringSubmatch(originURL)

	if len(matches) == 0 {
		// Not a GitHub SSH URL, nothing to do
		return nil
	}

	// Extract the user/repo path
	repoPath := matches[1]
	if !strings.HasSuffix(repoPath, ".git") {
		repoPath = repoPath + ".git"
	}

	// Convert to HTTPS URL
	httpsURL := fmt.Sprintf("https://github.com/%s", repoPath)

	fmt.Printf("Converting SSH remote to HTTPS for GitHub authentication...\n")
	fmt.Printf("  Old: %s\n", originURL)
	fmt.Printf("  New: %s\n", httpsURL)

	// Update the origin URL
	setOriginCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		fmt.Sprintf("cd /workspace && git remote set-url origin %s", httpsURL))
	if err := setOriginCmd.Run(); err != nil {
		return fmt.Errorf("failed to update origin URL: %w", err)
	}

	// Configure git to use gh for authentication
	// Only do this if GitHub integration is enabled
	if config.GitHub.Enabled {
		fmt.Println("Configuring git to use GitHub CLI for authentication...")
		ghSetupCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
			"cd /workspace && gh auth setup-git")
		if err := ghSetupCmd.Run(); err != nil {
			return fmt.Errorf("failed to setup gh auth: %w", err)
		}
		fmt.Println("✓ GitHub authentication configured")
	}

	return nil
}

func startTmuxSession(containerName, branchName, planningPrompt string, exactPrompt bool) error {
	// Create tmux configuration with status line showing container info and true color support
	tmuxConfig := generateTmuxConfig(containerName, branchName)

	// Write tmux config to container - use cat with heredoc to preserve newlines
	writeCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		fmt.Sprintf("cat > /home/node/.tmux.conf << 'EOF'\n%s\nEOF", tmuxConfig))
	if err := writeCmd.Run(); err != nil {
		return err
	}

	// Note: Config will be loaded when tmux session starts below

	// Prepare the task prompt that will be sent via tmux
	var taskPrompt string
	if exactPrompt {
		// In exact mode, send the prompt as-is without any wrapper
		taskPrompt = planningPrompt
	} else {
		// In normal mode, wrap with planning instructions
		taskPrompt = fmt.Sprintf(`%s

Please analyze this task and create a detailed implementation plan. Do not start coding yet - just plan the implementation.`, planningPrompt)
	}

	// Start tmux session with Claude running directly
	// Running Claude as the tmux command (not via send-keys) preserves the environment correctly
	// Explicitly set HOME and user to ensure credentials are found
	tmuxCmd := exec.Command("docker", "exec", "-u", "node", containerName, "sh", "-c",
		"cd /workspace && HOME=/home/node tmux new-session -d -s main 'claude --dangerously-skip-permissions'")

	// Capture output for debugging
	var stdout, stderr bytes.Buffer
	tmuxCmd.Stdout = &stdout
	tmuxCmd.Stderr = &stderr

	if err := tmuxCmd.Run(); err != nil {
		fmt.Printf("Tmux command stdout: %s\n", stdout.String())
		fmt.Printf("Tmux command stderr: %s\n", stderr.String())
		return fmt.Errorf("failed to start tmux: %w", err)
	}

	// Wait for tmux session to be ready
	fmt.Println("Waiting for tmux session to start...")
	for i := 0; i < 10; i++ {
		checkCmd := exec.Command("docker", "exec", "-u", "node", containerName, "tmux", "has-session", "-t", "main")
		var checkOut, checkErr bytes.Buffer
		checkCmd.Stdout = &checkOut
		checkCmd.Stderr = &checkErr

		if err := checkCmd.Run(); err == nil {
			break
		}
		if i == 9 {
			fmt.Printf("Timeout waiting for tmux session. Last check stderr: %s\n", checkErr.String())
			// List all tmux sessions for debugging
			listCmd := exec.Command("docker", "exec", "-u", "node", containerName, "tmux", "ls")
			listOut, _ := listCmd.CombinedOutput()
			fmt.Printf("All tmux sessions: %s\n", string(listOut))
			// Check if Claude process is running
			psCmd := exec.Command("docker", "exec", "-u", "node", containerName, "ps", "aux")
			psOut, _ := psCmd.CombinedOutput()
			fmt.Printf("Running processes:\n%s\n", string(psOut))
			return fmt.Errorf("tmux session failed to start after 5 seconds")
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Enable bell monitoring on the Claude window so we can detect when it needs attention
	monitorCmd := exec.Command("docker", "exec", "-u", "node", containerName,
		"tmux", "set-window-option", "-t", "main:0", "monitor-bell", "on")
	if err := monitorCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to enable bell monitoring: %v\n", err)
	}

	// Enable silence monitoring - triggers when Claude has no output for 10 seconds
	// This catches when Claude is paused waiting for input
	silenceCmd := exec.Command("docker", "exec", "-u", "node", containerName,
		"tmux", "set-window-option", "-t", "main:0", "monitor-silence", "10")
	if err := silenceCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to enable silence monitoring: %v\n", err)
	}

	// Create a background script to send the initial prompt
	// First accepts the bypass permissions prompt, then sends the task
	autoInputScript := fmt.Sprintf(`#!/bin/sh
# Wait for Claude to start and show the bypass permissions prompt
sleep 3

# Accept the bypass permissions prompt by pressing Down then Enter
tmux send-keys -t main:0 Down 2>/dev/null
sleep 0.3
tmux send-keys -t main:0 Enter 2>/dev/null

# Wait for Claude to fully initialize after accepting
sleep 3

# Send the task prompt
cat > /tmp/prompt-input.txt << 'PROMPT_EOF'
%s
PROMPT_EOF

# Send the prompt line by line to avoid issues
while IFS= read -r line || [ -n "$line" ]; do
    printf "%%s\n" "$line" | tmux load-buffer -
    tmux paste-buffer -t main:0 -d 2>/dev/null
done < /tmp/prompt-input.txt

# Send Enter to submit
sleep 0.5
tmux send-keys -t main:0 C-m 2>/dev/null
`, taskPrompt)

	fmt.Println("Setting up automated Claude startup...")

	// Write and execute the auto-input script in the background
	writeAutoInput := exec.Command("docker", "exec", containerName, "sh", "-c",
		fmt.Sprintf("cat > /tmp/auto-input.sh << 'EOF'\n%s\nEOF\nchmod +x /tmp/auto-input.sh", autoInputScript))
	if err := writeAutoInput.Run(); err != nil {
		return fmt.Errorf("failed to write auto-input script: %w", err)
	}

	// Run the auto-input script in the background as node user
	runAutoInput := exec.Command("docker", "exec", "-d", "-u", "node", containerName, "/tmp/auto-input.sh")
	if err := runAutoInput.Run(); err != nil {
		fmt.Printf("Warning: Failed to start auto-input script: %v\n", err)
	}

	fmt.Println("Automated input started for Claude...")

	// Window 1: Shell
	newWinCmd := exec.Command("docker", "exec", "-u", "node", containerName,
		"tmux", "new-window", "-t", "main:1", "-n", "shell", "-c", "cd /workspace && exec zsh")
	if err := newWinCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to create shell window: %v\n", err)
	}

	// Rename window 0
	renameCmd := exec.Command("docker", "exec", "-u", "node", containerName,
		"tmux", "rename-window", "-t", "main:0", "claude")
	if err := renameCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to rename claude window: %v\n", err)
	}

	// Set Claude window as active
	selectCmd := exec.Command("docker", "exec", containerName,
		"tmux", "select-window", "-t", "main:0")
	if err := selectCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to select claude window: %v\n", err)
	}

	return nil
}

func initializeFirewall(containerName string) error {
	// Write embedded firewall script to a temporary file
	tmpFile, err := os.CreateTemp("", "init-firewall-*.sh")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(assets.FirewallScript); err != nil {
		return fmt.Errorf("failed to write firewall script: %w", err)
	}
	tmpFile.Close()

	// Copy script to container
	copyCmd := exec.Command("docker", "cp", tmpFile.Name(), fmt.Sprintf("%s:/usr/local/bin/init-firewall.sh", containerName))
	if err := copyCmd.Run(); err != nil {
		return err
	}

	// Make the script executable (as root)
	chmodCmd := exec.Command("docker", "exec", "-u", "root", containerName, "chmod", "+x", "/usr/local/bin/init-firewall.sh")
	if err := chmodCmd.Run(); err != nil {
		return fmt.Errorf("failed to make firewall script executable: %w", err)
	}

	// Write allowed domains to container (using sudo for /etc write access)
	domainsList := strings.Join(config.Firewall.AllowedDomains, "\n")
	writeDomainsCmd := exec.Command("docker", "exec", "-u", "root", containerName, "sh", "-c",
		fmt.Sprintf("echo '%s' > /etc/allowed-domains.txt", domainsList))
	if err := writeDomainsCmd.Run(); err != nil {
		return fmt.Errorf("failed to write allowed domains: %w", err)
	}

	// Write internal DNS config if configured (for corporate networks)
	if config.Firewall.InternalDNS != "" {
		writeInternalDNSCmd := exec.Command("docker", "exec", "-u", "root", containerName, "sh", "-c",
			fmt.Sprintf("echo '%s' > /etc/internal-dns.txt", config.Firewall.InternalDNS))
		if err := writeInternalDNSCmd.Run(); err != nil {
			fmt.Printf("Warning: Failed to write internal DNS config: %v\n", err)
		}
	}

	// Write internal domains if configured
	if len(config.Firewall.InternalDomains) > 0 {
		internalDomainsList := strings.Join(config.Firewall.InternalDomains, "\n")
		writeInternalDomainsCmd := exec.Command("docker", "exec", "-u", "root", containerName, "sh", "-c",
			fmt.Sprintf("echo '%s' > /etc/internal-domains.txt", internalDomainsList))
		if err := writeInternalDomainsCmd.Run(); err != nil {
			fmt.Printf("Warning: Failed to write internal domains config: %v\n", err)
		}
	}

	// Run firewall initialization as root (with timeout in background)
	// We run it in the background because the verification steps can hang
	firewallCmd := exec.Command("docker", "exec", "-u", "root", "-d", containerName, "/usr/local/bin/init-firewall.sh")
	if err := firewallCmd.Run(); err != nil {
		return fmt.Errorf("failed to start firewall initialization: %w", err)
	}

	// Give the firewall a moment to initialize
	time.Sleep(1 * time.Second)

	fmt.Println("Firewall initialization started in background")

	// Copy configured apps to container
	if err := copyAppsToContainer(containerName); err != nil {
		fmt.Printf("Warning: Failed to copy apps: %v\n", err)
	}

	return nil
}

func copyAppsToContainer(containerName string) error {
	if len(config.Apps) == 0 {
		return nil // No apps configured
	}

	fmt.Printf("Copying %d configured app(s) to container...\n", len(config.Apps))

	for name, sourcePath := range config.Apps {
		expandedPath := expandPath(sourcePath)

		// Check for Linux-specific variant first (for cross-platform binaries)
		linuxPath := expandedPath + ".linux_aarch64"
		actualPath := expandedPath
		if _, err := os.Stat(linuxPath); err == nil {
			actualPath = linuxPath
		}

		// Check if source exists
		if _, err := os.Stat(actualPath); err != nil {
			fmt.Printf("  ⚠  Skipping %s (source not found: %s)\n", name, sourcePath)
			continue
		}

		// Copy to container (with original name, not platform suffix)
		destPath := fmt.Sprintf("/usr/local/bin/%s", name)
		containerPath := fmt.Sprintf("%s:%s", containerName, destPath)

		cpCmd := exec.Command("docker", "cp", actualPath, containerPath)
		if err := cpCmd.Run(); err != nil {
			fmt.Printf("  ⚠  Failed to copy %s: %v\n", name, err)
			continue
		}

		// Make executable and set ownership
		chmodCmd := exec.Command("docker", "exec", "-u", "root", containerName,
			"sh", "-c", fmt.Sprintf("chmod +x %s && chown node:node %s", destPath, destPath))
		if err := chmodCmd.Run(); err != nil {
			fmt.Printf("  ⚠  %s copied but failed to set permissions\n", name)
			continue
		}

		fmt.Printf("  ✓ %s\n", name)
	}

	return nil
}

func expandPath(path string) string {
	if path == "" {
		return ""
	}

	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Printf("Warning: Could not expand home directory: %v\n", err)
			return path
		}
		return filepath.Join(home, path[2:])
	}

	// Handle just "~" by itself
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}

	return path
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// CreateContainerFromTUI creates a new container with the given parameters (called from TUI)
func CreateContainerFromTUI(taskDescription, branchNameOverride string, skipConnect, exact bool) error {
	if taskDescription == "" {
		return fmt.Errorf("task description is required")
	}

	fmt.Printf("Creating container for: %s\n", truncateString(taskDescription, 80))

	// Step 1: Generate branch name (use override if provided, otherwise generate)
	var branchName string
	var planningPrompt string
	var err error

	if branchNameOverride != "" {
		// User provided custom branch name
		branchName = branchNameOverride
		planningPrompt = taskDescription // Use description as prompt
	} else {
		// Generate branch name and planning prompt using Claude
		branchName, planningPrompt, err = generateBranchAndPrompt(taskDescription, exact)
		if err != nil {
			return fmt.Errorf("failed to generate branch name: %w", err)
		}
	}

	// Step 2: Get next container number
	containerName, err := getNextContainerName(branchName)
	if err != nil {
		return fmt.Errorf("failed to generate container name: %w", err)
	}

	fmt.Printf("Container name: %s\n", containerName)
	fmt.Printf("Branch name: %s\n", branchName)

	// Step 3: Build Docker image if needed
	if err := ensureDockerImage(); err != nil {
		return fmt.Errorf("failed to ensure Docker image: %w", err)
	}

	// Step 4: Start container
	if err := startContainer(containerName); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Step 5: Copy current directory to container
	fmt.Println("Copying project files to container...")
	if err := copyProjectToContainer(containerName); err != nil {
		return fmt.Errorf("failed to copy project: %w", err)
	}

	// Step 6: Copy additional folders
	if err := copyAdditionalFolders(containerName); err != nil {
		return fmt.Errorf("failed to copy additional folders: %w", err)
	}

	// Step 7: Initialize git branch in container
	if err := initializeGitBranch(containerName, branchName); err != nil {
		return fmt.Errorf("failed to initialize git branch: %w", err)
	}

	// Step 7.1: Configure git user if specified
	if err := configureGitUser(containerName); err != nil {
		fmt.Printf("Warning: Failed to configure git user: %v\n", err)
	}

	// Step 7.5: Convert SSH GitHub remotes to HTTPS for gh authentication
	if err := setupGitHubRemote(containerName); err != nil {
		// Don't fail container creation, just warn
		fmt.Printf("Warning: Failed to setup GitHub remote: %v\n", err)
	}

	// Step 8: Start tmux session with Claude
	if err := startTmuxSession(containerName, branchName, planningPrompt, exact); err != nil {
		return fmt.Errorf("failed to start tmux session: %w", err)
	}

	fmt.Printf("\n✅ Container %s is ready!\n", containerName)

	// Auto-connect unless skipConnect is true
	if !skipConnect {
		fmt.Println("\nConnecting to container...")
		fmt.Println("Detach with: Ctrl+b d")
		fmt.Println("Switch windows: Ctrl+b 0 (Claude), Ctrl+b 1 (shell)")

		// Connect to tmux session
		connectCmd := exec.Command("docker", "exec", "-it", containerName, "tmux", "attach", "-t", "main")
		connectCmd.Stdin = os.Stdin
		connectCmd.Stdout = os.Stdout
		connectCmd.Stderr = os.Stderr

		if err := connectCmd.Run(); err != nil {
			fmt.Printf("\nWarning: Failed to connect: %v\n", err)
			fmt.Printf("You can connect later with: maestro connect %s\n", container.GetShortName(containerName, config.Containers.Prefix))
		}
	} else {
		fmt.Printf("Connect with: maestro connect %s\n", container.GetShortName(containerName, config.Containers.Prefix))
		fmt.Printf("Detach with: Ctrl+b d\n")
	}

	return nil
}

