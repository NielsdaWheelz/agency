package cobra

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newCompletionCmd() *cobra.Command {
	var output string

	cmd := &cobra.Command{
		Use:   "completion <shell>",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for agency.

The generated scripts include completion for repo refs, worktree refs,
invocation refs, and enum-like flags such as runner ids and log kinds.

By default, the script is printed to stdout. Use --output to write it directly
to a file.`,
		Example: `  agency completion bash > ~/.local/share/bash-completion/completions/agency
  agency completion zsh > ~/.zsh/completions/_agency
  agency completion --output ~/.zsh/completions/_agency zsh`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh"},
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := args[0]
			rootCmd := cmd.Root()
			writer := cmd.OutOrStdout()

			if output != "" {
				dir := filepath.Dir(output)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return errors.Wrap(errors.EInternal, fmt.Sprintf("failed to create directory %s", dir), err)
				}

				tmpPath := output + ".tmp"
				f, err := os.Create(tmpPath)
				if err != nil {
					return errors.Wrap(errors.EInternal, fmt.Sprintf("failed to create %s", output), err)
				}
				defer func() { _ = f.Close() }()

				var genErr error
				switch shell {
				case "bash":
					genErr = rootCmd.GenBashCompletion(f)
				case "zsh":
					genErr = rootCmd.GenZshCompletion(f)
				default:
					_ = os.Remove(tmpPath)
					return errors.New(errors.EUsage, fmt.Sprintf("unsupported shell: %s (supported: bash, zsh)", shell))
				}
				if genErr != nil {
					_ = os.Remove(tmpPath)
					return errors.Wrap(errors.EInternal, "failed to generate completion script", genErr)
				}
				if err := f.Close(); err != nil {
					_ = os.Remove(tmpPath)
					return errors.Wrap(errors.EInternal, fmt.Sprintf("failed to write %s", output), err)
				}
				if err := os.Rename(tmpPath, output); err != nil {
					_ = os.Remove(tmpPath)
					return errors.Wrap(errors.EInternal, fmt.Sprintf("failed to rename to %s", output), err)
				}
				return nil
			}

			switch shell {
			case "bash":
				return rootCmd.GenBashCompletion(writer)
			case "zsh":
				return rootCmd.GenZshCompletion(writer)
			default:
				return errors.New(errors.EUsage, fmt.Sprintf("unsupported shell: %s (supported: bash, zsh)", shell))
			}
		},
	}

	cmd.Flags().StringVar(&output, "output", "", "write completion script to file instead of stdout")

	return cmd
}
