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
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/uprockcom/maestro/pkg/notify/signal"
)

var signalCmd = &cobra.Command{
	Use:   "signal",
	Short: "Manage Signal notification integration",
}

var signalSetupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Set up Signal notifications (register number, verify, test)",
	RunE:  runSignalSetup,
}

var signalBackupCmd = &cobra.Command{
	Use:   "backup",
	Short: "Back up Signal registration data (Docker volume + config)",
	RunE:  runSignalBackup,
}

var signalRestoreCmd = &cobra.Command{
	Use:   "restore",
	Short: "Restore Signal registration data from backup",
	RunE:  runSignalRestore,
}

var signalConnectCmd = &cobra.Command{
	Use:   "connect",
	Short: "Connect to a team Signal relay server",
	Long: `Connect to a shared Signal relay server hosted by your team.
Your admin will provide the relay URL and an API key.`,
	RunE: runSignalConnect,
}

var signalAddUserCmd = &cobra.Command{
	Use:   "add-user",
	Short: "Generate an API key for a new relay user (admin command)",
	Long: `Generate an API key for a developer joining the team relay.
The key hash is appended to keys.json; the plaintext key is displayed
for the admin to share with the developer.`,
	RunE: runSignalAddUser,
}

func init() {
	rootCmd.AddCommand(signalCmd)
	signalCmd.AddCommand(signalSetupCmd)
	signalCmd.AddCommand(signalBackupCmd)
	signalCmd.AddCommand(signalRestoreCmd)
	signalCmd.AddCommand(signalConnectCmd)
	signalCmd.AddCommand(signalAddUserCmd)
}

func runSignalSetup(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)
	logger := func(format string, args ...interface{}) {
		fmt.Printf(format+"\n", args...)
	}

	fmt.Println("Signal Notification Setup")
	fmt.Println("=========================")
	fmt.Println()
	fmt.Println("This will set up a Signal bot to receive Maestro notifications.")
	fmt.Println("You need an SMS-capable phone number for the bot (separate from your personal number).")
	fmt.Println()

	// Step 1: Pull image
	fmt.Println("Step 1: Pulling Signal CLI Docker image...")
	if err := signal.PullImage(logger); err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	fmt.Println("Done.")
	fmt.Println()

	// Step 2: Port selection
	fmt.Print("Step 2: Container port [8080]: ")
	portStr, _ := reader.ReadString('\n')
	portStr = strings.TrimSpace(portStr)
	port := 8080
	if portStr != "" {
		p, err := strconv.Atoi(portStr)
		if err != nil {
			return fmt.Errorf("invalid port: %s", portStr)
		}
		port = p
	}

	// Step 3: Start container
	fmt.Println()
	fmt.Printf("Step 3: Starting Signal CLI container on port %d...\n", port)
	if err := signal.EnsureRunning(port, logger); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	fmt.Println("Container is running and healthy.")
	fmt.Println()

	// Step 4: Bot phone number
	fmt.Print("Step 4: Bot phone number (with country code, e.g. +12025551234): ")
	botNumber, _ := reader.ReadString('\n')
	botNumber = strings.TrimSpace(botNumber)
	if botNumber == "" {
		return fmt.Errorf("bot phone number is required")
	}

	// Step 5: Captcha
	fmt.Println()
	fmt.Println("Step 5: Signal requires a captcha for registration.")
	fmt.Println("  1. Open this URL in your browser:")
	fmt.Println("     https://signalcaptchas.org/registration/generate.html")
	fmt.Println("  2. Solve the captcha")
	fmt.Println("  3. Right-click the \"Open Signal\" link and copy the link to your clipboard")
	fmt.Println("     (it starts with signalcaptcha://)")
	fmt.Println()
	captchaLink, err := readCaptchaToken(reader)
	if err != nil {
		return err
	}
	// Extract token from signalcaptcha://signal-recaptcha-v2.6LfBXs0bAAAAAAjkDyyI1Lk5gBAUWzh... format
	captchaToken := strings.TrimPrefix(captchaLink, "signalcaptcha://")

	// Step 6: Register
	fmt.Println()
	fmt.Printf("Step 6: Registering %s (an SMS will be sent)...\n", botNumber)
	api := signal.NewAPIClient(fmt.Sprintf("http://127.0.0.1:%d", port), botNumber)
	if err := api.Register(botNumber, captchaToken); err != nil {
		return fmt.Errorf("registration failed: %w", err)
	}
	fmt.Println("SMS verification code sent.")
	fmt.Println()

	// Step 7: Verify
	fmt.Print("Step 7: Enter the verification code: ")
	code, _ := reader.ReadString('\n')
	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("verification code is required")
	}

	if err := api.Verify(botNumber, code); err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}
	fmt.Println("Verified successfully!")
	fmt.Println()

	// Step 8: Recipient
	fmt.Print("Step 8: Your phone number (where notifications will be sent): ")
	recipient, _ := reader.ReadString('\n')
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return fmt.Errorf("recipient phone number is required")
	}

	// Step 9: Test message
	fmt.Println()
	fmt.Println("Step 9: Sending a test message...")
	if _, err := api.SendMessage(recipient, "[maestro] Signal setup complete! You will receive notifications here."); err != nil {
		return fmt.Errorf("failed to send test message: %w", err)
	}
	fmt.Println("Test message sent!")
	fmt.Println()

	// Step 9: Confirm
	fmt.Print("Did you receive the test message? [Y/n]: ")
	confirm, _ := reader.ReadString('\n')
	confirm = strings.TrimSpace(strings.ToLower(confirm))
	if confirm != "" && confirm != "y" && confirm != "yes" {
		fmt.Println("Setup not saved. Please check your Signal app and try again.")
		return nil
	}

	// Step 11: Save config
	viper.Set("daemon.notifications.providers.signal.enabled", true)
	viper.Set("daemon.notifications.providers.signal.number", botNumber)
	viper.Set("daemon.notifications.providers.signal.recipient", recipient)
	viper.Set("daemon.notifications.providers.signal.container_port", port)

	if err := viper.WriteConfig(); err != nil {
		// Try WriteConfigAs if no config file exists yet
		configFile := viper.ConfigFileUsed()
		if configFile == "" {
			return fmt.Errorf("failed to save config: %w", err)
		}
		if err := viper.WriteConfigAs(configFile); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}
	}

	fmt.Println()
	fmt.Println("Signal notifications configured successfully!")
	fmt.Println("Restart the daemon to activate: maestro daemon stop && maestro daemon start")
	fmt.Println()
	fmt.Println("Tip: Run 'maestro signal backup' to save your registration data.")
	fmt.Println("     You can restore it later without re-registering.")
	return nil
}

