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
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

type waitSpec struct {
	Index int
	Raw   string
	Type  string   // "request", "idle", "script", "daemon", "message"
	Args  []string
}

type waitResult struct {
	Index   int
	Spec    string
	Type    string
	Success bool
	Result  interface{}
	Error   string
}

func parseWaitSpec(index int, raw string) (*waitSpec, error) {
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty spec at index %d", index)
	}

	spec := &waitSpec{
		Index: index,
		Raw:   raw,
		Type:  parts[0],
		Args:  parts[1:],
	}

	switch spec.Type {
	case "request":
		if len(spec.Args) != 2 {
			return nil, fmt.Errorf("spec %d: 'request' requires <id> <status>, got %d args", index, len(spec.Args))
		}
		if statusOrder(spec.Args[1]) < 0 {
			return nil, fmt.Errorf("spec %d: invalid status %q (use pending, fulfilled, or child_exited)", index, spec.Args[1])
		}
	case "idle":
		if len(spec.Args) != 1 {
			return nil, fmt.Errorf("spec %d: 'idle' requires <request-id>, got %d args", index, len(spec.Args))
		}
	case "script":
		if len(spec.Args) == 0 {
			return nil, fmt.Errorf("spec %d: 'script' requires a command", index)
		}
	case "daemon":
		if len(spec.Args) != 0 {
			return nil, fmt.Errorf("spec %d: 'daemon' takes no arguments", index)
		}
	case "message":
		if len(spec.Args) != 0 {
			return nil, fmt.Errorf("spec %d: 'message' takes no arguments", index)
		}
	default:
		return nil, fmt.Errorf("spec %d: unknown type %q (use request, idle, script, daemon, or message)", index, spec.Type)
	}

	return spec, nil
}

// runWaitSpec runs a single spec and sends the result on ch.
func runWaitSpec(ctx context.Context, spec *waitSpec, timeout int, ch chan<- waitResult) {
	result := waitResult{
		Index: spec.Index,
		Spec:  spec.Raw,
		Type:  spec.Type,
	}

	switch spec.Type {
	case "request":
		id := spec.Args[0]
		targetOrder := statusOrder(spec.Args[1])
		r, err := pollRequestStatus(ctx, id, targetOrder)
		if err != nil {
			result.Success = false
			if r != nil {
				result.Result = r
				result.Error = "request failed"
			} else {
				result.Error = err.Error()
			}
		} else {
			result.Success = true
			result.Result = r
		}

	case "idle":
		targetRequestID := spec.Args[0]
		r, err := pollIdleRequest(ctx, targetRequestID, timeout)
		if err != nil {
			result.Success = false
			if r != nil {
				result.Result = r
				result.Error = "request failed"
			} else {
				result.Error = err.Error()
			}
		} else {
			result.Success = true
			result.Result = r
		}

	case "script":
		exitCode, output, err := runScript(ctx, spec.Args)
		scriptResult := map[string]interface{}{
			"exit_code": exitCode,
			"output":    output,
		}
		if err != nil {
			result.Success = false
			result.Error = err.Error()
			result.Result = scriptResult
		} else if exitCode != 0 {
			result.Success = false
			result.Result = scriptResult
		} else {
			result.Success = true
			result.Result = scriptResult
		}

	case "daemon":
		resp, err := pollDaemon(ctx)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
		} else {
			result.Success = true
			var parsed interface{}
			if json.Unmarshal([]byte(resp), &parsed) == nil {
				result.Result = parsed
			} else {
				result.Result = resp
			}
		}

	case "message":
		msgResult, err := pollMessages(ctx)
		if err != nil {
			result.Success = false
			result.Error = err.Error()
		} else {
			result.Success = true
			result.Result = msgResult
		}
	}

	select {
	case ch <- result:
	case <-ctx.Done():
	}
}

