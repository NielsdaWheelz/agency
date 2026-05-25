package cobra

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newWatchCmd() *cobra.Command {
	var intervalStr string

	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Unified workspace/history/logs TUI",
		Long: `Open the unified Bubble Tea runtime for live monitoring.

The watch runtime composes workspace, history, and logs pages from
daemon-owned read APIs.

Keyboard shortcuts:
  - tab / shift+tab: move focus across Repos, Worktrees, and Agents
  - up/down (or k/j): move selection in the focused pane
  - enter: apply repo/worktree scope, or run the selected agent default action
  - b/esc: broaden workspace scope; go back from history/logs
  - o: open selected invocation sandbox
  - p: sync PR for selected invocation's worktree
  - h: open history for the selected invocation
  - l: open raw logs for the selected invocation
  - x: open the action menu, including recreate when a headed session is missing
  - r: refresh now
  - q: exit watch

Use --interval to tune periodic refresh cadence.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			interval, err := time.ParseDuration(intervalStr)
			if err != nil {
				return errors.New(errors.EInvalidArgument, err.Error())
			}
			if interval < 250*time.Millisecond || interval > 5*time.Second {
				return errors.New(errors.EInvalidArgument, "interval must be between 250ms and 5s")
			}

			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}
			return commands.Watch(ctx, cr, fsys, cwd, commands.WatchOpts{
				Interval: interval,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&intervalStr, "interval", "2s", "Refresh interval (250ms to 5s)")

	return cmd
}
