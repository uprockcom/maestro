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
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/uprockcom/maestro/pkg/paths"
)

// OperationType defines Docker operations that can be performed on containers
type OperationType string

const (
	OperationStop          OperationType = "stop"
	OperationRestart       OperationType = "restart"
	OperationDelete        OperationType = "delete"
	OperationRefreshTokens OperationType = "refresh-tokens"
)

// StopContainer stops a running container
func StopContainer(containerName string) error {
	cmd := exec.Command("docker", "stop", containerName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	return nil
}

// StartContainer starts a stopped container
func StartContainer(containerName string) error {
	cmd := exec.Command("docker", "start", containerName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	return nil
}

// RestartContainer performs a full container restart (docker stop + start)
func RestartContainer(containerName string) error {
	// Stop container
	stopCmd := exec.Command("docker", "stop", containerName)
	if err := stopCmd.Run(); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	// Start container
	startCmd := exec.Command("docker", "start", containerName)
	if err := startCmd.Run(); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	// Wait for container to be ready
	time.Sleep(2 * time.Second)

	return nil
}

// DeleteContainer removes a container and its volumes
func DeleteContainer(containerName string) error {
	// Remove container with volumes
	rmCmd := exec.Command("docker", "rm", "-f", "-v", containerName)
	if err := rmCmd.Run(); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}

	// Remove associated named volumes
	volumes := []string{
		fmt.Sprintf("%s-npm", containerName),
		fmt.Sprintf("%s-uv", containerName),
		fmt.Sprintf("%s-history", containerName),
	}

	for _, volume := range volumes {
		volCmd := exec.Command("docker", "volume", "rm", volume)
		volCmd.Run() // Ignore errors - volume might not exist
	}

	return nil
}

// TokenSource represents where a token was found
type TokenSource struct {
	Path       string    // File path (for host or temp file from container)
	ExpiresAt  time.Time // Token expiration time
	Source     string    // "host" or container name
	IsTempFile bool      // If true, path is a temp file that should be cleaned up
}

// FindFreshestToken searches host and all containers for the freshest unexpired token.
// Returns nil if no valid token is found anywhere.
func FindFreshestToken(containerPrefix string) (*TokenSource, error) {
	var freshest *TokenSource
	var tempFiles []string // Track temp files for cleanup

	defer func() {
		// Clean up temp files (except the freshest one if it's from a container)
		for _, tf := range tempFiles {
			if freshest == nil || tf != freshest.Path {
				os.Remove(tf)
			}
		}
	}()

	// Check host credentials
	hostCredPath := filepath.Join(paths.AuthDir(), ".credentials.json")
	if hostCreds, err := ReadCredentials(hostCredPath); err == nil {
		expiresAt := time.UnixMilli(hostCreds.ClaudeAiOauth.ExpiresAt)
		if !IsTokenExpired(hostCreds) {
			freshest = &TokenSource{
				Path:       hostCredPath,
				ExpiresAt:  expiresAt,
				Source:     "host",
				IsTempFile: false,
			}
		}
	}

	// Get all running containers to check their tokens
	containers, err := GetRunningContainers(containerPrefix)
	if err != nil {
		// Continue even if we can't list containers - host token may be available
		if freshest != nil {
			return freshest, nil
		}
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	// Check each container's credentials
	for _, c := range containers {
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("maestro-creds-%s.json", c.Name))
		tempFiles = append(tempFiles, tmpFile)

		copyCmd := exec.Command("docker", "cp",
			fmt.Sprintf("%s:/home/node/.claude/.credentials.json", c.Name),
			tmpFile)
		if err := copyCmd.Run(); err != nil {
			continue
		}

		creds, err := ReadCredentials(tmpFile)
		if err != nil {
			continue
		}

		if IsTokenExpired(creds) {
			continue
		}

		expiresAt := time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
		if freshest == nil || expiresAt.After(freshest.ExpiresAt) {
			// Clean up old freshest temp file if applicable
			if freshest != nil && freshest.IsTempFile {
				os.Remove(freshest.Path)
			}
			freshest = &TokenSource{
				Path:       tmpFile,
				ExpiresAt:  expiresAt,
				Source:     c.Name,
				IsTempFile: true,
			}
		}
	}

	if freshest == nil {
		return nil, fmt.Errorf("no valid credentials found (all expired or missing)")
	}

	return freshest, nil
}

// EnsureFreshToken checks if a container has a valid token, and if not,
// syncs the freshest available token from host or other containers.
// Returns nil if token is already fresh, or after successful sync.
// Returns error if no valid token is available anywhere.
func EnsureFreshToken(containerName, containerPrefix string) error {
	// First check if target container already has a valid token
	tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("maestro-check-%s.json", containerName))
	defer os.Remove(tmpFile)

	copyCmd := exec.Command("docker", "cp",
		fmt.Sprintf("%s:/home/node/.claude/.credentials.json", containerName),
		tmpFile)

	targetHasValidToken := false
	var targetExpiresAt time.Time

	if err := copyCmd.Run(); err == nil {
		if creds, err := ReadCredentials(tmpFile); err == nil {
			if !IsTokenExpired(creds) {
				targetHasValidToken = true
				targetExpiresAt = time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
			}
		}
	}

	// Find the freshest token available
	freshest, err := FindFreshestToken(containerPrefix)
	if err != nil {
		if targetHasValidToken {
			// Container has a valid token, even if we couldn't find others
			return nil
		}
		return err
	}
	defer func() {
		if freshest.IsTempFile {
			os.Remove(freshest.Path)
		}
	}()

	// If target already has a valid token that's as fresh or fresher, we're done
	if targetHasValidToken && !freshest.ExpiresAt.After(targetExpiresAt) {
		return nil
	}

	// Target either has no valid token or has an older one - sync the freshest
	destPath := fmt.Sprintf("%s:/home/node/.claude/.credentials.json", containerName)
	syncCmd := exec.Command("docker", "cp", freshest.Path, destPath)
	if err := syncCmd.Run(); err != nil {
		return fmt.Errorf("failed to sync credentials to container: %w", err)
	}

	// Fix ownership
	chownCmd := exec.Command("docker", "exec", "-u", "root", containerName,
		"chown", "node:node", "/home/node/.claude/.credentials.json")
	if err := chownCmd.Run(); err != nil {
		return fmt.Errorf("failed to fix credentials ownership: %w", err)
	}

	return nil
}

