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

Integration worktrees are the long-lived branches you intend to merge, push,
rebase, and open pull requests from. They are separate from agent sandboxes.

Use:
  agency worktree create   to make a new integration worktree
  agency worktree ls/show  to inspect worktrees
  agency worktree pr ...   to push, sync, and merge pull requests`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency worktree <command>")
		},
	}

	cmd.AddCommand(
		newWorktreeCreateCmd(),
		newWorktreeLSCmd(),
		newWorktreeShowCmd(),
		newWorktreePathCmd(),
		newWorktreeOpenCmd(),
		newWorktreeShellCmd(),
		newWorktreeRmCmd(),
		newWorktreePRCmd(),
		newWorktreeRebaseCmd(),
	)

	return cmd
}
