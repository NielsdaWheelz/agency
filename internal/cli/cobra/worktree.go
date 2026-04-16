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

Integration worktrees are stable branches you intend to merge, push, or open pull requests from.
Use agency worktree pr for pull request operations.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand")
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
