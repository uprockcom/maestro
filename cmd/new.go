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
	"crypto/rand"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/uprockcom/maestro/assets"
	"github.com/uprockcom/maestro/pkg/container"
	"github.com/uprockcom/maestro/pkg/version"
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

	// Validate the branch name and prompt user if invalid
	if !isValidBranchName(branchName) {
		fmt.Printf("Generated branch name '%s' is invalid.\n", branchName)
		branchName, err = promptUserForBranchName(taskDescription)
		if err != nil {
			return fmt.Errorf("failed to get branch name: %w", err)
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
	// Includes retry logic for robustness
	const maxRetries = 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		var claudePrompt string
		if attempt == 1 {
			claudePrompt = fmt.Sprintf(`Analyze this task and generate a branch name and planning prompt.

Task: %s

Step 1 - BRANCH NAME:
- Extract the CORE GOAL (ignore setup instructions like "read file X", "switch branch")
- Include key identifiers (PR numbers, issue IDs, feature names)
- Use prefix: feat/ fix/ refactor/ docs/ test/ review/ chore/
- Max 40 chars, lowercase, only letters/numbers/hyphens

Examples:
- "Read spec.md and add user auth" -> feat/user-auth
- "Review PR #42 for payments" -> review/pr-42
- "Fix issue #99 with login" -> fix/issue-99-login

Step 2 - PLANNING PROMPT:
- Create a detailed prompt for implementing this task

FORMAT (must match exactly):
BRANCH: <branch-name>
PROMPT: <planning-prompt>`, taskDescription)
		} else {
			// More explicit prompt on retry
			claudePrompt = fmt.Sprintf(`Extract the SEMANTIC MEANING and respond in exact format.

Task: %s

What is actually being done? (Ignore "please read X", "after doing Y" - get the real goal)

BAD branch: feat/please-read-file-and-review (too literal)
GOOD branch: review/pr-42 (captures actual goal)

Respond EXACTLY as:
BRANCH: prefix/short-name
PROMPT: your planning prompt here

Prefixes: feat/ fix/ refactor/ docs/ test/ review/ chore/`, taskDescription)
		}

		// Call Claude CLI in --print mode to generate branch and prompt (using haiku for speed/cost)
		cmd := exec.Command("claude", "--print", "Generate branch name and prompt", "--model", "haiku", "--dangerously-skip-permissions")
		cmd.Stdin = strings.NewReader(claudePrompt)
		output, err := cmd.Output()
		if err != nil {
			if attempt == maxRetries {
				// AI unavailable, use fallback
				break
			}
			continue
		}

		// Parse output
		outputStr := string(output)
		branchRe := regexp.MustCompile(`BRANCH:\s*(.+)`)
		promptRe := regexp.MustCompile(`PROMPT:\s*(.+)`)

		branchMatch := branchRe.FindStringSubmatch(outputStr)
		promptMatch := promptRe.FindStringSubmatch(outputStr)

		if len(branchMatch) > 1 && len(promptMatch) > 1 {
			branchName := strings.TrimSpace(branchMatch[1])

			// Normalize: convert to lowercase and remove any surrounding quotes
			branchName = strings.ToLower(branchName)
			branchName = strings.Trim(branchName, "\"'`")

			// Enforce max length (40 chars) in case AI ignored the instruction
			if len(branchName) > 40 {
				branchName = branchName[:40]
				branchName = strings.TrimRight(branchName, "-/")
			}

			// Validate the branch name format
			if isValidBranchName(branchName) {
				return branchName, strings.TrimSpace(promptMatch[1]), nil
			}
		}

		// Log retry if not last attempt
		if attempt < maxRetries {
			fmt.Printf("Branch generation attempt %d failed validation, retrying...\n", attempt)
		}
	}

	// Fallback to simple branch name generation
	simpleBranch := generateSimpleBranch(taskDescription)
	planningPrompt := fmt.Sprintf(`Please plan the implementation for the following task:

%s

Break down the implementation into clear steps and identify key components that need to be created or modified.`, taskDescription)
	return simpleBranch, planningPrompt, nil
}

