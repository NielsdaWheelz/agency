package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newWorktreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worktree",
		Short: "Manage integration worktrees",
		Long: `Manage integration worktrees.

Integration worktrees are stable branches you intend to merge, push, or PR.
They are independent of any agent invocation.

Available subcommands will be added in future releases:
  create    Create a new integration worktree
  ls        List integration worktrees
  show      Show details of a worktree
  path      Output worktree path for scripting
  open      Open worktree in editor
  shell     Open shell in worktree
  rm        Remove a worktree`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency worktree <create|ls|show|path|open|shell|rm>")
		},
	}

	return cmd
}
