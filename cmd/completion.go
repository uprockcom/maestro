// Copyright 2025 Nandor Kis
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
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for maestro.

To load completions:

Bash:
  $ source <(maestro completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ maestro completion bash > /etc/bash_completion.d/maestro
  # macOS:
  $ maestro completion bash > $(brew --prefix)/etc/bash_completion.d/maestro

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ maestro completion zsh > "${fpath[1]}/_maestro"

  # You may need to start a new shell for this setup to take effect.

Fish:
  $ maestro completion fish | source

  # To load completions for each session, execute once:
  $ maestro completion fish > ~/.config/fish/completions/maestro.fish

PowerShell:
  PS> maestro completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> maestro completion powershell > maestro.ps1
  # and source this file from your PowerShell profile.
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "bash":
			cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}
