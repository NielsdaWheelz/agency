package cobra

import (
	"context"
	"os"

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
		Short: "Full-screen workspace for readiness monitoring",
		Long: `Open the full-screen watch workspace for live monitoring.

The watch workspace composes repositories, worktrees, invocations, and
invocation review/readiness data from daemon-owned read APIs.

Keyboard shortcuts:
  - up/down (or k/j): move selection
  - enter: enter selected headed invocation
  - o: open selected invocation sandbox
  - p: sync PR for selected invocation's worktree
  - r: refresh now
  - q/esc: exit watch

Use --interval to tune periodic refresh cadence.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			interval, err := parseWatchInterval(intervalStr)
			if err != nil {
				return errors.New(errors.EInvalidArgument, err.Error())
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