// generateBranchNameOnly generates just a branch name via AI, without a planning prompt
// Includes retry logic and validation to handle cases where the AI returns invalid output
func generateBranchNameOnly(taskDescription string) (string, error) {
	const maxRetries = 3

	for attempt := 1; attempt <= maxRetries; attempt++ {
		var claudePrompt string
		if attempt == 1 {
			claudePrompt = fmt.Sprintf(`Extract the CORE TASK from this description and create a git branch name.

Description: %s

Instructions:
1. Identify what is actually being built/fixed/reviewed (ignore instructions like "read file X" or "switch to branch")
2. Extract key identifiers (PR numbers, ticket IDs, feature names)
3. Create a branch name: prefix/2-4-word-summary

Prefixes: feat/ fix/ refactor/ docs/ test/ review/ chore/

Examples:
- "Please read requirements.txt and implement user login" -> feat/user-login
- "Review PR #42 for the authentication module" -> review/pr-42
- "Fix the bug in issue #123 where payments fail" -> fix/issue-123-payments
- "After reading the spec, add dark mode support" -> feat/dark-mode
- "Refactor the database queries in the user service" -> refactor/user-db-queries

Output ONLY the branch name (lowercase, max 40 chars):`, taskDescription)
		} else {
			// More explicit prompt on retry
			claudePrompt = fmt.Sprintf(`What is the MAIN GOAL of this task? Create a branch name for it.

Task: %s

DO NOT include filler words from the description. Extract the semantic meaning.
BAD: feat/please-read-file-and-do-thing (too literal)
GOOD: feat/thing (captures the actual goal)

Format: prefix/short-name (lowercase, letters/numbers/hyphens only)
Prefixes: feat/ fix/ refactor/ docs/ test/ review/ chore/

Output ONLY the branch name:`, taskDescription)
		}

		// Call Claude CLI in --print mode to generate just the branch name (using haiku for speed/cost)
		cmd := exec.Command("claude", "--print", "Generate branch name", "--model", "haiku", "--dangerously-skip-permissions")
		cmd.Stdin = strings.NewReader(claudePrompt)
		output, err := cmd.Output()
		if err != nil {
			if attempt == maxRetries {
				return "", fmt.Errorf("AI unavailable after %d attempts: %w", maxRetries, err)
			}
			continue
		}

		// Parse output - just take the first line and trim it
		branchName := strings.TrimSpace(strings.Split(string(output), "\n")[0])

		// Skip empty results
		if branchName == "" {
			if attempt == maxRetries {
				return "", fmt.Errorf("empty branch name from AI after %d attempts", maxRetries)
			}
			continue
		}

		// Normalize: convert to lowercase and remove any surrounding quotes
		branchName = strings.ToLower(branchName)
		branchName = strings.Trim(branchName, "\"'`")

		// Enforce max length (40 chars) in case AI ignored the instruction
		if len(branchName) > 40 {
			branchName = branchName[:40]
			branchName = strings.TrimRight(branchName, "-/")
		}

		// Validate the branch name format
		if isValidBranchName(branchName) {
			return branchName, nil
		}

		// If invalid, log and retry
		if attempt < maxRetries {
			fmt.Printf("Branch name attempt %d returned invalid format, retrying...\n", attempt)
		}
	}

	return "", fmt.Errorf("failed to generate valid branch name after %d attempts", maxRetries)
}

// isValidBranchName checks if a string looks like a valid git branch name
// (lowercase with optional prefix like feat/, fix/, etc. containing only alphanumeric and hyphens)
func isValidBranchName(name string) bool {
	if name == "" {
		return false
	}
	// Must match pattern: optional prefix (feat/, fix/, etc.) followed by lowercase alphanumeric and hyphens
	// Valid examples: feat/add-auth, fix/bug-123, refactor/db-pool, add-new-feature
	validPattern := regexp.MustCompile(`^[a-z][a-z0-9-]*(/[a-z0-9][a-z0-9-]*)?$`)
	return validPattern.MatchString(name)
}

