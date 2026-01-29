package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Interactive TUI for monitoring worktrees and agents",
		Long: `Interactive TUI for monitoring worktrees and agents.

Watch provides a hierarchical live view of:
  - Integration worktrees
  - Agent invocations per worktree

Actions include attach, view logs, land/discard, stop/kill, and more.

This command will be implemented in a future release.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "watch is not yet implemented")
		},
	}

	return cmd
}
