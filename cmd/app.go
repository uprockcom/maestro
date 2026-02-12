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
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/uprockcom/maestro/pkg/container"
)

var (
	appSyncNow bool
	appCleanup bool
	appAll     bool
	appQuiet   bool
)

var appCmd = &cobra.Command{
	Use:   "app",
	Short: "Manage custom binaries synced to containers",
	Long: `Manage custom binaries that are automatically copied to all containers.

Apps are configured in ~/.maestro/config.yml and copied to /usr/local/bin in each container.
Use 'app update' to sync changes to running containers.`,
}

var appListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured apps",
	RunE:  runAppList,
}

var appAddCmd = &cobra.Command{
	Use:   "add <name> <source-path>",
	Short: "Add an app to configuration",
	Long: `Add an app to the configuration file.

The app will be copied to /usr/local/bin/<name> in all new containers.
Use --sync to immediately update all running containers.`,
	Args: cobra.ExactArgs(2),
	RunE: runAppAdd,
}

var appUpdateCmd = &cobra.Command{
	Use:   "update [name]",
	Short: "Update apps in running containers",
	Long: `Update apps in all running containers.

Specify an app name to update just that app, or use --all to update all apps.
Uses checksums to skip copying if the file hasn't changed.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runAppUpdate,
}

var appRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove an app from configuration",
	Long: `Remove an app from the configuration file.

Use --cleanup to also remove it from all running containers.`,
	Args: cobra.ExactArgs(1),
	RunE: runAppRemove,
}

func init() {
	rootCmd.AddCommand(appCmd)
	appCmd.AddCommand(appListCmd)
	appCmd.AddCommand(appAddCmd)
	appCmd.AddCommand(appUpdateCmd)
	appCmd.AddCommand(appRemoveCmd)

	appAddCmd.Flags().BoolVarP(&appSyncNow, "sync", "s", false, "Sync to running containers immediately")
	appUpdateCmd.Flags().BoolVarP(&appAll, "all", "a", false, "Update all configured apps")
	appUpdateCmd.Flags().BoolVarP(&appQuiet, "quiet", "q", false, "Suppress output (for Makefile integration)")
	appRemoveCmd.Flags().BoolVar(&appCleanup, "cleanup", false, "Remove from running containers")
	appRemoveCmd.Flags().BoolVarP(&appQuiet, "quiet", "q", false, "Suppress output")
}

func runAppList(cmd *cobra.Command, args []string) error {
	if len(config.Apps) == 0 {
		fmt.Println("No apps configured.")
		fmt.Println("\nAdd an app with: maestro app add <name> <path>")
		return nil
	}

	fmt.Println("Configured apps:")
	for name, source := range config.Apps {
		expandedPath := expandPath(source)
		if info, err := os.Stat(expandedPath); err == nil {
			size := formatFileSize(info.Size())
			fmt.Printf("  %-20s → %s (%s)\n", name, source, size)
		} else {
			fmt.Printf("  %-20s → %s (⚠ not found)\n", name, source)
		}
	}

	// Check status in running containers
	containers, err := container.GetRunningContainers(config.Containers.Prefix)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) > 0 {
		fmt.Printf("\nRunning containers: %d\n", len(containers))
	}

	return nil
}

func runAppAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	source := args[1]

	// Expand and validate source path
	expandedPath := expandPath(source)
	info, err := os.Stat(expandedPath)
	if err != nil {
		return fmt.Errorf("source file not found: %s", expandedPath)
	}

	if !appQuiet {
		fmt.Printf("✓ Verified source exists (%s)\n", formatFileSize(info.Size()))
	}

	// Check if already exists
	if _, exists := config.Apps[name]; exists {
		if !appQuiet {
			fmt.Printf("⚠  App '%s' already configured, updating path\n", name)
		}
	}

	// Add to config
	if config.Apps == nil {
		config.Apps = make(map[string]string)
	}
	config.Apps[name] = source

	// Write config
	if err := writeConfigFile(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	if !appQuiet {
		fmt.Printf("✓ Added %s to configuration\n", name)
	}

	// Sync to running containers if requested
	if appSyncNow {
		if err := updateSingleApp(name, appQuiet); err != nil {
			return err
		}
	}

	return nil
}

func runAppUpdate(cmd *cobra.Command, args []string) error {
	// Determine which apps to update
	var appsToUpdate []string

	if appAll {
		// Update all apps
		for name := range config.Apps {
			appsToUpdate = append(appsToUpdate, name)
		}
		if len(appsToUpdate) == 0 {
			if !appQuiet {
				fmt.Println("No apps configured to update")
			}
			return nil
		}
	} else if len(args) > 0 {
		// Update specific app
		name := args[0]
		if _, exists := config.Apps[name]; !exists {
			return fmt.Errorf("app '%s' not found in configuration", name)
		}
		appsToUpdate = []string{name}
	} else {
		return fmt.Errorf("specify an app name or use --all")
	}

	// Update each app
	for _, name := range appsToUpdate {
		if err := updateSingleApp(name, appQuiet); err != nil {
			if !appQuiet {
				fmt.Printf("⚠  Failed to update %s: %v\n", name, err)
			}
			continue
		}
	}

	return nil
}

func runAppRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Check if exists
	if _, exists := config.Apps[name]; !exists {
		return fmt.Errorf("app '%s' not found in configuration", name)
	}

	// Remove from config
	delete(config.Apps, name)

	// Write config
	if err := writeConfigFile(); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	if !appQuiet {
		fmt.Printf("✓ Removed %s from configuration\n", name)
	}

	// Cleanup from containers if requested
	if appCleanup {
		containers, err := container.GetRunningContainers(config.Containers.Prefix)
		if err != nil {
			return fmt.Errorf("failed to list containers: %w", err)
		}

		if !appQuiet {
			fmt.Printf("Removing from %d container(s)...\n", len(containers))
		}

		for _, c := range containers {
			destPath := fmt.Sprintf("/usr/local/bin/%s", name)
			rmCmd := exec.Command("docker", "exec", "-u", "root", c.Name, "rm", "-f", destPath)
			rmCmd.Run() // Ignore errors (file might not exist)
			if !appQuiet {
				fmt.Printf("  ✓ %s\n", c.ShortName)
			}
		}
	}

	return nil
}

// updateSingleApp updates a single app in all running containers
func updateSingleApp(appName string, quiet bool) error {
	sourcePath, exists := config.Apps[appName]
	if !exists {
		return fmt.Errorf("app '%s' not configured", appName)
	}

	expandedPath := expandPath(sourcePath)

	// Check for Linux-specific variant first (for cross-platform binaries)
	linuxPath := expandedPath + ".linux_aarch64"
	actualPath := expandedPath
	if _, err := os.Stat(linuxPath); err == nil {
		actualPath = linuxPath
	}

	if _, err := os.Stat(actualPath); err != nil {
		return fmt.Errorf("source file not found: %s", actualPath)
	}

	// Calculate source checksum once
	sourceChecksum, err := calculateChecksum(actualPath)
	if err != nil {
		return fmt.Errorf("failed to calculate checksum: %w", err)
	}

	// Get running containers
	containers, err := container.GetRunningContainers(config.Containers.Prefix)
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	if len(containers) == 0 {
		if !quiet {
			fmt.Println("No running containers to update")
		}
		return nil
	}

	if !quiet {
		fmt.Printf("Updating %s in %d container(s)...\n", appName, len(containers))
	}

	// Update containers concurrently
	var wg sync.WaitGroup
	results := make(chan string, len(containers))

	for _, c := range containers {
		wg.Add(1)
		go func(container container.Info) {
			defer wg.Done()

			destPath := fmt.Sprintf("/usr/local/bin/%s", appName)
			containerPath := fmt.Sprintf("%s:%s", container.Name, destPath)

			// Check if file exists and compare checksums
			checkCmd := exec.Command("docker", "exec", container.Name, "sh", "-c",
				fmt.Sprintf("sha256sum %s 2>/dev/null | awk '{print $1}'", destPath))
			if output, err := checkCmd.Output(); err == nil {
				existingChecksum := strings.TrimSpace(string(output))
				if existingChecksum == sourceChecksum {
					results <- fmt.Sprintf("  ✓ %s (already up to date)", container.ShortName)
					return
				}
			}

			// Copy file
			cpCmd := exec.Command("docker", "cp", actualPath, containerPath)
			if err := cpCmd.Run(); err != nil {
				results <- fmt.Sprintf("  ✗ %s: %v", container.ShortName, err)
				return
			}

			// Make executable and set ownership
			chmodCmd := exec.Command("docker", "exec", "-u", "root", container.Name,
				"sh", "-c", fmt.Sprintf("chmod +x %s && chown node:node %s", destPath, destPath))
			if err := chmodCmd.Run(); err != nil {
				results <- fmt.Sprintf("  ⚠ %s: copied but failed to set permissions", container.ShortName)
				return
			}

			results <- fmt.Sprintf("  ✓ %s", container.ShortName)
		}(c)
	}

	// Wait for all updates to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Print results as they come in
	successCount := 0
	if !quiet {
		for result := range results {
			fmt.Println(result)
			if strings.Contains(result, "✓") {
				successCount++
			}
		}
		fmt.Printf("✅ Updated %s in %d container(s)\n", appName, successCount)
	} else {
		// In quiet mode, just count successes
		for result := range results {
			if strings.Contains(result, "✓") {
				successCount++
			}
		}
	}

	return nil
}

// calculateChecksum calculates SHA256 checksum of a file
func calculateChecksum(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

// writeConfigFile writes the current config to the config file
func writeConfigFile() error {
	// Write all settings back to viper
	viper.Set("apps", config.Apps)

	return viper.WriteConfig()
}

// formatFileSize formats bytes to human-readable format
func formatFileSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