// promptUserForBranchName asks the user to provide a branch name when automated generation fails
func promptUserForBranchName(taskDescription string) (string, error) {
	fmt.Println("\n⚠️  Automated branch name generation failed.")
	fmt.Println("Please enter a branch name manually.")
	fmt.Println("(Use lowercase letters, numbers, and hyphens. e.g., feat/add-auth or fix/bug-123)")
	fmt.Printf("Task: %s\n", truncateString(taskDescription, 60))
	fmt.Print("Branch name: ")

	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read input: %w", err)
	}

	branchName := strings.TrimSpace(input)
	if branchName == "" {
		return "", fmt.Errorf("branch name cannot be empty")
	}

	// Sanitize user input - convert to lowercase and replace invalid chars
	branchName = strings.ToLower(branchName)
	branchName = regexp.MustCompile(`[^a-z0-9/-]+`).ReplaceAllString(branchName, "-")
	branchName = strings.Trim(branchName, "-")

	// Enforce max length
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

	// Handle edge case where description has no usable characters
	if desc == "" {
		desc = fmt.Sprintf("task-%d", time.Now().Unix()%100000)
	}

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

		// Mount known_hosts from host to avoid SSH host key verification prompts
		if config.SSH.KnownHostsPath != "" {
			knownHostsPath := expandPath(config.SSH.KnownHostsPath)
			if _, err := os.Stat(knownHostsPath); err == nil {
				args = append(args,
					"-v", fmt.Sprintf("%s:/home/node/.ssh/known_hosts:ro", knownHostsPath),
				)
			}
		}
	}

	// Mount Android SDK if configured (read-only for safety)
	if config.Android.SDKPath != "" {
		sdkPath := expandPath(config.Android.SDKPath)
		if _, err := os.Stat(sdkPath); err == nil {
			args = append(args,
				"-v", fmt.Sprintf("%s:/home/node/Android/Sdk:ro", sdkPath),
				"-e", "ANDROID_HOME=/home/node/Android/Sdk",
			)
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

	// Copy and import SSL certificates for Java
	if err := copySSLCertificates(containerName); err != nil {
		fmt.Printf("Warning: Failed to install SSL certificates: %v\n", err)
	}

	// Setup Android SDK environment (SDK is mounted as volume)
	if err := setupAndroidSDK(containerName); err != nil {
		fmt.Printf("Warning: Failed to setup Android SDK: %v\n", err)
	}

	// Initialize firewall
	fmt.Println("Setting up firewall...")
	if err := initializeFirewall(containerName); err != nil {
		fmt.Printf("Warning: Failed to initialize firewall: %v\n", err)
	}

	return nil
}

// MultiProgress manages a multi-line progress display (like docker pull)
type MultiProgress struct {
	mu          sync.Mutex
	items       map[string]*ProgressItem
	order       []string // Track order of items
	lineCount   int
	initialized bool
	done        chan bool
}

type ProgressItem struct {
	Name      string
	Status    string // "waiting", "copying", "done", "error"
	BytesRead int64
	TotalSize int64
	StartTime time.Time
	EndTime   time.Time
}

var globalProgress *MultiProgress

// InitMultiProgress initializes the global multi-progress display
func InitMultiProgress() *MultiProgress {
	globalProgress = &MultiProgress{
		items: make(map[string]*ProgressItem),
		done:  make(chan bool),
	}
	return globalProgress
}

// GetMultiProgress returns the global progress display
func GetMultiProgress() *MultiProgress {
	return globalProgress
}

// AddItem adds a new item to track
func (mp *MultiProgress) AddItem(name string, totalSize int64) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	mp.items[name] = &ProgressItem{
		Name:      name,
		Status:    "waiting",
		TotalSize: totalSize,
	}
	mp.order = append(mp.order, name)
}

// StartItem marks an item as started
func (mp *MultiProgress) StartItem(name string) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if item, ok := mp.items[name]; ok {
		item.Status = "copying"
		item.StartTime = time.Now()
	}
}

// UpdateItem updates bytes read for an item
func (mp *MultiProgress) UpdateItem(name string, bytesRead int64) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if item, ok := mp.items[name]; ok {
		item.BytesRead = bytesRead
	}
}

// CompleteItem marks an item as done
func (mp *MultiProgress) CompleteItem(name string) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if item, ok := mp.items[name]; ok {
		item.Status = "done"
		item.EndTime = time.Now()
	}
}

// ErrorItem marks an item as failed
func (mp *MultiProgress) ErrorItem(name string, err error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if item, ok := mp.items[name]; ok {
		item.Status = "error"
		item.EndTime = time.Now()
	}
}

// Start begins the progress display loop
func (mp *MultiProgress) Start() {
	go func() {
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-mp.done:
				return
			case <-ticker.C:
				mp.render()
			}
		}
	}()
}

// Stop stops the progress display and renders final state
func (mp *MultiProgress) Stop() {
	close(mp.done)
	mp.renderFinal()
}

