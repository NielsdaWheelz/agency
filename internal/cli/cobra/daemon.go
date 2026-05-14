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

The daemon supervises headless agent invocations, capturing output and
tracking process state.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand")
		},
	}

	cmd.AddCommand(
		newDaemonStartCmd(),
		newDaemonStatusCmd(),
		newDaemonStopCmd(),
		newDaemonInstallCmd(),
		newDaemonUninstallCmd(),
	)

	return cmd
}

func newDaemonStartCmd() *cobra.Command {
	var foreground bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon",
		Long: `Start the agency daemon.

By default the daemon is started as a detached background process. The command
waits for the daemon to pass a health check, prints its PID and socket path,
then returns control to the shell.

Use --foreground to run the server loop in the current process (for service
managers like launchd/systemd, or for debugging). Press Ctrl-C to stop.

If the daemon is already running, this command exits successfully (no-op).

The daemon listens on a Unix socket at ${AGENCY_DATA_DIR}/agencyd.sock.

Examples:
  agency daemon start               # background (default)
  agency daemon start --foreground   # foreground (for launchd/systemd)`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			return commands.DaemonStart(ctx, cr, fsys, commands.DaemonStartOpts{
				Foreground: foreground,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().BoolVar(&foreground, "foreground", false, "Run in foreground (for service managers or debugging)")

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

func newDaemonInstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install daemon as an OS service",
		Long: `Install the agency daemon as an OS-managed service.

On macOS, writes a launchd plist to ~/Library/LaunchAgents/ and loads it.
On Linux, writes a systemd user unit to ~/.config/systemd/user/ and enables it.

The service runs "agency daemon start --foreground" and is configured to:
- Start automatically on login
- Restart on failure

Examples:
  agency daemon install
  agency daemon uninstall   # to remove`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cr := exec.NewRealRunner()
			ctx := context.Background()

			return commands.DaemonInstall(ctx, cr, commands.DaemonInstallOpts{},
				cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	return cmd
}

func newDaemonUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove daemon OS service",
		Long: `Remove the agency daemon OS service.

On macOS, unloads and removes the launchd plist.
On Linux, stops, disables, and removes the systemd user unit.

Example:
  agency daemon uninstall`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cr := exec.NewRealRunner()
			ctx := context.Background()

			return commands.DaemonUninstall(ctx, cr, commands.DaemonUninstallOpts{},
				cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	return cmd
}
