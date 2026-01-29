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

func newResumeCmd() *cobra.Command {
	var repoPath string
	var detached bool
	var restart bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "resume <run>",
		Short: "Attach to tmux session (create if missing)",
		Long: `Attach to the tmux session for a run.
If session is missing, creates one and starts the runner.
Works from any directory; resolves runs globally.

Arguments:
  run    run name, run_id, or unique run_id prefix

Notes:
  - resume never runs scripts (setup/verify/archive)
  - resume preserves git state; only tmux session changes
  - --restart will lose in-tool history (chat context, etc.)`,
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

			opts := commands.ResumeOpts{
				RunID:    args[0],
				RepoPath: repoPath,
				Detached: detached,
				Restart:  restart,
				Yes:      yes,
			}

			return commands.Resume(ctx, cr, fsys, cwd, opts, os.Stdin, stdout, stderr)
		},
	}

	cmd.Flags().StringVar(&repoPath, "repo", "", "scope name resolution to a specific repo")
	cmd.Flags().BoolVar(&detached, "detached", false, "do not attach; return after ensuring session exists")
	cmd.Flags().BoolVar(&restart, "restart", false, "kill existing session (if any) and recreate")
	cmd.Flags().BoolVar(&yes, "yes", false, "skip confirmation prompt for --restart")

	return cmd
}
