package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func init() {
	completionCmd := &cobra.Command{
		Use:       "completion bash|zsh|fish|powershell",
		Short:     "Generate shell completion script",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletion(os.Stdout)
			default:
				return fail(fmt.Errorf("unsupported shell %q (use bash, zsh, fish, or powershell)", args[0]))
			}
		},
	}
	rootCmd.AddCommand(completionCmd)
}
