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

func newCleanCmd() *cobra.Command {
	var repoPath string
	var allowDirty bool
	var deleteBranch bool
	var yes bool

	cmd := &cobra.Command{
		Use:   "clean <run>",
		Short: "Archive without merging (abandon run)",
		Long: `Archive a run without merging (abandon).
Works from any directory; resolves runs globally.
Non-interactive usage requires explicit confirmation via --yes.

Arguments:
  run    run name, run_id, or unique run_id prefix

Behavior:
  - runs scripts.archive (timeout: 5m)
  - kills tmux session if exists
  - deletes worktree
  - retains metadata and logs
  - marks run as abandoned

  with --delete-branch:
  - deletes local git branch
  - deletes remote branch (if pushed)
  - closes PR (if exists)

Confirmation:
  you must type 'clean' to confirm the operation unless --yes.`,
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

			opts := commands.CleanOpts{
				RunID:        args[0],
				RepoPath:     repoPath,
				AllowDirty:   allowDirty,
				DeleteBranch: deleteBranch,
				Yes:          yes,
			}

			return commands.Clean(ctx, cr, fsys, cwd, opts, os.Stdin, stdout, stderr)
		},
	}

	cmd.Flags().StringVarP(&repoPath, "repo", "r", "", "scope name resolution to a specific repo")
	cmd.Flags().BoolVar(&allowDirty, "allow-dirty", false, "allow clean even if worktree has uncommitted changes")
	cmd.Flags().BoolVar(&deleteBranch, "delete-branch", false, "delete local/remote branch and close PR")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "confirm clean in non-interactive mode")

	return cmd
}
