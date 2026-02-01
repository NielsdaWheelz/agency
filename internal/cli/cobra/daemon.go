package cobra

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
	"github.com/NielsdaWheelz/agency/internal/exec"
	"github.com/NielsdaWheelz/agency/internal/fs"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the agency daemon",
		Long: `Manage the agency daemon.

The daemon supervises headless agent invocations, providing:
- Streaming stdout/stderr capture
- Correct process lifecycle management
- Reliable PID tracking and recovery

Subcommands:
  start     Start the daemon (foreground)
  status    Show daemon status
  stop      Stop the daemon`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand: agency daemon <start|status|stop>")
		},
	}

	cmd.AddCommand(
		newDaemonStartCmd(),
		newDaemonStatusCmd(),
		newDaemonStopCmd(),
	)

	return cmd
}

func newDaemonStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon",
		Long: `Start the agency daemon in the foreground.

The daemon runs a server loop in the current process. Use Ctrl-C to stop.
If the daemon is already running, this command exits successfully (no-op).

The daemon listens on a Unix socket at ${AGENCY_DATA_DIR}/agencyd.sock.

Example:
  agency daemon start`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.DaemonStart(ctx, cr, fsys, commands.DaemonStartOpts{
				Foreground: true,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	return cmd
}

func newDaemonStatusCmd() *cobra.Command {
	var jsonOut bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show daemon status",
		Long: `Show the status of the agency daemon.

Checks if the daemon is running and displays health information.

Example:
  agency daemon status
  agency daemon status --json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.DaemonStatus(ctx, fsys, commands.DaemonStatusOpts{
				JSON: jsonOut,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().BoolVar(&jsonOut, "json", false, "Output as JSON")

	return cmd
}

func newDaemonStopCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon",
		Long: `Stop the agency daemon.

By default, refuses to stop if there are active headless invocations.
Use --force to terminate all active invocations and stop the daemon.

Example:
  agency daemon stop
  agency daemon stop --force`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.DaemonStop(ctx, fsys, commands.DaemonStopOpts{
				Force: force,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Terminate active invocations and stop")

	return cmd
}