func (mp *MultiProgress) render() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if len(mp.items) == 0 {
		return
	}

	// Count active items (skip "waiting" - they haven't started yet)
	activeCount := 0
	for _, name := range mp.order {
		if mp.items[name].Status != "waiting" {
			activeCount++
		}
	}

	if activeCount == 0 {
		return
	}

	// Move cursor up if we've already printed lines
	if mp.initialized && mp.lineCount > 0 {
		fmt.Printf("\033[%dA", mp.lineCount)
	}

	mp.lineCount = activeCount
	mp.initialized = true

	for _, name := range mp.order {
		item := mp.items[name]
		if item.Status != "waiting" {
			mp.renderLine(item)
		}
	}
}

func (mp *MultiProgress) renderFinal() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Move cursor up to overwrite progress lines
	if mp.initialized && mp.lineCount > 0 {
		fmt.Printf("\033[%dA", mp.lineCount)
	}

	for _, name := range mp.order {
		item := mp.items[name]
		if item.Status != "waiting" {
			mp.renderLine(item)
		}
	}
}

func (mp *MultiProgress) renderLine(item *ProgressItem) {
	// Truncate name if too long
	displayName := item.Name
	if len(displayName) > 40 {
		displayName = displayName[:37] + "..."
	}

	// Clear line
	fmt.Print("\033[K")

	switch item.Status {
	case "copying":
		elapsed := time.Since(item.StartTime).Seconds()
		if elapsed < 0.1 {
			elapsed = 0.1
		}
		speed := float64(item.BytesRead) / elapsed / 1024 / 1024

		if item.TotalSize > 0 {
			pct := float64(item.BytesRead) / float64(item.TotalSize) * 100
			if pct > 100 {
				pct = 100
			}
			barWidth := 20
			filled := int(pct / 100 * float64(barWidth))
			bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
			fmt.Printf("%-40s  [%s] %5.1f%% %6.1f MB/s\n", displayName, bar, pct, speed)
		} else {
			fmt.Printf("%-40s  %8s  %6.1f MB/s\n", displayName, formatBytes(item.BytesRead), speed)
		}
	case "done":
		duration := item.EndTime.Sub(item.StartTime).Seconds()
		speed := float64(item.BytesRead) / duration / 1024 / 1024
		fmt.Printf("%-40s  ✓ %s in %.1fs (%.1f MB/s)\n", displayName, formatBytes(item.BytesRead), duration, speed)
	case "error":
		fmt.Printf("%-40s  ✗ Failed\n", displayName)
	}
}

// progressReader wraps an io.Reader to track bytes and report to MultiProgress
type progressReader struct {
	reader        io.Reader
	containerName string
	bytesRead     int64
	mu            sync.Mutex
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.mu.Lock()
	pr.bytesRead += int64(n)
	bytes := pr.bytesRead
	pr.mu.Unlock()

	// Report to global progress if active
	if mp := GetMultiProgress(); mp != nil {
		mp.UpdateItem(pr.containerName, bytes)
	}
	return n, err
}

func (pr *progressReader) getBytesRead() int64 {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	return pr.bytesRead
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// readMaestroIgnore reads exclusion patterns from .maestroignore file
func readMaestroIgnore(dir string) []string {
	ignorePath := filepath.Join(dir, ".maestroignore")
	file, err := os.Open(ignorePath)
	if err != nil {
		return nil // No .maestroignore file, that's fine
	}
	defer file.Close()

	var patterns []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		patterns = append(patterns, line)
	}
	return patterns
}

