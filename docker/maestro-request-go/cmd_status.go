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
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check daemon connectivity and status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !daemonAvailable() {
				fmt.Fprintln(os.Stderr, "Daemon not configured (daemon-ipc.json not found)")
				os.Exit(1)
			}

			resp, err := daemonCall("GET", "/status", "")
			if err != nil {
				fmt.Fprintln(os.Stderr, "Could not reach daemon.")
				os.Exit(1)
			}

			if resp == "" {
				fmt.Fprintln(os.Stderr, "Could not reach daemon.")
				os.Exit(1)
			}

			fmt.Fprintln(cmd.OutOrStdout(), resp)
			return nil
		},
	}
}
