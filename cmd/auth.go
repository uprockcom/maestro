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
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Authenticate Claude Code and GitHub CLI for MCL containers",
	Long: `Authenticate Claude Code and optionally GitHub CLI for MCL containers.

This command will:
1. Start a temporary container for Claude Code authentication
   - Follow the OAuth flow in your browser
   - Complete initial setup (theme, etc.)
   - Exit with Ctrl+D when done

2. Optionally set up GitHub CLI authentication
   - Authenticate with GitHub for features like PR reviews
   - Credentials stored in ~/.maestro/gh/

3. By default, sync new credentials to all running containers
   - Use --no-sync to skip this step

All authentication data is stored in ~/.maestro/ and shared (read-only) with containers.`,
	RunE: runAuth,
}

var noSync bool

func init() {
	rootCmd.AddCommand(authCmd)
	authCmd.Flags().BoolVar(&noSync, "no-sync", false, "Skip syncing credentials to running containers")
}

// runBedrockAuth handles authentication for AWS Bedrock users
func runBedrockAuth() error {
	fmt.Println("Bedrock mode enabled - using AWS authentication")

	// Copy Claude config from ~/.claude to maestro's auth directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}

	sourceClaudeDir := filepath.Join(homeDir, ".claude")
	destAuthPath := expandPath(config.Claude.AuthPath)

	// Ensure destination directory exists
	if err := os.MkdirAll(destAuthPath, 0755); err != nil {
		return fmt.Errorf("failed to create auth directory: %w", err)
	}

	// Check if source ~/.claude exists
	if _, err := os.Stat(sourceClaudeDir); os.IsNotExist(err) {
		fmt.Printf("Warning: ~/.claude directory not found\n")
		fmt.Println("You may need to run 'claude' once on the host to create initial config")
	} else {
		// Copy .credentials.json if exists
		srcCreds := filepath.Join(sourceClaudeDir, ".credentials.json")
		if _, err := os.Stat(srcCreds); err == nil {
			destCreds := filepath.Join(destAuthPath, ".credentials.json")
			if err := copyFile(srcCreds, destCreds); err != nil {
				fmt.Printf("Warning: Failed to copy credentials: %v\n", err)
			} else {
				fmt.Printf("✓ Copied credentials from %s\n", srcCreds)
			}
		}

		// Copy settings.json if exists (Claude Code settings)
		srcSettings := filepath.Join(sourceClaudeDir, "settings.json")
		if _, err := os.Stat(srcSettings); err == nil {
			destSettings := filepath.Join(destAuthPath, "settings.json")
			if err := copyFile(srcSettings, destSettings); err != nil {
				fmt.Printf("Warning: Failed to copy settings: %v\n", err)
			} else {
				fmt.Printf("✓ Copied settings from %s\n", srcSettings)
			}
		}
	}

	// Copy .claude.json from home directory if exists
	srcClaudeJson := filepath.Join(homeDir, ".claude.json")
	if _, err := os.Stat(srcClaudeJson); err == nil {
		destClaudeJson := filepath.Join(destAuthPath, ".claude.json")
		if err := copyFile(srcClaudeJson, destClaudeJson); err != nil {
			fmt.Printf("Warning: Failed to copy .claude.json: %v\n", err)
		} else {
			fmt.Printf("✓ Copied config from %s\n", srcClaudeJson)
		}
	}

	// Run AWS SSO login if profile is configured
	if config.AWS.Profile != "" {
		fmt.Printf("\nRunning AWS SSO login for profile: %s\n", config.AWS.Profile)
		fmt.Println("This will open a browser window for authentication...")

		ssoCmd := exec.Command("aws", "sso", "login", "--profile", config.AWS.Profile)
		ssoCmd.Stdin = os.Stdin
		ssoCmd.Stdout = os.Stdout
		ssoCmd.Stderr = os.Stderr

		if err := ssoCmd.Run(); err != nil {
			return fmt.Errorf("AWS SSO login failed: %w", err)
		}
		fmt.Println("✓ AWS SSO login successful")
	}

	fmt.Println("\n✅ Bedrock authentication setup complete!")
	fmt.Printf("AWS Profile: %s\n", config.AWS.Profile)
	fmt.Printf("AWS Region: %s\n", config.AWS.Region)
	fmt.Printf("Bedrock Model: %s\n", config.Bedrock.Model)
	fmt.Println("\nYou can now create containers with: maestro new <description>")

	return nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, input, 0644)
}

