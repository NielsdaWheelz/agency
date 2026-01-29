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

func newKillCmd() *cobra.Command {
	var repoPath string

	cmd := &cobra.Command{
		Use:   "kill <run>",
		Short: "Kill tmux session (workspace remains)",
		Long: `Kill the tmux session for a run.
Workspace remains intact.
Works from any directory; resolves runs globally.

Arguments:
  run    run name, run_id, or unique run_id prefix`,
		Args: cobra.ExactArgs(1),
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

			opts := commands.KillOpts{
				RunID:    args[0],
				RepoPath: repoPath,
			}

			return commands.Kill(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", "", "scope name resolution to a specific repo")

	return cmd
}
