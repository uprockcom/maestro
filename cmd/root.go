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

	"github.com/uprockcom/maestro/pkg/paths"
	"github.com/uprockcom/maestro/pkg/tui"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	cfgFile string
	config  *Config
)

// Config represents the maestro configuration
type Config struct {
	Claude struct {
		ConfigPath  string `mapstructure:"config_path"`
		AuthPath    string `mapstructure:"auth_path"`
		DefaultMode string `mapstructure:"default_mode"`
	} `mapstructure:"claude"`

	Containers struct {
		Prefix string `mapstructure:"prefix"`
		Image  string `mapstructure:"image"`
		Resources struct {
			Memory string `mapstructure:"memory"`
			CPUs   string `mapstructure:"cpus"`
		} `mapstructure:"resources"`
	} `mapstructure:"containers"`

	Tmux struct {
		DefaultSession string `mapstructure:"default_session"`
		Prefix         string `mapstructure:"prefix"`
	} `mapstructure:"tmux"`

	Firewall struct {
		AllowedDomains  []string `mapstructure:"allowed_domains"`
		InternalDNS     string   `mapstructure:"internal_dns"`
		InternalDomains []string `mapstructure:"internal_domains"`
	} `mapstructure:"firewall"`

	Sync struct {
		AdditionalFolders []string `mapstructure:"additional_folders"`
	} `mapstructure:"sync"`

	SSH struct {
		Enabled bool `mapstructure:"enabled"`
	} `mapstructure:"ssh"`

	SSL struct {
		CertificatesPath string `mapstructure:"certificates_path"`
	} `mapstructure:"ssl"`

	Git struct {
		UserName  string `mapstructure:"user_name"`
		UserEmail string `mapstructure:"user_email"`
	} `mapstructure:"git"`

	GitHub struct {
		Enabled    bool   `mapstructure:"enabled"`
		ConfigPath string `mapstructure:"config_path"`
	} `mapstructure:"github"`

	AWS struct {
		Enabled bool   `mapstructure:"enabled"`
		Profile string `mapstructure:"profile"`
		Region  string `mapstructure:"region"`
	} `mapstructure:"aws"`

	Bedrock struct {
		Enabled bool   `mapstructure:"enabled"`
		Model   string `mapstructure:"model"`
	} `mapstructure:"bedrock"`

	Daemon struct {
		CheckInterval string `mapstructure:"check_interval"`
		ShowNag       bool   `mapstructure:"show_nag"`
		TokenRefresh  struct {
			Enabled   bool   `mapstructure:"enabled"`
			Threshold string `mapstructure:"threshold"`
		} `mapstructure:"token_refresh"`
		Notifications struct {
			Enabled            bool     `mapstructure:"enabled"`
			AttentionThreshold string   `mapstructure:"attention_threshold"`
			NotifyOn           []string `mapstructure:"notify_on"`
			QuietHours         struct {
				Start string `mapstructure:"start"`
				End   string `mapstructure:"end"`
			} `mapstructure:"quiet_hours"`
		} `mapstructure:"notifications"`
	} `mapstructure:"daemon"`

	Apps map[string]string `mapstructure:"apps"` // name -> source path
}