func runSignalBackup(cmd *cobra.Command, args []string) error {
	backupDir := filepath.Join(expandPath(config.Claude.AuthPath), "signal-backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return fmt.Errorf("failed to create backup directory: %w", err)
	}

	timestamp := time.Now().Format("20060102-150405")
	backupFile := filepath.Join(backupDir, fmt.Sprintf("signal-%s.tar.gz", timestamp))

	// Back up the Docker volume
	fmt.Println("Backing up Signal registration data...")
	dockerCmd := exec.Command("docker", "run", "--rm",
		"-v", "maestro-signal-data:/data:ro",
		"-v", backupDir+":/backup",
		"alpine",
		"tar", "czf", fmt.Sprintf("/backup/signal-%s.tar.gz", timestamp), "-C", "/data", ".",
	)
	if out, err := dockerCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("backup failed: %w\n%s", err, string(out))
	}

	fmt.Printf("Backup saved to: %s\n", backupFile)

	// Show config values for reference
	fmt.Println()
	fmt.Println("Current Signal config (also saved in config.yml):")
	fmt.Printf("  number:    %s\n", config.Daemon.Notifications.Providers.Signal.Number)
	fmt.Printf("  recipient: %s\n", config.Daemon.Notifications.Providers.Signal.Recipient)
	fmt.Printf("  port:      %d\n", config.Daemon.Notifications.Providers.Signal.ContainerPort)

	// List all backups
	fmt.Println()
	entries, _ := os.ReadDir(backupDir)
	fmt.Printf("Available backups (%d):\n", len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".tar.gz") {
			info, _ := e.Info()
			if info != nil {
				fmt.Printf("  %s  (%s)\n", e.Name(), formatFileSize(info.Size()))
			} else {
				fmt.Printf("  %s\n", e.Name())
			}
		}
	}
	return nil
}