func runAuth(cmd *cobra.Command, cmdArgs []string) error {
	// If Bedrock is enabled, use different auth flow
	if config.Bedrock.Enabled {
		return runBedrockAuth()
	}

	// Ensure MCL Claude directory exists
	authPath := expandPath(config.Claude.AuthPath)
	if err := os.MkdirAll(authPath, 0755); err != nil {
		return fmt.Errorf("failed to create MCL Claude directory: %w", err)
	}

	fmt.Printf("MCL Claude directory: %s\n", authPath)

	// Nuke existing auth directory contents
	fmt.Println("Clearing existing authentication data...")
	entries, err := os.ReadDir(authPath)
	if err != nil {
		return fmt.Errorf("failed to read MCL Claude directory: %w", err)
	}
	for _, entry := range entries {
		entryPath := filepath.Join(authPath, entry.Name())
		if err := os.RemoveAll(entryPath); err != nil {
			return fmt.Errorf("failed to remove %s: %w", entryPath, err)
		}
	}
	fmt.Println("✓ Cleared existing authentication data")

	// Ensure Docker image exists
	if err := ensureDockerImage(); err != nil {
		return fmt.Errorf("failed to ensure Docker image: %w", err)
	}

	authContainerName := config.Containers.Prefix + "auth"

	// Check if auth container already exists
	checkCmd := exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("name=%s", authContainerName), "--format", "{{.Names}}")
	output, _ := checkCmd.Output()
	if len(output) > 0 {
		// Remove existing auth container
		fmt.Println("Removing existing auth container...")
		exec.Command("docker", "rm", "-f", authContainerName).Run()
	}

	fmt.Println("\nStarting authentication container...")
	fmt.Println("This container has READ-WRITE access to:", authPath)
	fmt.Println("\nPlease authenticate and configure Claude Code:")
	fmt.Println("1. Complete the initial setup (choose theme, etc.)")
	fmt.Println("2. Follow the OAuth login flow in your browser")
	fmt.Println("3. Once fully authenticated and configured, press Ctrl+D or type 'exit'")
	fmt.Println("4. Your credentials AND configuration will be saved for all future MCL containers")
	fmt.Println("\n========================================================================")

	// Start temporary auth container with RW mount (no --rm so we can copy files after exit)
	args := []string{
		"run", "-it",
		"--name", authContainerName,
		"-v", fmt.Sprintf("%s:/home/node/.claude", authPath),
		"-w", "/workspace",
	}

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

	args = append(args,
		config.Containers.Image,
		"claude", "--dangerously-skip-permissions",
	)

	authCmd := exec.Command("docker", args...)
	authCmd.Stdin = os.Stdin
	authCmd.Stdout = os.Stdout
	authCmd.Stderr = os.Stderr

	if err := authCmd.Run(); err != nil {
		return fmt.Errorf("authentication failed: %w", err)
	}

	fmt.Println("\nCopying configuration from auth container...")

	// Copy .claude.json from container's home directory to host
	// This file contains onboarding state, permissions, and account info
	copyConfigCmd := exec.Command("docker", "cp",
		fmt.Sprintf("%s:/home/node/.claude.json", authContainerName),
		filepath.Join(authPath, ".claude.json"))
	if err := copyConfigCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to copy .claude.json from container: %v\n", err)
	}

	// Clean up auth container now that we've copied the files
	fmt.Println("Cleaning up auth container...")
	exec.Command("docker", "rm", "-f", authContainerName).Run()

	// Check if both credentials and config were created
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

	if credExists && configExists {
		fmt.Println("\n✅ Authentication and configuration successful!")
		fmt.Printf("Credentials saved to: %s\n", credPath)
		fmt.Printf("Configuration saved to: %s\n", configPath)
		fmt.Println("\nYou can now create maestro containers with: maestro new <description>")
	} else {
		fmt.Println("\n⚠️  Warning: Setup incomplete.")
		if !credExists {
			fmt.Println("  - Missing .credentials.json (authentication)")
		}
		if !configExists {
			fmt.Println("  - Missing .claude.json (configuration)")
		}
		fmt.Println("\nAuthentication may not have completed successfully.")
		fmt.Println("You can try running 'maestro auth' again.")
		return fmt.Errorf("authentication incomplete")
	}

	// Sync credentials to running containers unless --no-sync is set
	if !noSync {
		if err := syncCredentialsToContainers(); err != nil {
			fmt.Printf("\n⚠️  Warning: Failed to sync credentials to containers: %v\n", err)
			fmt.Println("You can manually restart containers or try syncing again later.")
		}
	}

	// Ask user if they want to set up GitHub CLI
	fmt.Println("\n========================================================================")
	fmt.Print("\nWould you like to set up GitHub CLI (gh) authentication? (y/N): ")
	var response string
	fmt.Scanln(&response)

	if response == "y" || response == "Y" || response == "yes" || response == "Yes" {
		if err := setupGitHubAuth(); err != nil {
			fmt.Printf("\n⚠️  GitHub CLI setup failed: %v\n", err)
			fmt.Println("You can skip this and run 'gh auth login' manually later.")
		}
	} else {
		fmt.Println("\nSkipping GitHub CLI setup.")
		fmt.Println("You can set it up later by running 'gh auth login' in a container,")
		fmt.Println("or enable github.enabled in your config file and authenticate on the host.")
	}

	return nil
}

