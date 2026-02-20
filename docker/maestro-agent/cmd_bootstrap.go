// Copyright 2026 Christopher O'Connell
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

	"github.com/spf13/cobra"
)

func bootstrapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Assemble and print bootstrap prompt to stdout",
		RunE: func(cmd *cobra.Command, args []string) error {
			EnsureStateDirs()
			InitLog()

			manifest, err := LoadManifest()
			if err != nil {
				return fmt.Errorf("no manifest found: %w", err)
			}

			prompt := BuildBootstrapPrompt(manifest)
			fmt.Print(prompt)
			return nil
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print current agent state",
		RunE: func(cmd *cobra.Command, args []string) error {
			EnsureStateDirs()

			state := ReadState()
			fmt.Printf("state: %s\n", state)

			if HasQueuedMessages() {
				fmt.Println("queue: messages pending")
			} else {
				fmt.Println("queue: empty")
			}

			if isUserConnected() {
				fmt.Println("tmux: user connected")
			} else {
				fmt.Println("tmux: no user")
			}

			if HasManifest() {
				manifest, err := LoadManifest()
				if err == nil {
					fmt.Printf("type: %s\n", manifest.Type)
				}
			} else {
				fmt.Println("manifest: not found")
			}

			return nil
		},
	}
}
