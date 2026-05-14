package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
	"github.com/NielsdaWheelz/agency/internal/errors"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage user config",
		Long: `Manage user-scoped agency configuration.

Use "agency config init" to scaffold an operational config.json by detecting
installed runner and editor executables on the current machine.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New(errors.EUsage, "specify a subcommand")
		},
	}

	cmd.AddCommand(
		newConfigInitCmd(),
	)

	return cmd
}

func newConfigInitCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Scaffold user config",
		Long: `Scaffold user config by detecting installed runners and editors.

This command works without a repo and writes only AGENCY_CONFIG_DIR/config.json.
It fails without writing if no supported runner is found on PATH.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, _, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}

			return commands.ConfigInit(ctx, cr, fsys, commands.ConfigInitOpts{
				Force: force,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config.json")

	return cmd
}