func setupGitHubAuth() error {
	// Ensure MCL gh directory exists
	mclGhPath := expandPath(config.GitHub.ConfigPath)
	if err := os.MkdirAll(mclGhPath, 0755); err != nil {
		return fmt.Errorf("failed to create MCL gh directory: %w", err)
	}

	fmt.Printf("\nGitHub CLI directory: %s\n", mclGhPath)

	// Clear existing GitHub auth data
	fmt.Println("Clearing existing GitHub authentication data...")
	entries, err := os.ReadDir(mclGhPath)
	if err == nil {
		for _, entry := range entries {
			entryPath := filepath.Join(mclGhPath, entry.Name())
			os.RemoveAll(entryPath)
		}
	}
	fmt.Println("✓ Cleared existing GitHub authentication data")

	ghAuthContainerName := config.Containers.Prefix + "gh-auth"

	// Check if gh auth container already exists
	checkCmd := exec.Command("docker", "ps", "-a", "--filter", fmt.Sprintf("name=%s", ghAuthContainerName), "--format", "{{.Names}}")
	output, _ := checkCmd.Output()
	if len(output) > 0 {
		// Remove existing gh auth container
		fmt.Println("Removing existing gh auth container...")
		exec.Command("docker", "rm", "-f", ghAuthContainerName).Run()
	}

	fmt.Println("\nStarting GitHub CLI authentication container...")
	fmt.Println("This container has READ-WRITE access to:", mclGhPath)
	fmt.Println("\nPlease authenticate with GitHub:")
	fmt.Println("1. The 'gh auth login' command will start automatically")
	fmt.Println("2. Follow the prompts to authenticate (browser or token)")
	fmt.Println("3. Once authenticated, the container will exit automatically")
	fmt.Println("\n========================================================================")

	// Start temporary gh auth container with RW mount
	args := []string{
		"run", "-it",
		"--name", ghAuthContainerName,
		"-v", fmt.Sprintf("%s:/home/node/.config/gh", mclGhPath),
		"-w", "/workspace",
	}

	// Mount host SSL certificates for corporate proxies (Zscaler, etc.)
	if _, err := os.Stat("/etc/ssl/certs/ca-certificates.crt"); err == nil {
		args = append(args,
			"-v", "/etc/ssl/certs:/etc/ssl/certs:ro",
			"-e", "SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt",
			"-e", "CURL_CA_BUNDLE=/etc/ssl/certs/ca-certificates.crt",
		)
	}

	args = append(args,
		config.Containers.Image,
		"gh", "auth", "login",
	)

	ghAuthCmd := exec.Command("docker", args...)
	ghAuthCmd.Stdin = os.Stdin
	ghAuthCmd.Stdout = os.Stdout
	ghAuthCmd.Stderr = os.Stderr

	if err := ghAuthCmd.Run(); err != nil {
		// Clean up container even on error
		exec.Command("docker", "rm", "-f", ghAuthContainerName).Run()
		return fmt.Errorf("GitHub authentication failed: %w", err)
	}

	// Clean up gh auth container
	fmt.Println("\nCleaning up GitHub auth container...")
	exec.Command("docker", "rm", "-f", ghAuthContainerName).Run()

	// Check if authentication was successful
	hostsPath := filepath.Join(mclGhPath, "hosts.yml")
	if _, err := os.Stat(hostsPath); err == nil {
		fmt.Println("\n✅ GitHub CLI authentication successful!")
		fmt.Printf("Configuration saved to: %s\n", mclGhPath)
		fmt.Println("\nGitHub CLI will be available in all MCL containers when github.enabled is true.")
	} else {
		return fmt.Errorf("hosts.yml not found - authentication may not have completed")
	}

	return nil
}