// RefreshTokens finds the freshest token and syncs it to a specific container
func RefreshTokens(containerName string) error {
	// Find freshest token by checking host and all containers
	hostCredPath := filepath.Join(paths.AuthDir(), ".credentials.json")

	var freshestPath string
	var freshestTime time.Time

	// Check host credentials
	if hostCreds, err := ReadCredentials(hostCredPath); err == nil {
		freshestPath = hostCredPath
		freshestTime = time.UnixMilli(hostCreds.ClaudeAiOauth.ExpiresAt)
	}

	// Get all running containers to check their tokens
	containers, err := GetRunningContainers("mcl-")
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Check each container's credentials
	for _, c := range containers {
		tmpFile := filepath.Join(os.TempDir(), fmt.Sprintf("maestro-creds-%s.json", c.Name))
		copyCmd := exec.Command("docker", "cp",
			fmt.Sprintf("%s:/home/node/.claude/.credentials.json", c.Name),
			tmpFile)
		if err := copyCmd.Run(); err != nil {
			continue
		}
		defer os.Remove(tmpFile)

		if creds, err := ReadCredentials(tmpFile); err == nil {
			expiresAt := time.UnixMilli(creds.ClaudeAiOauth.ExpiresAt)
			if expiresAt.After(freshestTime) {
				freshestPath = tmpFile
				freshestTime = expiresAt
			}
		}
	}

	if freshestPath == "" {
		return fmt.Errorf("no valid credentials found")
	}

	// Check if token is expired
	freshCreds, err := ReadCredentials(freshestPath)
	if err != nil {
		return fmt.Errorf("failed to read freshest credentials: %w", err)
	}

	if IsTokenExpired(freshCreds) {
		return fmt.Errorf("all tokens are expired")
	}

	// Copy freshest credentials to target container
	copyCmd := exec.Command("docker", "cp", freshestPath,
		fmt.Sprintf("%s:/home/node/.claude/.credentials.json", containerName))
	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy credentials to container: %w", err)
	}

	// Fix ownership
	chownCmd := exec.Command("docker", "exec", "-u", "root", containerName,
		"chown", "node:node", "/home/node/.claude/.credentials.json")
	if err := chownCmd.Run(); err != nil {
		return fmt.Errorf("failed to fix credentials ownership: %w", err)
	}

	return nil
}

// AddDomainToAllContainers adds a domain to all running containers' firewall
func AddDomainToAllContainers(domain string) error {
	// Get all running containers
	cmd := exec.Command("docker", "ps", "--filter", "status=running", "--format", "{{.Names}}")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to list running containers: %w", err)
	}

	containerNames := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(containerNames) == 0 || (len(containerNames) == 1 && containerNames[0] == "") {
		return nil // No running containers
	}

	// Add domain to each running container
	for _, containerName := range containerNames {
		if err := AddDomainToContainer(containerName, domain); err != nil {
			// Log error but continue with other containers
			fmt.Fprintf(os.Stderr, "Warning: failed to add domain to %s: %v\n", containerName, err)
		}
	}

	return nil
}

// AddDomainToContainer adds a domain to a specific container's firewall
func AddDomainToContainer(containerName, domain string) error {
	dnsmasqConf := "/tmp/dnsmasq-firewall.conf"

	// Check if domain already in config
	checkConfCmd := exec.Command("docker", "exec", containerName, "grep", "-q", fmt.Sprintf("ipset=/%s/", domain), dnsmasqConf)
	if checkConfCmd.Run() == nil {
		return nil // Already configured
	}

	// Append domain to dnsmasq config
	appendCmd := exec.Command("docker", "exec", "-u", "root", containerName, "sh", "-c",
		fmt.Sprintf("echo 'ipset=/%s/allowed-domains' >> %s && echo 'server=/%s/8.8.8.8' >> %s",
			domain, dnsmasqConf, domain, dnsmasqConf))
	if err := appendCmd.Run(); err != nil {
		return fmt.Errorf("failed to update dnsmasq config: %w", err)
	}

	// Restart dnsmasq
	restartCmd := exec.Command("docker", "exec", "-u", "root", containerName, "sh", "-c",
		"pkill -9 dnsmasq 2>/dev/null || true; sleep 0.2; dnsmasq --conf-file=/tmp/dnsmasq-firewall.conf")
	if err := restartCmd.Run(); err != nil {
		return fmt.Errorf("failed to restart dnsmasq: %w", err)
	}

	// Perform initial DNS resolution
	resolveCmd := exec.Command("docker", "exec", containerName, "sh", "-c",
		fmt.Sprintf("dig +short %s | head -5", domain))
	_, _ = resolveCmd.Output() // Ignore errors from resolution

	return nil
}
