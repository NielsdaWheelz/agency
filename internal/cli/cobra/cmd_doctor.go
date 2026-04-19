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
directory. If --agency-config is relative, it is resolved from the current
directory before loading.`,
		Example: `  agency doctor
  agency doctor --path /path/to/repo
  agency doctor --agency-config ./agency.json
  agency doctor --path /path/to/repo --agency-config ./agency.local.json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			stdout := cmd.OutOrStdout()
			stderr := cmd.ErrOrStderr()

			cwd, err := os.Getwd()
			if err != nil {
				return errors.Wrap(errors.EInternal, "failed to get working directory", err)
			}

			cr := exec.NewRealRunner()
			fsys := fs.NewRealFS()
			ctx := context.Background()

			opts := commands.DoctorOpts{
				Path:             path,
				AgencyConfigPath: agencyConfigPath,
			}

			return commands.Doctor(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&path, "path", "", "target checkout path (defaults to current directory)")
	cmd.Flags().StringVar(&agencyConfigPath, "agency-config", "", "load agency config from this file")

	return cmd
}