func runSignalRestore(cmd *cobra.Command, args []string) error {
	backupDir := filepath.Join(expandPath(config.Claude.AuthPath), "signal-backups")

	// Find available backups
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return fmt.Errorf("no backups found at %s", backupDir)
	}

	var backups []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".tar.gz") {
			backups = append(backups, e.Name())
		}
	}
	if len(backups) == 0 {
		return fmt.Errorf("no backup files found in %s", backupDir)
	}

	// If a specific backup is given as argument, use it
	var backupFile string
	if len(args) > 0 {
		backupFile = args[0]
		// Check if it's a full path or just a filename
		if !filepath.IsAbs(backupFile) {
			backupFile = filepath.Join(backupDir, backupFile)
		}
		if _, err := os.Stat(backupFile); err != nil {
			return fmt.Errorf("backup file not found: %s", backupFile)
		}
	} else {
		// Show available backups and let user pick
		fmt.Println("Available backups:")
		for i, b := range backups {
			info, _ := os.Stat(filepath.Join(backupDir, b))
			if info != nil {
				fmt.Printf("  %d. %s  (%s)\n", i+1, b, formatFileSize(info.Size()))
			} else {
				fmt.Printf("  %d. %s\n", i+1, b)
			}
		}
		fmt.Println()

		reader := bufio.NewReader(os.Stdin)
		fmt.Printf("Select backup [1]: ")
		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)
		idx := 0
		if choice != "" {
			n, err := strconv.Atoi(choice)
			if err != nil || n < 1 || n > len(backups) {
				return fmt.Errorf("invalid selection: %s", choice)
			}
			idx = n - 1
		}
		backupFile = filepath.Join(backupDir, backups[idx])
	}

	fmt.Printf("Restoring from: %s\n", filepath.Base(backupFile))

	// Stop the signal container if running
	if signal.IsRunning() {
		fmt.Println("Stopping Signal container...")
		logger := func(format string, args ...interface{}) {
			fmt.Printf(format+"\n", args...)
		}
		signal.Stop(logger)
	}

	// Restore the volume
	fmt.Println("Restoring registration data...")
	dockerCmd := exec.Command("docker", "run", "--rm",
		"-v", "maestro-signal-data:/data",
		"-v", filepath.Dir(backupFile)+":/backup:ro",
		"alpine",
		"sh", "-c", "rm -rf /data/* && tar xzf /backup/"+filepath.Base(backupFile)+" -C /data",
	)
	if out, err := dockerCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("restore failed: %w\n%s", err, string(out))
	}

	// Restart container
	port := config.Daemon.Notifications.Providers.Signal.ContainerPort
	if port == 0 {
		port = 8080
	}
	fmt.Println("Starting Signal container...")
	logger := func(format string, args ...interface{}) {
		fmt.Printf(format+"\n", args...)
	}
	if err := signal.EnsureRunning(port, logger); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}

	fmt.Println()
	fmt.Println("Restore complete! Registration data has been recovered.")
	fmt.Println("Restart the daemon to reconnect: maestro daemon stop && maestro daemon start")
	return nil
}

func runSignalConnect(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Connect to Signal Relay")
	fmt.Println("=======================")
	fmt.Println()
	fmt.Println("Your team admin will provide the relay URL and API key.")
	fmt.Println()

	// Step 1: Relay URL
	fmt.Print("Relay URL (e.g. https://signal.example.com): ")
	relayURL, _ := reader.ReadString('\n')
	relayURL = strings.TrimSpace(relayURL)
	if relayURL == "" {
		return fmt.Errorf("relay URL is required")
	}
	relayURL = strings.TrimRight(relayURL, "/")

	// Step 2: API key
	fmt.Print("API key: ")
	apiKey, _ := reader.ReadString('\n')
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return fmt.Errorf("API key is required")
	}

	// Step 3: Bot number
	fmt.Print("Bot phone number (the shared team bot, e.g. +12025551234): ")
	botNumber, _ := reader.ReadString('\n')
	botNumber = strings.TrimSpace(botNumber)
	if botNumber == "" {
		return fmt.Errorf("bot phone number is required")
	}

	// Step 4: Recipient number
	fmt.Print("Your phone number (where you receive notifications): ")
	recipient, _ := reader.ReadString('\n')
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return fmt.Errorf("recipient phone number is required")
	}

	// Step 5: Health check
	fmt.Println()
	fmt.Println("Checking relay connection...")
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", relayURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("failed to create health check request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("relay health check failed: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay health check returned %d", resp.StatusCode)
	}
	fmt.Println("Relay is healthy!")

	// Step 6: Save config
	fmt.Println()
	viper.Set("daemon.notifications.providers.signal.enabled", true)
	viper.Set("daemon.notifications.providers.signal.number", botNumber)
	viper.Set("daemon.notifications.providers.signal.recipient", recipient)
	viper.Set("daemon.notifications.providers.signal.url", relayURL)
	viper.Set("daemon.notifications.providers.signal.api_key", apiKey)

	if err := viper.WriteConfig(); err != nil {
		configFile := viper.ConfigFileUsed()
		if configFile == "" {
			return fmt.Errorf("failed to save config: %w", err)
		}
		if err := viper.WriteConfigAs(configFile); err != nil {
			return fmt.Errorf("failed to write config: %w", err)
		}
	}

	fmt.Println("Signal relay configured successfully!")
	fmt.Println("Restart the daemon to activate: maestro daemon stop && maestro daemon start")
	return nil
}

