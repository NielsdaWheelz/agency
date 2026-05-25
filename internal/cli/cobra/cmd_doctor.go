package cobra

import (
	"github.com/spf13/cobra"

	"github.com/NielsdaWheelz/agency/internal/commands"
)

func newDoctorCmd() *cobra.Command {
	var path string
	var agencyConfigPath string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check one repo checkout and show resolved config",
		Long: `Check the current repository setup without modifying anything.

Doctor verifies the repo checkout, required tools, resolved runner command, and
resolved agency scripts for one git repository. It requires user config from
"agency config init".

If --path is omitted, doctor uses the repository that contains the current
directory. If the selected path is inside an agency-managed worktree or
sandbox, doctor reports the owning repo's canonical config. If --agency-config
is relative, it is resolved from the current directory before loading.`,
		Example: `  agency doctor
  agency doctor --path /path/to/repo
  agency doctor --agency-config ./agency.json
  agency doctor --path /path/to/repo --agency-config ./agency.local.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cr, fsys, cwd, err := realCommandDepsFromCmd(cmd)
			if err != nil {
				return err
			}
			return commands.Doctor(ctx, cr, fsys, cwd, commands.DoctorOpts{
				Path:             path,
				AgencyConfigPath: agencyConfigPath,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "target checkout path (defaults to current directory)")
	cmd.Flags().StringVar(&agencyConfigPath, "agency-config", "", "load agency config from this file")

	return cmd
}
