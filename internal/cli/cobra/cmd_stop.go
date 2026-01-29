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

func newStopCmd() *cobra.Command {
	var repoPath string

	cmd := &cobra.Command{
		Use:   "stop <run>",
		Short: "Send C-c to runner (best-effort interrupt)",
		Long: `Send C-c to the runner in the tmux session (best-effort interrupt).
Sets needs_attention flag on the run.
Works from any directory; resolves runs globally.

Arguments:
  run    run name, run_id, or unique run_id prefix

Notes:
  - best-effort only; may not stop the runner if it is in the middle of an operation
  - session remains alive; use 'agency resume --restart' to guarantee a fresh runner`,
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

			opts := commands.StopOpts{
				RunID:    args[0],
				RepoPath: repoPath,
			}

			return commands.Stop(ctx, cr, fsys, cwd, opts, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", "", "scope name resolution to a specific repo")

	return cmd
}