func copyProjectToContainer(containerName string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	// Determine compression setting (default: true for backward compatibility)
	useCompression := config.Sync.Compress == nil || *config.Sync.Compress

	// Check if we're in batch mode (MultiProgress active)
	mp := GetMultiProgress()
	isBatchMode := mp != nil

	// Signal start to MultiProgress
	if isBatchMode {
		mp.StartItem(containerName)
	} else {
		fmt.Printf("Copying source code to %s...\n", containerName)
	}

	startTime := time.Now()

	// Build exclude arguments (defaults + .maestroignore)
	excludeArgs := []string{"--exclude=node_modules", "--exclude=.git"}
	for _, pattern := range readMaestroIgnore(cwd) {
		excludeArgs = append(excludeArgs, "--exclude="+pattern)
	}

	// Create tar of current directory (excluding .git which is copied separately)
	var tarCmd *exec.Cmd
	var dockerCmd *exec.Cmd
	if useCompression {
		// Use gzip compression (slower for large projects but smaller transfer)
		tarArgs := append([]string{"-czf", "-"}, excludeArgs...)
		tarArgs = append(tarArgs, ".")
		tarCmd = exec.Command("tar", tarArgs...)
		dockerCmd = exec.Command("docker", "exec", "-i", containerName, "tar", "-xzf", "-", "-C", "/workspace")
	} else {
		// No compression (faster for large projects on local Docker)
		tarArgs := append([]string{"-cf", "-"}, excludeArgs...)
		tarArgs = append(tarArgs, ".")
		tarCmd = exec.Command("tar", tarArgs...)
		dockerCmd = exec.Command("docker", "exec", "-i", containerName, "tar", "-xf", "-", "-C", "/workspace")
	}
	tarCmd.Dir = cwd

	// Connect pipes with progress tracking
	pipe, err := tarCmd.StdoutPipe()
	if err != nil {
		if isBatchMode {
			mp.ErrorItem(containerName, err)
		}
		return err
	}

	// Create progress reader
	pr := &progressReader{
		reader:        pipe,
		containerName: containerName,
	}
	dockerCmd.Stdin = pr

	// Start both commands
	if err := tarCmd.Start(); err != nil {
		if isBatchMode {
			mp.ErrorItem(containerName, err)
		}
		return err
	}
	if err := dockerCmd.Start(); err != nil {
		if isBatchMode {
			mp.ErrorItem(containerName, err)
		}
		return err
	}

	// Wait for completion
	tarErr := tarCmd.Wait()
	dockerErr := dockerCmd.Wait()

	bytesRead := pr.getBytesRead()
	duration := time.Since(startTime)

	if tarErr != nil {
		if isBatchMode {
			mp.ErrorItem(containerName, tarErr)
		}
		return tarErr
	}
	if dockerErr != nil {
		if isBatchMode {
			mp.ErrorItem(containerName, dockerErr)
		}
		return dockerErr
	}

	// Update final bytes and mark complete
	if isBatchMode {
		mp.UpdateItem(containerName, bytesRead)
		mp.CompleteItem(containerName)
	} else {
		speed := float64(bytesRead) / duration.Seconds() / 1024 / 1024
		fmt.Printf("  Copied %s in %.1fs (%.1f MB/s)\n", formatBytes(bytesRead), duration.Seconds(), speed)
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

	// Write AWS config flag if Bedrock or AWS is enabled
	// This tells the firewall script to add AWS domain rules
	if config.AWS.Enabled || config.Bedrock.Enabled {
		writeAWSConfigCmd := exec.Command("docker", "exec", "-u", "root", containerName, "sh", "-c",
			"echo 'enabled' > /etc/aws-enabled.txt")
		if err := writeAWSConfigCmd.Run(); err != nil {
			fmt.Printf("Warning: Failed to write AWS config: %v\n", err)
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

func setupAndroidSDK(containerName string) error {
	sdkPath := expandPath(config.Android.SDKPath)
	if sdkPath == "" {
		return nil // No Android SDK configured
	}

	// Check if SDK exists
	if _, err := os.Stat(sdkPath); err != nil {
		return nil // SDK not found
	}

	fmt.Println("Setting up Android SDK...")

	// Set ANDROID_HOME environment variable in .zshrc
	envCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		`echo 'export ANDROID_HOME=/home/node/Android/Sdk' >> /home/node/.zshrc && echo 'export PATH=$PATH:$ANDROID_HOME/platform-tools:$ANDROID_HOME/cmdline-tools/latest/bin' >> /home/node/.zshrc`)
	if err := envCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to set ANDROID_HOME: %v\n", err)
	}

	// Update local.properties in workspace if it exists
	updateLocalPropertiesCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		`if [ -f /workspace/local.properties ]; then
			sed -i 's|sdk.dir=.*|sdk.dir=/home/node/Android/Sdk|' /workspace/local.properties
			echo "  ✓ Updated local.properties"
		fi`)
	if err := updateLocalPropertiesCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to update local.properties: %v\n", err)
	}

	fmt.Println("  ✓ Android SDK mounted at /home/node/Android/Sdk")

	return nil
}

