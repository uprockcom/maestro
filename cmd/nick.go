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

package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/uprockcom/maestro/pkg/daemon"
)

var nickListFlag bool

var nickCmd = &cobra.Command{
	Use:   "nick [container] [nickname]",
	Short: "Assign a nickname to a container",
	Long: `Assign a short nickname to a container for easy reference.

Examples:
  maestro nick feat-auth-1 auth       # Assign nickname "auth"
  maestro nick --list                 # List all nicknames`,
	Args: cobra.MaximumNArgs(2),
	RunE: runNick,
}

func init() {
	rootCmd.AddCommand(nickCmd)
	nickCmd.Flags().BoolVar(&nickListFlag, "list", false, "List all nicknames")
}

func runNick(cmd *cobra.Command, args []string) error {
	store := getNicknameStore()

	if nickListFlag || len(args) == 0 {
		all := store.All()
		if len(all) == 0 {
			fmt.Println("No nicknames assigned")
			return nil
		}
		for nick, container := range all {
			fmt.Printf("  %s → %s\n", nick, container)
		}
		return nil
	}

	if len(args) < 2 {
		return fmt.Errorf("usage: maestro nick <container> <nickname>")
	}

	containerShort := args[0]
	nickname := args[1]

	// Resolve container name
	containerName := resolveContainerName(containerShort)

	if err := store.Set(nickname, containerName); err != nil {
		return fmt.Errorf("failed to save nickname: %w", err)
	}

	fmt.Printf("Nickname %q → %s\n", nickname, containerName)
	return nil
}

// getNicknameStore returns a NicknameStore using the standard path.
func getNicknameStore() *daemon.NicknameStore {
	authDir := expandPath(config.Claude.AuthPath)
	return daemon.NewNicknameStore(filepath.Join(authDir, "nicknames.yml"))
}
