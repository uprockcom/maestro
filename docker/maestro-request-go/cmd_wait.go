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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

const defaultPollInterval = 2 * time.Second

var errRequestFailed = errors.New("request failed")

// --- Shared polling helpers ---

// pollRequestStatus polls a request file until it reaches targetOrder or context is canceled.
// Returns (*RequestFile, nil) on success, (*RequestFile, errRequestFailed) on failed status,
// (nil, ctx.Err()) on context cancellation.
func pollRequestStatus(ctx context.Context, id string, targetOrder int) (*RequestFile, error) {
	for {
		r, err := readRequestFile(id)
		if err == nil {
			currentOrder := statusOrder(r.Status)
			if currentOrder == -1 {
				return r, errRequestFailed
			}
			if currentOrder >= targetOrder {
				return r, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(defaultPollInterval):
		}
	}
}

// runScript runs a command with context cancellation and process group cleanup.
// Captures stdout+stderr (does not stream). Returns (exitCode, capturedOutput, error).
// exitCode is -1 if the process could not be started or was killed by context.
func runScript(ctx context.Context, command []string) (int, string, error) {
	child := exec.CommandContext(ctx, command[0], command[1:]...)
	child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	output, err := child.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			if child.Process != nil {
				_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
			}
			return -1, string(output), ctx.Err()
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), string(output), nil
		}
		return -1, string(output), err
	}
	return 0, string(output), nil
}

// pollDaemon polls daemon availability until reachable or context is canceled.
// Returns (statusJSON, nil) on success, ("", ctx.Err()) on cancellation.
func pollDaemon(ctx context.Context) (string, error) {
	for {
		if daemonAvailable() {
			resp, err := daemonCall("GET", "/status", "")
			if err == nil && resp != "" {
				return resp, nil
			}
		}

		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(defaultPollInterval):
		}
	}
}

// pollIdleRequest sends a wait_idle request to the daemon and polls until fulfilled or failed.
// The timeout parameter is the daemon-side timeout; the context controls the local polling deadline.
// Returns (*RequestFile, nil) on fulfilled, (*RequestFile, errRequestFailed) on failed.
func pollIdleRequest(ctx context.Context, targetRequestID string, timeout int) (*RequestFile, error) {
	id, err := generateUUID()
	if err != nil {
		return nil, err
	}

	parent := hostname()

	reqJSON, _ := json.Marshal(map[string]interface{}{
		"id":                id,
		"action":            "wait_idle",
		"parent":            parent,
		"target_request_id": targetRequestID,
		"timeout":           timeout,
	})

	rf := &RequestFile{
		ID:              id,
		Action:          "wait_idle",
		Parent:          parent,
		Status:          "pending",
		RequestedAt:     nowUTC(),
		TargetRequestID: targetRequestID,
		Timeout:         timeout,
	}
	if err := writeRequestFile(rf); err != nil {
		return nil, err
	}

	if !daemonAvailable() {
		return nil, fmt.Errorf("daemon not available")
	}

	resp, err := daemonCall("POST", "/request", string(reqJSON))
	if err != nil {
		return nil, err
	}

	var result map[string]interface{}
	if json.Unmarshal([]byte(resp), &result) == nil {
		if status, _ := result["status"].(string); status == "error" {
			errMsg, _ := result["error"].(string)
			return nil, fmt.Errorf("%s", errMsg)
		}
	}

	// Poll until fulfilled or failed
	for {
		r, readErr := readRequestFile(id)
		if readErr == nil {
			switch r.Status {
			case "fulfilled":
				return r, nil
			case "failed":
				return r, errRequestFailed
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(defaultPollInterval):
		}
	}
}

// --- Cobra commands ---

func waitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Block until a condition is met",
	}

	cmd.AddCommand(waitRequestCmd())
	cmd.AddCommand(waitScriptCmd())
	cmd.AddCommand(waitDaemonCmd())
	cmd.AddCommand(waitIdleCmd())
	cmd.AddCommand(waitAnyCmd())
	cmd.AddCommand(waitAllCmd())
	return cmd
}

