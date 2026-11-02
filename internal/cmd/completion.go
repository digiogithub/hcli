package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newCompletionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate the shell completion script",
		Long: `Generate the autocompletion script for your shell.

Bash (needs bash-completion):
  source <(hcli completion bash)
  # permanent:
  hcli completion bash > ~/.local/share/bash-completion/completions/hcli

Zsh:
  hcli completion zsh > "${fpath[1]}/_hcli"
  # or, with oh-my-zsh: hcli completion zsh > ~/.oh-my-zsh/completions/_hcli
  # make sure "autoload -U compinit; compinit" runs in ~/.zshrc

Fish:
  hcli completion fish > ~/.config/fish/completions/hcli.fish

PowerShell:
  hcli completion powershell | Out-String | Invoke-Expression
  # permanent: add the line above to $PROFILE

Completion is dynamic: resource names (servers, contexts, ...) are fetched
from the API with the active context.`,
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		DisableFlagsInUseLine: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			root := cmd.Root()
			out := cmd.OutOrStdout()
			switch args[0] {
			case "bash":
				return root.GenBashCompletionV2(out, true)
			case "zsh":
				return root.GenZshCompletion(out)
			case "fish":
				return root.GenFishCompletion(out, true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(out)
			}
			return fmt.Errorf("unsupported shell %q", args[0])
		},
	}
}
