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
		Long: `Generate shell completion scripts.
By default, prints the script to stdout.
Use --output to write directly to a file.

Arguments:
  shell    target shell: bash or zsh

Installation:

  bash (with bash-completion package):
    agency completion bash > ~/.local/share/bash-completion/completions/agency
    # or: agency completion --output ~/.local/share/bash-completion/completions/agency bash

  bash (manual):
    agency completion bash > ~/.agency-completion.bash
    echo 'source ~/.agency-completion.bash' >> ~/.bashrc

  zsh (with fpath):
    agency completion zsh > ~/.zsh/completions/_agency
    # ensure ~/.zsh/completions is in fpath before compinit

  zsh (manual):
    agency completion zsh > ~/.agency-completion.zsh
    echo 'source ~/.agency-completion.zsh' >> ~/.zshrc

After installation, restart your shell.`,
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh"},
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := args[0]
			rootCmd := cmd.Root()
			stdout := cmd.OutOrStdout()

			// Determine output writer
			var writer = stdout
			if output != "" {
				// Create parent directories if needed
				dir := filepath.Dir(output)
				if err := os.MkdirAll(dir, 0755); err != nil {
					return errors.Wrap(errors.EInternal, fmt.Sprintf("failed to create directory %s", dir), err)
				}

				// Write to file atomically using temp file + rename
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

			// Write to stdout
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