// signalAddUserEntry represents a user entry in keys.json.
type signalAddUserEntry struct {
	Name      string `json:"name"`
	KeyHash   string `json:"key_hash"`
	Recipient string `json:"recipient"`
}

type signalKeysFile struct {
	Users []signalAddUserEntry `json:"users"`
}

func runSignalAddUser(cmd *cobra.Command, args []string) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("Add User to Signal Relay")
	fmt.Println("========================")
	fmt.Println()

	// Step 1: User name
	fmt.Print("User name (e.g. alice): ")
	name, _ := reader.ReadString('\n')
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("user name is required")
	}

	// Step 2: Recipient phone number
	fmt.Print("User's phone number (e.g. +15551234567): ")
	recipient, _ := reader.ReadString('\n')
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return fmt.Errorf("recipient phone number is required")
	}

	// Step 3: Generate API key
	keyBytes := make([]byte, 32)
	if _, err := cryptoRandRead(keyBytes); err != nil {
		return fmt.Errorf("failed to generate random key: %w", err)
	}
	plaintext := hex.EncodeToString(keyBytes)
	h := sha256.Sum256([]byte(plaintext))
	keyHash := hex.EncodeToString(h[:])

	// Step 4: Output
	fmt.Println()
	fmt.Println("Generated API key (give this to the developer):")
	fmt.Println()
	fmt.Printf("  %s\n", plaintext)
	fmt.Println()

	// Step 5: Keys file handling
	fmt.Print("Path to keys.json [./keys.json]: ")
	keysPath, _ := reader.ReadString('\n')
	keysPath = strings.TrimSpace(keysPath)
	if keysPath == "" {
		keysPath = "keys.json"
	}

	newUser := signalAddUserEntry{
		Name:      name,
		KeyHash:   keyHash,
		Recipient: recipient,
	}

	var kf signalKeysFile
	if data, err := os.ReadFile(keysPath); err == nil {
		json.Unmarshal(data, &kf)
	}
	kf.Users = append(kf.Users, newUser)

	data, err := json.MarshalIndent(kf, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal keys file: %w", err)
	}

	if err := os.WriteFile(keysPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write keys file: %w", err)
	}

	fmt.Printf("User %q added to %s\n", name, keysPath)
	fmt.Println()
	fmt.Println("The developer should run: maestro signal connect")
	fmt.Printf("  Relay URL: (your relay URL)\n")
	fmt.Printf("  API key:   %s\n", plaintext)
	return nil
}

// cryptoRandRead is a variable for testing; defaults to crypto/rand.Read.
var cryptoRandRead = cryptoRandReadDefault

func cryptoRandReadDefault(b []byte) (int, error) {
	return rand.Read(b)
}

// readCaptchaToken reads the captcha token via clipboard or file.
// The token is too long (~500+ chars) for terminal line buffers in canonical mode,
// so direct paste often fails. We offer clipboard and file-based alternatives.
func readCaptchaToken(reader *bufio.Reader) (string, error) {
	// Detect clipboard command
	var clipCmd string
	switch runtime.GOOS {
	case "darwin":
		clipCmd = "pbpaste"
	case "linux":
		// Try xclip first, then xsel
		if _, err := exec.LookPath("xclip"); err == nil {
			clipCmd = "xclip -selection clipboard -o"
		} else if _, err := exec.LookPath("xsel"); err == nil {
			clipCmd = "xsel --clipboard --output"
		}
	}

	if clipCmd != "" {
		fmt.Println("Press Enter to read from clipboard (or type a file path):")
	} else {
		fmt.Println("Enter a file path containing the captcha link:")
	}
	fmt.Print("> ")

	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	var token string

	if input == "" && clipCmd != "" {
		// Read from clipboard
		parts := strings.Fields(clipCmd)
		cmd := exec.Command(parts[0], parts[1:]...)
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("failed to read clipboard: %w", err)
		}
		token = strings.TrimSpace(string(out))
	} else if input != "" {
		// Try as file path
		expanded := expandPath(input)
		data, err := os.ReadFile(expanded)
		if err != nil {
			// Not a file — treat as direct paste (might be truncated but try anyway)
			token = input
		} else {
			token = strings.TrimSpace(string(data))
		}
	} else {
		return "", fmt.Errorf("captcha is required for Signal registration")
	}

	if !strings.Contains(token, "signalcaptcha") {
		return "", fmt.Errorf("invalid captcha token (expected signalcaptcha:// link, got: %.40s...)", token)
	}

	fmt.Printf("Captcha token read (%d chars)\n", len(token))
	return token, nil
}

