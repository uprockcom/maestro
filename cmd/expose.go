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
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

const exposePrefix = "maestro-expose-"

var (
	exposeHostPort int
	exposeList     bool
)

var exposeCmd = &cobra.Command{
	Use:   "expose <container> [port]",
	Short: "Forward a container port to the host",
	Long: `Forward a port from a running Maestro container to the macOS host using a
sidecar container. On macOS Docker Desktop, container IPs are not routable from
the host, so this command launches a small alpine/socat sidecar that bridges the
port.

If no port is specified, the command discovers listening ports inside the
container and prints them so you can re-run with a specific port.

Use --list to see all active port forwards.`,
	Args: cobra.RangeArgs(0, 2),
	RunE: runExpose,
}

var unexposeCmd = &cobra.Command{
	Use:   "unexpose <container> <port>",
	Short: "Remove a forwarded port",
	Long:  `Remove a port forward that was created with 'maestro expose'.`,
	Args:  cobra.ExactArgs(2),
	RunE:  runUnexpose,
}

func init() {
	rootCmd.AddCommand(exposeCmd)
	rootCmd.AddCommand(unexposeCmd)

	exposeCmd.Flags().IntVar(&exposeHostPort, "host-port", 0,
		"Host port to bind (default: same as container port)")
	exposeCmd.Flags().BoolVar(&exposeList, "list", false,
		"List all active port forwards")
}

func runExpose(cmd *cobra.Command, args []string) error {
	// --list: show all active sidecars regardless of other args
	if exposeList {
		return listExposures()
	}

	// Need at least a container name
	if len(args) == 0 {
		return fmt.Errorf("usage: maestro expose <container> [port]\nUse --list to see active forwards")
	}

	shortName := args[0]
	containerName := resolveContainerName(shortName)

	// Verify container is running
	if err := requireRunning(containerName, shortName); err != nil {
		return err
	}

	// No port: discover and print listening ports
	if len(args) < 2 {
		return discoverPorts(containerName)
	}

	// Parse and validate port
	port, err := parsePort(args[1])
	if err != nil {
		return err
	}

	// Determine host port
	hostPort := port
	if exposeHostPort != 0 {
		hp, err := parsePort(strconv.Itoa(exposeHostPort))
		if err != nil {
			return fmt.Errorf("invalid --host-port: %w", err)
		}
		hostPort = hp
	}

	return launchSidecar(containerName, port, hostPort)
}

func runUnexpose(cmd *cobra.Command, args []string) error {
	shortName := args[0]
	containerName := resolveContainerName(shortName)

	port, err := parsePort(args[1])
	if err != nil {
		return err
	}

	return removeSidecar(containerName, port)
}

// listExposures prints a table of all running expose sidecars.
func listExposures() error {
	out, err := exec.Command("docker", "ps", "-a",
		"--filter", "name="+exposePrefix,
		"--format", "{{.Names}}\t{{.State}}\t{{.Ports}}").Output()
	if err != nil {
		return fmt.Errorf("failed to list exposures: %w", err)
	}

	lines := nonEmpty(strings.Split(string(out), "\n"))
	if len(lines) == 0 {
		fmt.Println("No active port forwards.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SIDECAR\tSTATE\tPORTS")
	for _, line := range lines {
		fmt.Fprintln(w, line)
	}
	w.Flush()
	return nil
}

// discoverPorts runs ss inside the container and prints LISTEN ports.
func discoverPorts(containerName string) error {
	out, err := exec.Command("docker", "exec", containerName, "ss", "-tlnp").Output()
	if err != nil {
		return fmt.Errorf("failed to run ss in container: %w", err)
	}

	// Parse ss output: columns are Netid State Recv-Q Send-Q Local Address:Port ...
	// We want lines where State == LISTEN and extract the port from Local Address:Port.
	var ports []string
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		// ss -tlnp output: Netid State Recv-Q Send-Q Local-Address:Port Peer-Address:Port [Process]
		if len(fields) < 5 {
			continue
		}
		// State is second field (index 1)
		if fields[1] != "LISTEN" {
			continue
		}
		// Local Address:Port is fifth field (index 4)
		localAddr := fields[4]
		// Port is after the last colon
		idx := strings.LastIndex(localAddr, ":")
		if idx < 0 {
			continue
		}
		p := localAddr[idx+1:]
		if p != "" && p != "*" {
			ports = append(ports, p)
		}
	}

	if len(ports) == 0 {
		fmt.Println("No listening ports found in container.")
		return nil
	}

	fmt.Printf("Listening ports in %s:\n", containerName)
	for _, p := range ports {
		fmt.Printf("  %s\n", p)
	}
	fmt.Printf("\nTo forward a port, run:\n  maestro expose %s <port>\n", containerName)
	return nil
}