func waitRequestCmd() *cobra.Command {
	var timeout int

	cmd := &cobra.Command{
		Use:   "request <id> <status>",
		Short: "Wait for a request to reach a target status",
		Long: `Wait for a request to reach a target status.

Status ordering: pending (0) < fulfilled (1) < child_exited (2).
The wait succeeds if the current status is >= the target status.
If the request reaches "failed" status, the wait exits with code 1.

Use --timeout 0 for a single (non-blocking) check.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			targetStatus := args[1]
			targetOrder := statusOrder(targetStatus)

			if targetOrder < 0 {
				fmt.Fprintf(os.Stderr, "Error: invalid target status %q (use pending, fulfilled, or child_exited)\n", targetStatus)
				os.Exit(1)
			}

			// Single check mode
			if timeout == 0 {
				return checkRequestOnce(id, targetOrder)
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()

			r, err := pollRequestStatus(ctx, id, targetOrder)
			if err != nil {
				if errors.Is(err, errRequestFailed) && r != nil {
					data, _ := json.MarshalIndent(r, "", "  ")
					fmt.Fprintln(cmd.OutOrStdout(), string(data))
					os.Exit(1)
				}
				if ctx.Err() == context.DeadlineExceeded {
					fmt.Fprintf(os.Stderr, "Timeout: request %s did not reach status %q within %ds\n", id, targetStatus, timeout)
					os.Exit(1)
				}
				return err
			}

			data, _ := json.MarshalIndent(r, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}

	cmd.Flags().IntVar(&timeout, "timeout", 300, "Timeout in seconds (0 for single check)")
	return cmd
}

func checkRequestOnce(id string, targetOrder int) error {
	r, err := readRequestFile(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	currentOrder := statusOrder(r.Status)
	data, _ := json.MarshalIndent(r, "", "  ")

	if currentOrder == -1 {
		// failed
		fmt.Println(string(data))
		os.Exit(1)
	}
	if currentOrder >= targetOrder {
		fmt.Println(string(data))
		return nil
	}

	// Not yet reached
	fmt.Println(string(data))
	os.Exit(1)
	return nil
}

func waitScriptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "script <command> [args...] [--timeout <seconds>]",
		Short: "Run a command and wait for it to complete",
		Long: `Run a command and wait for it to complete with optional timeout.

The --timeout flag must appear at the END of the argument list to avoid
conflicting with flags of the wrapped command.

Examples:
  maestro-request wait script sleep 10 --timeout 5
  maestro-request wait script ./build.sh --verbose --timeout 120`,
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("script requires a command to run")
			}

			// Parse --timeout from the end of args
			timeout := 300 // default
			command := args

			if len(args) >= 2 && args[len(args)-2] == "--timeout" {
				val, err := strconv.Atoi(args[len(args)-1])
				if err != nil {
					return fmt.Errorf("invalid timeout value: %s", args[len(args)-1])
				}
				timeout = val
				command = args[:len(args)-2]
			}

			if len(command) == 0 {
				return fmt.Errorf("script requires a command to run")
			}

			ctx := context.Background()
			if timeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
				defer cancel()
			}

			// Standalone script streams output directly
			child := exec.CommandContext(ctx, command[0], command[1:]...)
			child.Stdout = os.Stdout
			child.Stderr = os.Stderr
			child.Stdin = os.Stdin
			child.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

			err := child.Run()
			if err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					if child.Process != nil {
						_ = syscall.Kill(-child.Process.Pid, syscall.SIGKILL)
					}
					fmt.Fprintf(os.Stderr, "Timeout: command did not complete within %ds\n", timeout)
					os.Exit(1)
				}

				if exitErr, ok := err.(*exec.ExitError); ok {
					os.Exit(exitErr.ExitCode())
				}
				return err
			}
			return nil
		},
	}

	return cmd
}

func waitDaemonCmd() *cobra.Command {
	var timeout int

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Wait for the daemon to become reachable",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()

			resp, err := pollDaemon(ctx)
			if err != nil {
				if ctx.Err() == context.DeadlineExceeded {
					fmt.Fprintf(os.Stderr, "Timeout: daemon not reachable within %ds\n", timeout)
					os.Exit(1)
				}
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), resp)
			return nil
		},
	}

	cmd.Flags().IntVar(&timeout, "timeout", 60, "Timeout in seconds")
	return cmd
}

func waitIdleCmd() *cobra.Command {
	var timeout int

	cmd := &cobra.Command{
		Use:   "idle <request-id>",
		Short: "Wait for a child container's Claude to become idle",
		Long: `Wait for a child container's Claude to become idle (waiting for input).

The request-id must be from a "maestro-request new" command that created the child.
Returns the request JSON on success, exits with code 1 on failure or timeout.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetRequestID := args[0]

			pollTimeout := timeout + 30
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(pollTimeout)*time.Second)
			defer cancel()

			r, err := pollIdleRequest(ctx, targetRequestID, timeout)
			if err != nil {
				if errors.Is(err, errRequestFailed) && r != nil {
					data, _ := json.MarshalIndent(r, "", "  ")
					fmt.Fprintln(cmd.OutOrStdout(), string(data))
					os.Exit(1)
				}
				if ctx.Err() == context.DeadlineExceeded {
					fmt.Fprintf(os.Stderr, "Timeout: wait_idle did not complete within %ds\n", pollTimeout)
					os.Exit(1)
				}
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}

			data, _ := json.MarshalIndent(r, "", "  ")
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}

	cmd.Flags().IntVar(&timeout, "timeout", 300, "Timeout in seconds")
	return cmd
}