func copySSLCertificates(containerName string) error {
	certsPath := expandPath(config.SSL.CertificatesPath)
	if certsPath == "" {
		return nil // No certificates configured
	}

	// Check if certificates directory exists
	if _, err := os.Stat(certsPath); err != nil {
		return nil // No certificates to copy
	}

	// List certificate files
	entries, err := os.ReadDir(certsPath)
	if err != nil {
		return fmt.Errorf("failed to read certificates directory: %w", err)
	}

	var certFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && (filepath.Ext(entry.Name()) == ".crt" || filepath.Ext(entry.Name()) == ".pem") {
			certFiles = append(certFiles, entry.Name())
		}
	}

	if len(certFiles) == 0 {
		return nil // No certificate files found
	}

	fmt.Printf("Installing %d SSL certificate(s) for Java...\n", len(certFiles))

	// Create temporary directory in container for certificates
	mkdirCmd := exec.Command("docker", "exec", "-u", "root", containerName, "mkdir", "-p", "/tmp/host-certs")
	if err := mkdirCmd.Run(); err != nil {
		return fmt.Errorf("failed to create temp certs directory: %w", err)
	}

	// Copy each certificate and import into Java keystore
	for _, certFile := range certFiles {
		certPath := filepath.Join(certsPath, certFile)

		// Copy certificate to container
		copyCmd := exec.Command("docker", "cp", certPath, fmt.Sprintf("%s:/tmp/host-certs/%s", containerName, certFile))
		if err := copyCmd.Run(); err != nil {
			fmt.Printf("  ⚠  Failed to copy %s: %v\n", certFile, err)
			continue
		}

		// Generate alias from filename (remove extension, replace special chars)
		alias := certFile[:len(certFile)-len(filepath.Ext(certFile))]
		alias = regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(alias, "_")

		// Import into Java keystore (using keytool)
		// The default cacerts password is 'changeit'
		importCmd := exec.Command("docker", "exec", "-u", "root", containerName, "keytool",
			"-importcert",
			"-noprompt",
			"-trustcacerts",
			"-alias", alias,
			"-file", fmt.Sprintf("/tmp/host-certs/%s", certFile),
			"-keystore", "/usr/local/jdk-17.0.2/lib/security/cacerts",
			"-storepass", "changeit",
		)
		output, err := importCmd.CombinedOutput()
		if err != nil {
			// Check if it's just a duplicate alias error (certificate already exists)
			if !strings.Contains(string(output), "already exists") {
				fmt.Printf("  ⚠  Failed to import %s: %v\n", certFile, err)
			}
			continue
		}
		fmt.Printf("  ✓ %s\n", certFile)
	}

	// Cleanup temp directory
	cleanupCmd := exec.Command("docker", "exec", "-u", "root", containerName, "rm", "-rf", "/tmp/host-certs")
	cleanupCmd.Run() // Ignore errors on cleanup

	// Change keystore password from default 'changeit' to a random password
	// This prevents the default password from being used to tamper with the keystore
	newPassword := generateRandomPassword(32)
	changePassCmd := exec.Command("docker", "exec", "-u", "root", containerName, "keytool",
		"-storepasswd",
		"-keystore", "/usr/local/jdk-17.0.2/lib/security/cacerts",
		"-storepass", "changeit",
		"-new", newPassword,
	)
	if err := changePassCmd.Run(); err != nil {
		fmt.Printf("  ⚠  Failed to change keystore password: %v\n", err)
	} else {
		fmt.Println("  ✓ Keystore password randomized")
	}

	return nil
}

// generateRandomPassword generates a cryptographically random password
func generateRandomPassword(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	return string(b)
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
		// User provided custom branch name - sanitize it
		branchName = strings.ToLower(branchNameOverride)
		branchName = regexp.MustCompile(`[^a-z0-9/-]+`).ReplaceAllString(branchName, "-")
		branchName = strings.Trim(branchName, "-")
		planningPrompt = taskDescription // Use description as prompt
	} else {
		// Generate branch name and planning prompt using Claude
		branchName, planningPrompt, err = generateBranchAndPrompt(taskDescription, exact)
		if err != nil {
			return fmt.Errorf("failed to generate branch name: %w", err)
		}
	}

	// Validate the branch name and prompt user if invalid
	if !isValidBranchName(branchName) {
		fmt.Printf("Generated branch name '%s' is invalid.\n", branchName)
		branchName, err = promptUserForBranchName(taskDescription)
		if err != nil {
			return fmt.Errorf("failed to get branch name: %w", err)
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

