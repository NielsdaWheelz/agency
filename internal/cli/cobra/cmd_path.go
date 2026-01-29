package cobra

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newPathCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "path <run>",
		Short: "Output worktree path for a run (for scripting)",
		Long: `Output the worktree path for a run (single line, for scripting).
Resolves globally (works from anywhere, not just inside a repo).

Arguments:
  run    run name, run_id, or unique run_id prefix

Shell integration:
  # add to your .bashrc or .zshrc:
  acd() { cd "$(agency path "$1")" || return 1; }

  # then use:
  acd my-feature`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()
			ctx := context.Background()

			opts := commands.PathOpts{
				RunRef: args[0],
			}

			return commands.Path(ctx, opts, stdout, stderr)
		},
	}

	return cmd
}