func waitAnyCmd() *cobra.Command {
	var timeout int

	cmd := &cobra.Command{
		Use:   `any "spec1" "spec2" ... [--timeout seconds]`,
		Short: "Wait for the first of multiple conditions to complete",
		Long: `Wait for the first of multiple conditions to complete (OR logic).

Each argument is a quoted spec string:
  "request <id> <status>"  — wait for request to reach status
  "idle <request-id>"      — wait for child's Claude to become idle
  "script <cmd> [args...]" — run command, wait for exit
  "daemon"                 — wait for daemon to become reachable
  "message"                — wait for a message in pending-messages queue

Returns JSON with the first matching spec. Exit code 0 if the match
succeeded, 1 if it failed, 2 on global timeout.

Examples:
  maestro-request wait any "request $ID child_exited" "script ./monitor.sh" --timeout 600
  maestro-request wait any "idle $ID" "request $ID child_exited"
  maestro-request wait any "script ./watch.sh" "message" --timeout 3600`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specs, err := parseAllSpecs(args)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()

			ch := make(chan waitResult, len(specs))
			for _, s := range specs {
				go runWaitSpec(ctx, s, timeout, ch)
			}

			select {
			case result := <-ch:
				cancel()

				others := make([]map[string]interface{}, 0, len(specs)-1)
				for _, s := range specs {
					if s.Index != result.Index {
						others = append(others, map[string]interface{}{
							"index":  s.Index,
							"spec":   s.Raw,
							"status": "canceled",
						})
					}
				}

				output := map[string]interface{}{
					"matched_index": result.Index,
					"matched_spec":  result.Spec,
					"matched_type":  result.Type,
					"success":       result.Success,
					"result":        result.Result,
					"others":        others,
				}
				if result.Error != "" {
					output["error"] = result.Error
				}

				data, _ := json.MarshalIndent(output, "", "  ")
				fmt.Fprintln(cmd.OutOrStdout(), string(data))

				if !result.Success {
					os.Exit(1)
				}
				return nil

			case <-ctx.Done():
				fmt.Fprintf(os.Stderr, "Timeout: no spec completed within %ds\n", timeout)
				os.Exit(2)
				return nil
			}
		},
	}

	cmd.Flags().IntVar(&timeout, "timeout", 300, "Global timeout in seconds")
	return cmd
}

func waitAllCmd() *cobra.Command {
	var timeout int

	cmd := &cobra.Command{
		Use:   `all "spec1" "spec2" ... [--timeout seconds]`,
		Short: "Wait for all conditions to complete",
		Long: `Wait for all conditions to complete (AND logic).

Each argument is a quoted spec string:
  "request <id> <status>"  — wait for request to reach status
  "idle <request-id>"      — wait for child's Claude to become idle
  "script <cmd> [args...]" — run command, wait for exit
  "daemon"                 — wait for daemon to become reachable
  "message"                — wait for a message in pending-messages queue

Returns JSON array of all results. If any spec fails, remaining specs
are canceled and the command exits with code 1. Exit code 2 on global timeout.

Examples:
  maestro-request wait all "request $ID1 child_exited" "request $ID2 child_exited" --timeout 600
  maestro-request wait all "request $ID fulfilled" "daemon"`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			specs, err := parseAllSpecs(args)
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()

			ch := make(chan waitResult, len(specs))
			for _, s := range specs {
				go runWaitSpec(ctx, s, timeout, ch)
			}

			results := make([]waitResult, 0, len(specs))
			for i := 0; i < len(specs); i++ {
				select {
				case result := <-ch:
					results = append(results, result)
					if !result.Success {
						cancel()
						outputAllResults(cmd, results)
						os.Exit(1)
						return nil
					}

				case <-ctx.Done():
					fmt.Fprintf(os.Stderr, "Timeout: not all specs completed within %ds\n", timeout)
					os.Exit(2)
					return nil
				}
			}

			outputAllResults(cmd, results)
			return nil
		},
	}

	cmd.Flags().IntVar(&timeout, "timeout", 300, "Global timeout in seconds")
	return cmd
}

func parseAllSpecs(args []string) ([]*waitSpec, error) {
	specs := make([]*waitSpec, len(args))
	for i, raw := range args {
		s, err := parseWaitSpec(i, raw)
		if err != nil {
			return nil, err
		}
		specs[i] = s
	}
	return specs, nil
}

func outputAllResults(cmd *cobra.Command, results []waitResult) {
	formatted := make([]map[string]interface{}, len(results))
	for i, r := range results {
		m := map[string]interface{}{
			"index":   r.Index,
			"spec":    r.Spec,
			"success": r.Success,
			"result":  r.Result,
		}
		if r.Error != "" {
			m["error"] = r.Error
		}
		formatted[i] = m
	}

	output := map[string]interface{}{
		"results": formatted,
	}
	data, _ := json.MarshalIndent(output, "", "  ")
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
}