var rootCmd = &cobra.Command{
	Use:   "maestro",
	Short: "Multi-Container Claude - Manage isolated Claude development environments",
	Long: `maestro (Multi-Container Claude) is a tool for managing isolated Docker containers
for Claude Code development. It allows you to run multiple Claude instances in
parallel, each in their own isolated environment with proper branch management.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Keep running TUI in a loop until user explicitly quits
		// Maintain cached state for seamless return from containers
		var cachedState *tui.CachedState
		for {
			result, newState, err := tui.Run(config.Containers.Prefix, cachedState)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
				os.Exit(1)
			}

			// Save state for next iteration (seamless return)
			cachedState = newState

			// Handle the result action
			if result == nil {
				break
			}

			switch result.Action {
			case tui.ActionConnect:
				// Connect to the selected container
				err := performConnect(result.ContainerName)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error connecting: %v\n", err)
					fmt.Println("Press Enter to continue...")
					fmt.Scanln()
				}
				// Loop continues, TUI will restart with cached state
			case tui.ActionCreate:
				// Create a new container
				err := performCreate(result.TaskDescription, result.BranchName, result.NoConnect, result.Exact)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error creating container: %v\n", err)
					fmt.Println("Press Enter to continue...")
					fmt.Scanln()
				}
				// Loop continues, TUI will restart with cached state
			case tui.ActionEditConfig:
				// TODO: Open config file in editor
				fmt.Printf("Edit config: %s\n", result.FilePath)
				fmt.Println("Press Enter to continue...")
				fmt.Scanln()
			case tui.ActionRunCommand:
				// TODO: Execute the command
				fmt.Println("Run command (not yet implemented)")
				fmt.Println("Press Enter to continue...")
				fmt.Scanln()
			case tui.ActionRunAuth:
				// Run maestro auth command
				fmt.Println("\nRunning authentication setup...")
				if err := runAuth(nil, nil); err != nil {
					fmt.Fprintf(os.Stderr, "Error during authentication: %v\n", err)
					fmt.Println("Press Enter to continue...")
					fmt.Scanln()
				} else {
					fmt.Println("\n✓ Authentication complete!")
					fmt.Println("Press Enter to return to Maestro...")
					fmt.Scanln()
				}
				// Loop continues, TUI will restart with cached state
			case tui.ActionQuit:
				// Exit the loop
				return
			}
		}
	},
}

// Execute runs the root command
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "",
		"config file (default is $HOME/.maestro/config.yml)")
}

// performConnect connects to a container's tmux session
func performConnect(containerName string) error {
	// Verify container is running
	checkCmd := exec.Command("docker", "inspect", "-f", "{{.State.Status}}", containerName)
	output, err := checkCmd.Output()
	if err != nil {
		// Try as short name
		shortName := containerName
		if !strings.HasPrefix(shortName, config.Containers.Prefix) {
			containerName = config.Containers.Prefix + shortName
			checkCmd = exec.Command("docker", "inspect", "-f", "{{.State.Status}}", containerName)
			output, err = checkCmd.Output()
			if err != nil {
				return fmt.Errorf("container %s not found", shortName)
			}
		} else {
			return fmt.Errorf("container %s not found", containerName)
		}
	}

	state := strings.TrimSpace(string(output))
	if state == "" {
		return fmt.Errorf("container %s not found", containerName)
	}
	if state != "running" {
		return fmt.Errorf("container %s is not running (status: %s)", containerName, state)
	}

	fmt.Printf("Connecting to %s...\n", containerName)
	fmt.Println("Detach with: Ctrl+b d")
	fmt.Println("Switch windows: Ctrl+b 0 (Claude), Ctrl+b 1 (shell)")

	// Connect to tmux session
	connectCmd := exec.Command("docker", "exec", "-it", containerName, "tmux", "attach", "-t", "main")
	connectCmd.Stdin = os.Stdin
	connectCmd.Stdout = os.Stdout
	connectCmd.Stderr = os.Stderr

	return connectCmd.Run()
}

// performCreate creates a new container from TUI form data
func performCreate(taskDescription, branchName string, noConnect, exact bool) error {
	if taskDescription == "" {
		return fmt.Errorf("task description is required")
	}

	return CreateContainerFromTUI(taskDescription, branchName, noConnect, exact)
}

func initConfig() {
	if cfgFile != "" {
		viper.SetConfigFile(cfgFile)
	} else {
		// Use new paths package for config location
		configDir := paths.GetConfigDir()
		configFile := paths.ConfigFile()

		// Try new path first (~/.maestro/config.yml)
		viper.SetConfigFile(configFile)

		// Also search in config directory for flexibility
		viper.AddConfigPath(configDir)
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")

		// Check for legacy config and show migration message if found
		if paths.HasLegacyConfig() {
			legacyFile := paths.LegacyConfigFile()
			if _, err := os.Stat(legacyFile); err == nil {
				fmt.Fprintf(os.Stderr, "\n⚠️  Warning: Found old configuration at %s\n", legacyFile)
				fmt.Fprintf(os.Stderr, "   Run: ./scripts/migrate-configs.sh to migrate to %s\n\n", configFile)
			}
		}
	}

	// Set defaults - use paths package for directory defaults
	viper.SetDefault("claude.config_path", "~/.claude")
	viper.SetDefault("claude.auth_path", paths.AuthDir())
	viper.SetDefault("claude.default_mode", "yolo")
	viper.SetDefault("containers.prefix", "maestro-")
	viper.SetDefault("containers.image", "ghcr.io/uprockcom/maestro:latest")
	viper.SetDefault("containers.resources.memory", "4g")
	viper.SetDefault("containers.resources.cpus", "2")
	viper.SetDefault("tmux.default_session", "main")
	viper.SetDefault("tmux.prefix", "C-b")
	viper.SetDefault("firewall.allowed_domains", []string{
		"registry.npmjs.org",
		"api.anthropic.com",
		"github.com",
		"pypi.org",
		"files.pythonhosted.org",
		"sentry.io",
		"statsig.anthropic.com",
		"statsig.com",
		// AWS Bedrock domains
		"sts.amazonaws.com",
		"bedrock.amazonaws.com",
		"bedrock-runtime.amazonaws.com",
	})
	viper.SetDefault("firewall.internal_dns", "")
	viper.SetDefault("firewall.internal_domains", []string{})
	viper.SetDefault("ssh.enabled", false)
	viper.SetDefault("ssl.certificates_path", paths.CertificatesDir())
	viper.SetDefault("git.user_name", "")
	viper.SetDefault("git.user_email", "")
	viper.SetDefault("github.enabled", false)
	viper.SetDefault("github.config_path", paths.GitHubAuthDir())
	viper.SetDefault("aws.enabled", false)
	viper.SetDefault("aws.profile", "")
	viper.SetDefault("aws.region", "")
	viper.SetDefault("bedrock.enabled", false)
	viper.SetDefault("bedrock.model", "")
	viper.SetDefault("daemon.check_interval", "30m")
	viper.SetDefault("daemon.show_nag", true)
	viper.SetDefault("daemon.token_refresh.enabled", true)
	viper.SetDefault("daemon.token_refresh.threshold", "6h")
	viper.SetDefault("daemon.notifications.enabled", true)
	viper.SetDefault("daemon.notifications.attention_threshold", "5m")
	viper.SetDefault("daemon.notifications.notify_on", []string{"attention_needed", "token_expiring"})
	viper.SetDefault("daemon.notifications.quiet_hours.start", "")
	viper.SetDefault("daemon.notifications.quiet_hours.end", "")
	viper.SetDefault("apps", map[string]string{})
	viper.SetDefault("wizard.always_run", false)
	viper.SetDefault("wizard.resume_after_auth", false)

	// Read config
	if err := viper.ReadInConfig(); err != nil {
		// Config file not found is OK, use defaults
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintf(os.Stderr, "Error reading config file: %v\n", err)
		}
	}

	// Unmarshal config
	config = &Config{}
	if err := viper.Unmarshal(config); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing config: %v\n", err)
		os.Exit(1)
	}
}