func syncCredentialsToContainers() error {
	fmt.Println("\n========================================================================")
	fmt.Println("Syncing credentials to running containers...")

	// Get all running containers
	dockerCmd := exec.Command("docker", "ps", "--format", "{{.Names}}\t{{.State}}")
	output, err := dockerCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Filter for running MCL containers
	var runningContainers []string
	for _, line := range strings.Split(string(output), "\n") {
		if line == "" {
			continue
		}

		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}

		name := parts[0]
		state := parts[1]

		// Skip auth containers and non-MCL containers
		if !strings.HasPrefix(name, config.Containers.Prefix) {
			continue
		}
		if strings.Contains(name, "-auth") {
			continue
		}
		if state != "running" {
			continue
		}

		runningContainers = append(runningContainers, name)
	}

	if len(runningContainers) == 0 {
		fmt.Println("No running containers found. Credentials will be available when new containers are created.")
		return nil
	}

	fmt.Printf("Found %d running container(s) to update\n", len(runningContainers))

	// Get the credentials path
	authPath := expandPath(config.Claude.AuthPath)
	credPath := filepath.Join(authPath, ".credentials.json")

	// Check if credentials exist
	if _, err := os.Stat(credPath); err != nil {
		return fmt.Errorf("credentials file not found: %s", credPath)
	}

	// Sync credentials to each container
	successCount := 0
	for _, containerName := range runningContainers {
		fmt.Printf("  Updating %s... ", containerName)

		// Copy credentials to container
		copyCmd := exec.Command("docker", "cp",
			credPath,
			fmt.Sprintf("%s:/home/node/.claude/.credentials.json", containerName))
		if err := copyCmd.Run(); err != nil {
			fmt.Printf("FAILED: %v\n", err)
			continue
		}

		// Fix ownership (run as root)
		chownCmd := exec.Command("docker", "exec", "-u", "root", containerName,
			"chown", "node:node", "/home/node/.claude/.credentials.json")
		if err := chownCmd.Run(); err != nil {
			fmt.Printf("WARNING: ownership fix failed: %v\n", err)
		}

		fmt.Println("✓")
		successCount++
	}

	if successCount == len(runningContainers) {
		fmt.Printf("\n✅ Successfully synced credentials to %d container(s)\n", successCount)
	} else {
		fmt.Printf("\n⚠️  Synced credentials to %d/%d container(s)\n", successCount, len(runningContainers))
	}

	return nil
}
