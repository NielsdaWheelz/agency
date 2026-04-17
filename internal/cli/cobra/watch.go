package cobra

import (
	"context"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
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
  - up/down (or k/j): move selection
  - enter: attach to selected headed invocation
  - o: open selected invocation sandbox
  - p: sync PR for selected invocation's worktree
  - h: open history for the selected invocation
  - l: open raw logs for the selected invocation
  - b/esc: go back from history/logs
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

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get cwd", err)
			}

			ctx := context.Background()
			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			return commands.Watch(ctx, cr, fsys, cwd, commands.WatchOpts{
				Interval: interval,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&intervalStr, "interval", "2s", "Refresh interval (250ms to 5s)")

	return cmd
}
