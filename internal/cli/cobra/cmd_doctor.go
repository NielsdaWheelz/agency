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
	var repoPath string
	var agencyConfigPath string

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check prerequisites and show resolved paths",
		Long: `Check prerequisites and show resolved paths.
Verifies git, tmux, gh, runner command, and scripts are present and configured.
Requires user config; run "agency config init" first.
Defaults to current directory; use --repo to target a different repo.`,
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
				RepoPath:         repoPath,
				AgencyConfigPath: agencyConfigPath,
			}

			return commands.Doctor(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", "", "target a specific repo (default: current directory)")
	cmd.Flags().StringVar(&agencyConfigPath, "agency-config", "", "load agency config from this file")

	return cmd
}
