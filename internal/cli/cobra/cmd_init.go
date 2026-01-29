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

func newInitCmd() *cobra.Command {
	var repoPath string
	var noGitignore bool
	var force bool

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create agency.json template and stub scripts",
		Long: `Create agency.json template and stub scripts in a repo.
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

			opts := commands.InitOpts{
				RepoPath:    repoPath,
				NoGitignore: noGitignore,
				Force:       force,
			}

			return commands.Init(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", "", "target a specific repo (default: current directory)")
	cmd.Flags().BoolVar(&noGitignore, "no-gitignore", false, "do not modify .gitignore")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing agency.json")

	return cmd
}