// launchSidecar starts the alpine/socat sidecar container.
// The sidecar runs on the default bridge network with a published port and
// connects to the target container by its bridge IP. We can't use
// --network container:X because Docker doesn't allow -p with that mode.
func launchSidecar(containerName string, port, hostPort int) error {
	sidecarName := sidecarName(containerName, port)

	// Check if sidecar already exists
	stateOut, err := exec.Command("docker", "ps", "-a",
		"--filter", "name=^"+sidecarName+"$",
		"--format", "{{.State}}").Output()
	if err != nil {
		return fmt.Errorf("failed to check for existing sidecar: %w", err)
	}

	existing := strings.TrimSpace(string(stateOut))
	if existing == "running" {
		fmt.Printf("Port %d of %s is already forwarded to localhost:%d\n", port, containerName, hostPort)
		return nil
	}
	if existing != "" {
		// Exists but not running — remove the stale container first
		fmt.Printf("Removing stale sidecar (%s)...\n", existing)
		if err := exec.Command("docker", "rm", "-f", sidecarName).Run(); err != nil {
			return fmt.Errorf("failed to remove stale sidecar: %w", err)
		}
	}

	// Get the target container's IP on its Docker network so the sidecar can
	// reach it. Try all attached networks and use the first non-empty IP.
	targetIP, err := getContainerIP(containerName)
	if err != nil {
		return err
	}

	socatArg := fmt.Sprintf("TCP-LISTEN:%d,fork,reuseaddr", port)
	connectArg := fmt.Sprintf("TCP-CONNECT:%s:%d", targetIP, port)

	runArgs := []string{
		"run", "-d",
		"--name", sidecarName,
		"-p", fmt.Sprintf("%d:%d", hostPort, port),
		"alpine/socat",
		socatArg,
		connectArg,
	}

	if out, err := exec.Command("docker", runArgs...).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start sidecar: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	// Verify the sidecar actually started (it might exit immediately if port is not open)
	verifyOut, err := exec.Command("docker", "ps", "-a",
		"--filter", "name=^"+sidecarName+"$",
		"--format", "{{.State}}").Output()
	if err != nil {
		return fmt.Errorf("failed to verify sidecar state: %w", err)
	}

	state := strings.TrimSpace(string(verifyOut))
	if state != "running" {
		// Fetch logs to give a useful error message
		logs, _ := exec.Command("docker", "logs", sidecarName).CombinedOutput()
		_ = exec.Command("docker", "rm", "-f", sidecarName).Run()
		return fmt.Errorf("sidecar exited immediately (state: %s)\n%s", state, strings.TrimSpace(string(logs)))
	}

	fmt.Printf("Forwarding %s:%d -> localhost:%d\n", containerName, port, hostPort)
	return nil
}

// getContainerIP returns the container's IP address on its Docker network.
func getContainerIP(containerName string) (string, error) {
	out, err := exec.Command("docker", "inspect",
		"--format", "{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}",
		containerName).Output()
	if err != nil {
		return "", fmt.Errorf("failed to get container IP: %w", err)
	}

	for _, ip := range strings.Fields(string(out)) {
		if ip != "" {
			return ip, nil
		}
	}
	return "", fmt.Errorf("container %s has no IP address on any network", containerName)
}

// removeSidecar stops and removes a sidecar container.
func removeSidecar(containerName string, port int) error {
	name := sidecarName(containerName, port)

	// Check it exists
	stateOut, err := exec.Command("docker", "ps", "-a",
		"--filter", "name=^"+name+"$",
		"--format", "{{.State}}").Output()
	if err != nil {
		return fmt.Errorf("failed to check sidecar: %w", err)
	}

	if strings.TrimSpace(string(stateOut)) == "" {
		return fmt.Errorf("no port forward found for %s:%d", containerName, port)
	}

	if out, err := exec.Command("docker", "rm", "-f", name).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to remove sidecar: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	fmt.Printf("Removed port forward for %s:%d\n", containerName, port)
	return nil
}

// removeExposeSidecarsForContainers removes all expose sidecars associated with
// any of the given container names. Errors are collected but do not abort the loop.
func removeExposeSidecarsForContainers(containerNames []string) {
	if len(containerNames) == 0 {
		return
	}

	// List all expose sidecars at once
	out, err := exec.Command("docker", "ps", "-a",
		"--filter", "name="+exposePrefix,
		"--format", "{{.Names}}").Output()
	if err != nil {
		return
	}

	for _, sidecar := range nonEmpty(strings.Split(string(out), "\n")) {
		for _, target := range containerNames {
			// Sidecar name is maestro-expose-<containerName>-<port>
			prefix := exposePrefix + target + "-"
			if strings.HasPrefix(sidecar, prefix) {
				if err := exec.Command("docker", "rm", "-f", sidecar).Run(); err != nil {
					fmt.Printf("  Warning: failed to remove sidecar %s: %v\n", sidecar, err)
				} else {
					fmt.Printf("  Removed expose sidecar: %s\n", sidecar)
				}
				break
			}
		}
	}
}

// sidecarName returns the canonical name for an expose sidecar.
func sidecarName(containerName string, port int) string {
	return fmt.Sprintf("%s%s-%d", exposePrefix, containerName, port)
}

// requireRunning returns an error if the named container is not in the running state.
func requireRunning(containerName, shortName string) error {
	out, err := exec.Command("docker", "ps",
		"--filter", "name=^"+containerName+"$",
		"--format", "{{.State}}").Output()
	if err != nil {
		return fmt.Errorf("failed to check container status: %w", err)
	}

	state := strings.TrimSpace(string(out))
	if state != "running" {
		return fmt.Errorf("container %s is not running", shortName)
	}
	return nil
}

// parsePort parses and validates a port string.
func parsePort(s string) (int, error) {
	p, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || p < 1 || p > 65535 {
		return 0, fmt.Errorf("invalid port %q: must be an integer between 1 and 65535", s)
	}
	return p, nil
}

// nonEmpty filters empty strings from a slice.
func nonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
