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
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/uprockcom/maestro/pkg/container"
	"github.com/uprockcom/maestro/pkg/paths"
	"gopkg.in/yaml.v3"
)

var addDomainCmd = &cobra.Command{
	Use:   "add-domain <container-name> <domain>",
	Short: "Add an allowed domain to a container's firewall",
	Long: `Add a domain to the firewall whitelist for a specific container.
The domain will be resolved and added to the container's firewall rules.

This is a temporary addition for the running container. To make it permanent,
add the domain to your configuration file.`,
	Args: cobra.ExactArgs(2),
	RunE: runAddDomain,
}

func init() {
	rootCmd.AddCommand(addDomainCmd)
}

func runAddDomain(cmd *cobra.Command, args []string) error {
	shortName := args[0]
	domain := args[1]

	// Validate domain before any operations
	if err := container.ValidateDomain(domain); err != nil {
		return fmt.Errorf("invalid domain: %w", err)
	}

	containerName := resolveContainerName(shortName)

	// Check if container is running
	checkCmd := exec.Command("docker", "ps", "--filter", fmt.Sprintf("name=%s", containerName), "--format", "{{.State}}")
	output, err := checkCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}

	state := strings.TrimSpace(string(output))
	if state != "running" {
		return fmt.Errorf("container %s is not running", shortName)
	}

	fmt.Printf("Adding %s to firewall whitelist for %s...\n", domain, containerName)

	// Add domain to dnsmasq configuration so it automatically tracks all IPs
	fmt.Println("  Updating dnsmasq configuration...")
	dnsmasqConf := "/tmp/dnsmasq-firewall.conf"

	// Check if domain already in config (grep arg is safe — not through shell)
	checkConfCmd := exec.Command("docker", "exec", containerName, "grep", "-qF",
		fmt.Sprintf("ipset=/%s/", domain), dnsmasqConf)
	if checkConfCmd.Run() == nil {
		fmt.Printf("  Domain %s already in dnsmasq config\n", domain)
	} else {
		// Append domain to dnsmasq config using positional parameters (no interpolation)
		appendCmd := exec.Command("docker", "exec", "-u", "root", containerName,
			"sh", "-c", `printf '%s\n' "ipset=/$1/allowed-domains" "server=/$1/8.8.8.8" >> "$2"`,
			"_", domain, dnsmasqConf)
		if err := appendCmd.Run(); err != nil {
			return fmt.Errorf("failed to update dnsmasq config: %w", err)
		}
		fmt.Println("  Updated dnsmasq config")
	}

	// Restart dnsmasq to pick up new config
	fmt.Println("  Restarting dnsmasq...")
	restartCmd := exec.Command("docker", "exec", "-u", "root", containerName, "sh", "-c",
		"pkill -9 dnsmasq 2>/dev/null || true; sleep 0.2; dnsmasq --conf-file=/tmp/dnsmasq-firewall.conf")
	if err := restartCmd.Run(); err != nil {
		return fmt.Errorf("failed to restart dnsmasq: %w", err)
	}

	// Give dnsmasq a moment to start
	// time.Sleep(500 * time.Millisecond)

	// Now do an initial resolution to populate the ipset
	fmt.Println("  Performing initial DNS resolution...")
	resolveCmd := exec.Command("docker", "exec", containerName,
		"sh", "-c", `dig +short "$1" | head -5`, "_", domain)
	output, err = resolveCmd.Output()
	if err != nil {
		fmt.Printf("  Warning: initial resolution failed: %v\n", err)
	} else {
		ips := strings.Split(strings.TrimSpace(string(output)), "\n")
		fmt.Printf("  Resolved %d IPs (dnsmasq will track all future resolutions)\n", len(ips))
	}

	fmt.Printf("\n✅ Domain %s added to %s\n", domain, containerName)
	fmt.Println("   DNS queries for this domain will now automatically populate the firewall whitelist.")
	fmt.Printf("\nTo make this permanent, add it to %s:\n", paths.ConfigFile())
	fmt.Printf("  firewall:\n    allowed_domains:\n      - %s\n", domain)

	// Offer to update config
	fmt.Printf("\nWould you like to add this domain to %s now? [y/N]: ", paths.ConfigFile())
	var response string
	fmt.Scanln(&response)

	if strings.ToLower(response) == "y" || strings.ToLower(response) == "yes" {
		if err := updateConfigWithDomain(domain); err != nil {
			fmt.Printf("Failed to update config: %v\n", err)
		} else {
			fmt.Printf("✅ Updated %s\n", paths.ConfigFile())
		}
	}

	return nil
}

func updateConfigWithDomain(domain string) error {
	configPath := paths.ConfigFile()

	// Read current config
	var configData map[string]interface{}

	if _, err := os.Stat(configPath); err == nil {
		// File exists, read it
		content, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}

		if err := yaml.Unmarshal(content, &configData); err != nil {
			return err
		}
	} else {
		// File doesn't exist, create new config
		configData = make(map[string]interface{})
	}

	// Add domain to firewall.allowed_domains
	if configData["firewall"] == nil {
		configData["firewall"] = make(map[string]interface{})
	}

	firewall := configData["firewall"].(map[string]interface{})

	var domains []string
	if firewall["allowed_domains"] != nil {
		// Convert existing domains to string slice
		existingDomains := firewall["allowed_domains"].([]interface{})
		for _, d := range existingDomains {
			domains = append(domains, d.(string))
		}
	} else {
		// Use defaults from viper
		domains = viper.GetStringSlice("firewall.allowed_domains")
	}

	// Check if domain already exists
	for _, d := range domains {
		if d == domain {
			fmt.Printf("Domain %s already in config\n", domain)
			return nil
		}
	}

	// Add new domain
	domains = append(domains, domain)
	firewall["allowed_domains"] = domains
	configData["firewall"] = firewall

	// Write updated config
	output, err := yaml.Marshal(configData)
	if err != nil {
		return err
	}

	return os.WriteFile(configPath, output, 0644)
}
